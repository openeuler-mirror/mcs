// Package io provides the IO system for micrun container runtime.
// It handles bidirectional data copying between FIFOs and TTY for RTOS containers.
package io

import (
	"context"
	"sync"
	"time"

	"micrun/internal/support/contextx"
	"micrun/internal/support/lockutil"
	log "micrun/internal/support/logger"
	"micrun/internal/support/panicsafe"
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

	// ClientAttached is fired when a live stdin writer is present.
	// A successful stdout write is not enough: start -d can briefly have a
	// reader. Create-time FIFO paths alone are not a client.
	ClientAttached

	// ClientDetached is fired on create-time stdin EOF (no writer) and
	// when a later non-TTY attach writer closes stdin.
	ClientDetached
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
	panicsafe.Go("io event bus auto-close", func() {
		<-ctx.Done()
		bus.Close()
	})
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
	panicsafe.Go("io event bus context unsubscribe", func() {
		select {
		case <-ctx.Done():
		case <-b.ctx.Done():
		}
		b.unsubscribe(eventType, ch)
	})
	return ch
}

func (b *EventBus) unsubscribe(eventType EventType, ch eventSubscriber) {
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
			// Close under the write lock: Publish sends under the read
			// lock, so a send can never race this close.
			close(ch)
			break
		}
	})
}

// Publish publishes an event to all subscribers.
func (b *EventBus) Publish(event Event) {
	// Deliver under the read lock: a send can then never race a channel
	// close, because Close/unsubscribe close channels only under the write
	// lock. (The previous snapshot-then-send-outside-the-lock design relied
	// on recover to swallow send-on-closed panics — which is a data race
	// under the Go memory model, flagged by the race detector.) Holding the
	// read lock across a blocked control-event send cannot stall Close:
	// Close cancels the bus context BEFORE taking the write lock, aborting
	// any blocked sender promptly.
	lockutil.WithReadLock(&b.mu, func() {
		if b.closed || b.ctx.Err() != nil {
			return
		}
		event.Timestamp = timex.Now(b.now)
		for _, ch := range b.subscribers[event.Type] {
			publishEvent(b.ctx, ch, event)
		}
	})
}

// controlEventSendTimeout bounds a blocking control-event delivery. The
// subscriber (handleIOEvents) drains a 16-slot buffered channel continuously;
// if it has not consumed anything for this long it is wedged, and keeping the
// IO copier worker blocked on the send forever (stdin/stdout pump dead, no
// exit/detach detection) is strictly worse than dropping the event loudly.
const controlEventSendTimeout = 5 * time.Second

func isControlEvent(t EventType) bool {
	switch t {
	case ExitCommandDetected, StdinClosed, DetachDetected, InterruptDetected, ClientAttached, ClientDetached:
		return true
	default:
		return false
	}
}

func publishEvent(ctx context.Context, ch eventSubscriber, event Event) {
	defer func() {
		_ = recover()
	}()

	if isControlEvent(event.Type) {
		// Control events must not be dropped under backpressure: losing
		// ClientAttached after CloseIO leaves attached=false and can
		// auto-close a live reattach. Block — but bounded: an unbounded
		// send deadlocks the IO copier forever when the subscriber is
		// wedged, and aborts on bus shutdown so Close is never held up.
		timer := time.NewTimer(controlEventSendTimeout)
		defer timer.Stop()
		select {
		case ch <- event:
		case <-ctx.Done():
		case <-timer.C:
			log.Errorf("[IO] control event %v for %s dropped: subscriber did not drain for %v",
				event.Type, event.ContainerID, controlEventSendTimeout)
		}
		return
	}

	select {
	case ch <- event:
	default:
		// Channel full, drop event to avoid blocking
	}
}

// publishEventSafe is kept for tests that exercise recover-on-closed-channel.
func publishEventSafe(ch eventSubscriber, event Event) {
	publishEvent(context.Background(), ch, event)
}

// Close closes the event bus and all subscriber channels.
func (b *EventBus) Close() {
	// Cancel first: a Publish blocked in a control-event send under the
	// read lock aborts via ctx.Done and releases the lock, so the write
	// lock below cannot deadlock behind it.
	b.cancel()
	b.withLock(func() {
		if b.closed {
			return
		}
		b.closed = true
		// Close channels under the write lock so concurrent Publish (which
		// sends under the read lock) cannot race the close.
		for _, chs := range b.subscribers {
			for _, ch := range chs {
				close(ch)
			}
		}
		b.subscribers = make(map[EventType][]eventSubscriber)
	})
}
