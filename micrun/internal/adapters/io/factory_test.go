package io

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"micrun/internal/ports"
)

func TestEventStreamSubscribeManyForwardsConfiguredEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := NewEventBus(ctx)
	t.Cleanup(bus.Close)
	stream := &eventStream{ctx: ctx, bus: bus}
	events := stream.SubscribeMany(ports.IOEventExitCommand, ports.IOEventDetach, ports.IOEventError)

	bus.Publish(Event{Type: ExitCommandDetected, ContainerID: "c1"})
	got := receiveIOEvent(t, events)
	if got.Type != ports.IOEventExitCommand || got.ContainerID != "c1" {
		t.Fatalf("forwarded event = %+v, want exit event for c1", got)
	}

	bus.Publish(Event{Type: DetachDetected, ContainerID: "c1"})
	got = receiveIOEvent(t, events)
	if got.Type != ports.IOEventDetach || got.ContainerID != "c1" {
		t.Fatalf("forwarded event = %+v, want detach event for c1", got)
	}

	expectedErr := errors.New("io failed")
	bus.Publish(Event{Type: IOError, ContainerID: "c1", Err: expectedErr})
	got = receiveIOEvent(t, events)
	if got.Type != ports.IOEventError || got.ContainerID != "c1" || got.Err != expectedErr {
		t.Fatalf("forwarded event = %+v, want IO error event for c1", got)
	}
}

func TestEventStreamSubscribeManyReturnsClosedWhenStreamContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	bus := NewEventBus(ctx)
	t.Cleanup(bus.Close)
	stream := &eventStream{ctx: ctx, bus: bus}
	events := stream.SubscribeMany(ports.IOEventExitCommand)

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected event channel to be closed when context already canceled")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed channel")
	}
}

func TestEventStreamSubscribeManyClosesOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bus := NewEventBus(ctx)
	stream := &eventStream{ctx: ctx, bus: bus}
	events := stream.SubscribeMany(ports.IOEventExitCommand, ports.IOEventDetach)

	cancel()
	bus.Close()

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected merged event stream to close after context cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for merged event stream to close")
	}
}

func TestEventStreamSubscribeManyCancelsUnderlyingSubscribers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bus := NewEventBus(context.Background())
	defer bus.Close()
	stream := &eventStream{ctx: ctx, bus: bus}
	events := stream.SubscribeMany(ports.IOEventDetach)
	if count := eventSubscriberCount(bus, DetachDetected); count != 1 {
		t.Fatalf("underlying detach subscriber count = %d, want 1", count)
	}

	cancel()

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected merged event stream to close after context cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for merged event stream to close")
	}
	if count := eventSubscriberCount(bus, DetachDetected); count != 0 {
		t.Fatalf("underlying detach subscriber count after cancel = %d, want 0", count)
	}
}

func TestSessionFactoryPropagatesContextToSessionLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	factory := NewFactory()

	manager, stream, err := factory.NewSession(ctx, ports.IOSessionConfig{
		ContainerID: "c1",
		StdinFIFO:   "binary://stdin",
		StdoutFIFO:  "fd://1",
		StderrFIFO:  "socket:///run/micrun/stderr.sock",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session, ok := manager.(*Session)
	if !ok {
		t.Fatalf("manager type = %T, want *Session", manager)
	}
	t.Cleanup(session.copier.Stop)

	events := stream.SubscribeMany(ports.IOEventExitCommand)
	cancel()

	assertContextDone(t, session.ctx)
	assertContextDone(t, session.copier.ctx)
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected event stream to close after context cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event stream to close")
	}
}

func TestEventTypeMappingsRejectUnknownValues(t *testing.T) {
	if _, ok := eventTypeToAdapter(ports.IOEventType(99)); ok {
		t.Fatal("unknown port event type should be rejected")
	}
	if _, ok := eventTypeToPort(EventType(99)); ok {
		t.Fatal("unknown adapter event type should be rejected")
	}
}

