// Package io provides the IO system for micrun container runtime.
// It handles bidirectional data copying between FIFOs and TTY for RTOS containers.
package io

import (
	"context"
	"sync"
	"time"

	"micrun/internal/support/contextx"
	"micrun/internal/support/lockutil"
	"micrun/internal/support/timex"
)

const eventChannelBufferSize = 16

// EventType represents the type of IO event.
type EventType int

const (
	// ExitCommandDetected is fired when the user types "exit" in the shell.
	ExitCommandDetected EventType = iota

	// IOError is fired when an IO error occurs.
	IOError

	// TTYReady is fired when the TTY is ready for IO.
	TTYReady

	// StdinClosed is fired when the stdin FIFO is closed by the client.
	StdinClosed

	// DetachDetected is fired when the user presses the detach key sequence (Ctrl+P, Ctrl+Q).
	DetachDetected

	// InterruptDetected is fired when the user presses Ctrl+C in a TTY session.
	InterruptDetected
)

// Event represents an IO event.
type Event struct {
	Type        EventType
	ContainerID string
	Err         error
	Timestamp   time.Time
}

// EventSubscriber is a read-only channel that receives events.
type EventSubscriber = <-chan Event

type eventSubscriber chan Event

// EventBus provides a simple event bus for IO layer to publish events
// and shim layer to subscribe to events.
type EventBus struct {
	subscribers map[EventType][]eventSubscriber
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	closed      bool
	now         timex.Clock
}

// NewEventBus creates a new event bus.
func NewEventBus(ctx context.Context) *EventBus {
	return newEventBus(ctx, nil)
}

func newEventBus(ctx context.Context, now timex.Clock) *EventBus {
	ctx = contextx.OrBackground(ctx)
	ctx, cancel := context.WithCancel(ctx)
	bus := &EventBus{
		subscribers: make(map[EventType][]eventSubscriber),
		ctx:         ctx,
		cancel:      cancel,
		now:         now,
	}
	go func() {
		<-ctx.Done()
		bus.Close()
	}()
	return bus
}

func (b *EventBus) withLock(body func()) {
	lockutil.WithLock(&b.mu, body)
}

// Subscribe subscribes to events of a specific type.
// Returns a channel that will receive events.
func (b *EventBus) Subscribe(eventType EventType) EventSubscriber {
	return b.subscribe(eventType)
}

func (b *EventBus) subscribe(eventType EventType) eventSubscriber {
	ch := make(eventSubscriber, eventChannelBufferSize)
	b.withLock(func() {
		if b.closed || b.ctx.Err() != nil {
			close(ch)
			return
		}
		b.subscribers[eventType] = append(b.subscribers[eventType], ch)
	})
	return ch
}

// SubscribeContext subscribes to events until either the provided context or
// the bus context is canceled.
func (b *EventBus) SubscribeContext(ctx context.Context, eventType EventType) EventSubscriber {
	ctx = contextx.OrBackground(ctx)
	if ctx.Err() != nil {
		ch := make(eventSubscriber, eventChannelBufferSize)
		close(ch)
		return ch
	}

	ch := b.subscribe(eventType)
	go func() {
		select {
		case <-ctx.Done():
		case <-b.ctx.Done():
		}
		b.unsubscribe(eventType, ch)
	}()
	return ch
}

func (b *EventBus) unsubscribe(eventType EventType, ch eventSubscriber) {
	closeSubscriber := false
	b.withLock(func() {
		if b.closed {
			return
		}
		subscribers := b.subscribers[eventType]
		for i, subscriber := range subscribers {
			if subscriber != ch {
				continue
			}
			copy(subscribers[i:], subscribers[i+1:])
			subscribers[len(subscribers)-1] = nil
			subscribers = subscribers[:len(subscribers)-1]
			if len(subscribers) == 0 {
				delete(b.subscribers, eventType)
			} else {
				b.subscribers[eventType] = subscribers
			}
			closeSubscriber = true
			break
		}
	})
	if closeSubscriber {
		close(ch)
	}
}

// Publish publishes an event to all subscribers.
func (b *EventBus) Publish(event Event) {
	type publishSnapshot struct {
		subscribers   []eventSubscriber
		shouldPublish bool
	}
	snapshot := lockutil.WithReadLockValue(&b.mu, func() publishSnapshot {
		if b.closed || b.ctx.Err() != nil {
			return publishSnapshot{}
		}
		event.Timestamp = timex.Now(b.now)
		return publishSnapshot{
			subscribers:   append([]eventSubscriber(nil), b.subscribers[event.Type]...),
			shouldPublish: true,
		}
	})
	if !snapshot.shouldPublish {
		return
	}

	for _, ch := range snapshot.subscribers {
		publishEventSafe(ch, event)
	}
}

func publishEventSafe(ch eventSubscriber, event Event) {
	defer func() {
		_ = recover()
	}()

	select {
	case ch <- event:
	default:
		// Channel full, drop event to avoid blocking
	}
}

// Close closes the event bus and all subscriber channels.
func (b *EventBus) Close() {
	var subs []eventSubscriber
	b.withLock(func() {
		if b.closed {
			return
		}
		b.closed = true
		for _, chs := range b.subscribers {
			subs = append(subs, chs...)
		}
		b.subscribers = make(map[EventType][]eventSubscriber)
	})
	for _, ch := range subs {
		close(ch)
	}
	b.cancel()
}
