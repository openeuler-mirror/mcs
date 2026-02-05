// Package io provides the IO system for micrun container runtime.
// It handles bidirectional data copying between FIFOs and TTY for RTOS containers.
package io

import (
	"context"
	"sync"
	"time"
)

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
)

// Event represents an IO event.
type Event struct {
	Type        EventType
	ContainerID string
	Data        interface{}
	Timestamp   time.Time
}

// EventSubscriber is a channel that receives events.
type EventSubscriber chan Event

// EventBus provides a simple event bus for IO layer to publish events
// and shim layer to subscribe to events.
type EventBus struct {
	subscribers map[EventType][]EventSubscriber
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewEventBus creates a new event bus.
func NewEventBus(ctx context.Context) *EventBus {
	ctx, cancel := context.WithCancel(ctx)
	return &EventBus{
		subscribers: make(map[EventType][]EventSubscriber),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Subscribe subscribes to events of a specific type.
// Returns a channel that will receive events.
func (b *EventBus) Subscribe(eventType EventType) EventSubscriber {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(EventSubscriber, 16)
	b.subscribers[eventType] = append(b.subscribers[eventType], ch)

	// Start goroutine to clean up subscriber when context is canceled
	go func() {
		<-b.ctx.Done()
		b.unsubscribe(eventType, ch)
	}()

	return ch
}

// Publish publishes an event to all subscribers.
func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	event.Timestamp = time.Now()

	subs := b.subscribers[event.Type]
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
			// Channel full, drop event to avoid blocking
		}
	}
}

// Unsubscribe removes a subscriber.
func (b *EventBus) unsubscribe(eventType EventType, ch EventSubscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[eventType]
	for i, sub := range subs {
		if sub == ch {
			b.subscribers[eventType] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
}

// Close closes the event bus and all subscriber channels.
func (b *EventBus) Close() {
	b.cancel()

	b.mu.Lock()
	defer b.mu.Unlock()

	for _, subs := range b.subscribers {
		for _, ch := range subs {
			close(ch)
		}
	}
	b.subscribers = make(map[EventType][]EventSubscriber)
}