func TestEventTypeMappingsAreBidirectional(t *testing.T) {
	for _, mapping := range ioEventTypeMappings() {
		port := mapping.port
		adapter := mapping.adapter

		t.Run(fmt.Sprintf("port-to-adapter/%d", port), func(t *testing.T) {
			got, ok := eventTypeToAdapter(port)
			if !ok {
				t.Fatalf("eventTypeToAdapter(%v) was not found", port)
			}
			if got != adapter {
				t.Fatalf("eventTypeToAdapter(%v) = %v, want %v", port, got, adapter)
			}
		})

		t.Run(fmt.Sprintf("adapter-to-port/%d", adapter), func(t *testing.T) {
			got, ok := eventTypeToPort(adapter)
			if !ok {
				t.Fatalf("eventTypeToPort(%v) was not found", adapter)
			}
			if got != port {
				t.Fatalf("eventTypeToPort(%v) = %v, want %v", adapter, got, port)
			}
		})
	}
}

func TestEventTypeMappingsAreUnique(t *testing.T) {
	seenPorts := make(map[ports.IOEventType]struct{}, len(ioEventTypeMappings()))
	seenAdapters := make(map[EventType]struct{}, len(ioEventTypeMappings()))
	for _, mapping := range ioEventTypeMappings() {
		port := mapping.port
		adapter := mapping.adapter

		if _, ok := seenPorts[port]; ok {
			t.Fatalf("duplicated port event type in mappings: %v", port)
		}
		if _, ok := seenAdapters[adapter]; ok {
			t.Fatalf("duplicated adapter event type in mappings: %v", adapter)
		}
		seenPorts[port] = struct{}{}
		seenAdapters[adapter] = struct{}{}
	}
}

func ioEventTypeMappings() []struct {
	port    ports.IOEventType
	adapter EventType
} {
	return []struct {
		port    ports.IOEventType
		adapter EventType
	}{
		{ports.IOEventExitCommand, ExitCommandDetected},
		{ports.IOEventError, IOError},
		{ports.IOEventTTYReady, TTYReady},
		{ports.IOEventStdinClosed, StdinClosed},
		{ports.IOEventDetach, DetachDetected},
		{ports.IOEventInterrupt, InterruptDetected},
	}
}

func TestEventStreamSubscribeManyClosesForUnknownEventType(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := NewEventBus(ctx)
	t.Cleanup(bus.Close)
	stream := &eventStream{ctx: ctx, bus: bus}
	events := stream.SubscribeMany(ports.IOEventType(99))

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected unknown event stream to close")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for unknown event stream to close")
	}
}

func TestEventStreamSubscribeManyDeduplicatesEventTypeSubscriptions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := NewEventBus(ctx)
	t.Cleanup(bus.Close)
	stream := &eventStream{ctx: ctx, bus: bus}
	events := stream.SubscribeMany(ports.IOEventDetach, ports.IOEventDetach, ports.IOEventDetach)

	bus.Publish(Event{Type: DetachDetected, ContainerID: "c1"})

	got := receiveIOEvent(t, events)
	if got.Type != ports.IOEventDetach {
		t.Fatalf("forwarded event type = %v, want %v", got.Type, ports.IOEventDetach)
	}

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected single detach event after deduplicated subscriptions")
		}
	case <-time.After(80 * time.Millisecond):
	}
}

func TestEventStreamSubscribeManyIgnoresUnknownAndDedupes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := NewEventBus(ctx)
	t.Cleanup(bus.Close)
	stream := &eventStream{ctx: ctx, bus: bus}
	events := stream.SubscribeMany(ports.IOEventType(99), ports.IOEventDetach, ports.IOEventDetach)

	bus.Publish(Event{Type: DetachDetected, ContainerID: "c1"})

	got := receiveIOEvent(t, events)
	if got.Type != ports.IOEventDetach {
		t.Fatalf("forwarded event type = %v, want %v", got.Type, ports.IOEventDetach)
	}

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected only one detach event for duplicated subscription")
		}
	case <-time.After(80 * time.Millisecond):
	}
}

func receiveIOEvent(t *testing.T, events ports.IOEventSubscriber) ports.IOEvent {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event stream closed unexpectedly")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for IO event")
		return ports.IOEvent{}
	}
}
