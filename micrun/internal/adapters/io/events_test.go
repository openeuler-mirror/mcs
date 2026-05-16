package io

import (
	"context"
	"testing"
	"time"
)

func TestPublishEventSafeRecoversFromClosedSubscriber(t *testing.T) {
	ch := make(eventSubscriber, 1)
	close(ch)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("publishEventSafe did not recover: %v", r)
		}
	}()

	publishEventSafe(ch, Event{
		Type:        ExitCommandDetected,
		ContainerID: "container1",
	})
}

func TestEventBusPublishConcurrentCloseDoesNotPanic(t *testing.T) {
	for i := 0; i < 200; i++ {
		bus := NewEventBus(context.Background())
		_ = bus.Subscribe(ExitCommandDetected)

		done := make(chan struct{})
		panicErr := make(chan interface{}, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					panicErr <- r
				}
				close(done)
			}()
			bus.Publish(Event{Type: ExitCommandDetected, ContainerID: "container1"})
		}()
		bus.Close()

		<-done
		select {
		case err := <-panicErr:
			t.Fatalf("Publish panicked during close race: %v", err)
		default:
		}
	}
}

func TestEventBusSubscribeAfterCloseReturnsClosedChannel(t *testing.T) {
	bus := NewEventBus(context.Background())
	bus.Close()

	events := bus.Subscribe(ExitCommandDetected)
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected subscription after Close to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed subscription")
	}
}

func TestEventBusSubscribeContextRemovesSubscriberOnCancel(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	events := bus.SubscribeContext(ctx, ExitCommandDetected)
	if count := eventSubscriberCount(bus, ExitCommandDetected); count != 1 {
		t.Fatalf("subscriber count = %d, want 1", count)
	}

	cancel()

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected subscription to close after context cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for context subscription to close")
	}
	if count := eventSubscriberCount(bus, ExitCommandDetected); count != 0 {
		t.Fatalf("subscriber count after cancellation = %d, want 0", count)
	}
}

func TestEventBusSubscribeContextCanceledBeforeSubscribeDoesNotRegister(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	events := bus.SubscribeContext(ctx, ExitCommandDetected)
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected subscription to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed subscription")
	}
	if count := eventSubscriberCount(bus, ExitCommandDetected); count != 0 {
		t.Fatalf("subscriber count = %d, want 0", count)
	}
}

func TestEventBusCloseIsIdempotentAndPublishAfterCloseNoops(t *testing.T) {
	bus := NewEventBus(context.Background())
	events := bus.Subscribe(ExitCommandDetected)

	bus.Close()
	bus.Close()
	bus.Publish(Event{Type: ExitCommandDetected, ContainerID: "container1"})

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected subscription to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed subscription")
	}
}

func eventSubscriberCount(bus *EventBus, eventType EventType) int {
	bus.mu.RLock()
	defer bus.mu.RUnlock()
	return len(bus.subscribers[eventType])
}

func TestEventBusParentCancelClosesSubscribers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bus := NewEventBus(ctx)
	events := bus.Subscribe(ExitCommandDetected)

	cancel()

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected subscription to close after parent context cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscription close after context cancellation")
	}
}

func TestEventBusNilContextUsesBackground(t *testing.T) {
	bus := NewEventBus(nil)
	defer bus.Close()

	events := bus.Subscribe(ExitCommandDetected)
	bus.Publish(Event{Type: ExitCommandDetected, ContainerID: "container1"})

	select {
	case event := <-events:
		if event.ContainerID != "container1" {
			t.Fatalf("event container = %q, want container1", event.ContainerID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event from nil-context bus")
	}
}

func TestEventBusPublishUsesInjectedClock(t *testing.T) {
	now := time.Date(2026, 4, 27, 13, 14, 15, 0, time.UTC)
	bus := newEventBus(context.Background(), func() time.Time { return now })
	defer bus.Close()
	events := bus.Subscribe(ExitCommandDetected)

	bus.Publish(Event{Type: ExitCommandDetected, ContainerID: "container1"})

	select {
	case event := <-events:
		if !event.Timestamp.Equal(now) {
			t.Fatalf("event timestamp = %s, want %s", event.Timestamp, now)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}
