package io

import (
	"context"
	"testing"
	"time"

	"micrun/internal/domain/console"
)

func TestPublishConsoleEventDispatchesMappedEvents(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()
	copier := NewCopier(Config{
		ContainerID: "console-events",
		EventBus:    bus,
	})
	defer copier.finishStop(0)

	exit := bus.Subscribe(ExitCommandDetected)
	detach := bus.Subscribe(DetachDetected)
	interrupt := bus.Subscribe(InterruptDetected)

	copier.publishConsoleEvent(console.EventExitCommand)
	copier.publishConsoleEvent(console.EventDetach)
	copier.publishConsoleEvent(console.EventInterrupt)

	assertEventType(t, exit, ExitCommandDetected)
	assertEventType(t, detach, DetachDetected)
	assertEventType(t, interrupt, InterruptDetected)
}

func TestPublishConsoleEventIgnoresUnknownEvents(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()
	copier := NewCopier(Config{
		ContainerID: "unknown-console-event",
		EventBus:    bus,
	})
	defer copier.finishStop(0)
	events := bus.Subscribe(ExitCommandDetected)

	copier.publishConsoleEvent(console.EventKind(99))

	select {
	case <-events:
		t.Fatal("unexpected event for unsupported console event kind")
	case <-time.After(200 * time.Millisecond):
	}
}

func assertEventType(t *testing.T, ch EventSubscriber, want EventType) {
	t.Helper()
	select {
	case event := <-ch:
		if event.Type != want {
			t.Fatalf("event type = %v, want %v", event.Type, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for event %v", want)
	}
}
