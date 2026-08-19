package io

import (
	"context"
	"reflect"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	"micrun/internal/support/panicsafe"
)

var _ ports.IOSessionFactory = (*SessionFactory)(nil)

// SessionFactory adapts the concrete IO session implementation to the
// application-layer IO ports.
type SessionFactory struct{}

func NewFactory() *SessionFactory {
	return &SessionFactory{}
}

func (f *SessionFactory) NewSession(ctx context.Context, config ports.IOSessionConfig) (ports.IOManager, ports.IOEventStream, error) {
	ctx = contextx.OrBackground(ctx)
	session, err := NewSession(Config{
		Context:     ctx,
		ContainerID: config.ContainerID,
		StdinFIFO:   config.StdinFIFO,
		StdoutFIFO:  config.StdoutFIFO,
		StderrFIFO:  config.StderrFIFO,
		TTYIn:       config.TTYIn,
		TTYOut:      config.TTYOut,
		TTYErr:      config.TTYErr,
		Terminal:    config.Terminal,
		FilterNUL:   config.FilterNUL,
		ExecMode:    config.ExecMode,
		DetachKeys:  config.DetachKeys,
	})
	if err != nil {
		return nil, nil, err
	}

	return session, session.EventStream(), nil
}

func (f *SessionFactory) IsValidFIFOPath(path string) bool {
	return IsValidFIFOPath(path)
}

func (f *SessionFactory) GenerateFIFOPath(namespace, containerID, stream string) string {
	return GenerateStandardFIFOPath(namespace, containerID, stream)
}

type eventStream struct {
	ctx     context.Context
	bus     *EventBus
	session *Session
}

// Current reports whether this stream's bus is still the session's active
// bus. A session restart (renewContext) closes the old bus and installs a
// new one; events already queued on the old bus keep draining to old
// subscribers, and handlers must not act on them.
func (s *eventStream) Current() bool {
	if s == nil || s.session == nil {
		return false
	}
	s.session.mu.Lock()
	defer s.session.mu.Unlock()
	return s.session.eventBus != nil && s.session.eventBus == s.bus
}

type adapterEventSource struct {
	eventType ports.IOEventType
	events    EventSubscriber
}

func (s *eventStream) subscribeAdapterEvent(eventType ports.IOEventType) (adapterEventSource, bool) {
	if s == nil || s.bus == nil {
		return adapterEventSource{}, false
	}

	ctx := s.context()
	if ctx.Err() != nil {
		return adapterEventSource{}, false
	}
	adapterType, ok := eventTypeToAdapter(eventType)
	if !ok {
		return adapterEventSource{}, false
	}

	return adapterEventSource{
		eventType: eventType,
		events:    s.bus.SubscribeContext(ctx, adapterType),
	}, true
}

func (s *eventStream) SubscribeMany(eventTypes ...ports.IOEventType) ports.IOEventSubscriber {
	out := make(chan ports.IOEvent, eventChannelBufferSize)
	if s == nil || s.bus == nil || len(eventTypes) == 0 {
		close(out)
		return out
	}
	ctx := s.context()

	unique := make(map[ports.IOEventType]struct{}, len(eventTypes))
	sources := make([]adapterEventSource, 0, len(eventTypes))

	for _, eventType := range eventTypes {
		if _, ok := unique[eventType]; ok {
			continue
		}
		unique[eventType] = struct{}{}

		source, active := s.subscribeAdapterEvent(eventType)
		if !active {
			continue
		}
		sources = append(sources, source)
	}

	if len(sources) == 0 {
		close(out)
		return out
	}

	// Single forwarder goroutine multiplexing all sources: per-source
	// goroutines used to race each other into `out`, so a ClientDetached
	// published after a ClientAttached (serialized inside the copier) could
	// overtake it after the fan-in, flipping the consumer's attached view
	// (stuck-true suspends auto-close forever; stuck-false kills a live
	// session after the grace window).
	cases := make([]reflect.SelectCase, 0, len(sources)+1)
	for _, source := range sources {
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(source.events),
		})
	}
	cases = append(cases, reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(ctx.Done()),
	})
	panicsafe.Go("io event fan-in", func() {
		for {
			chosen, val, ok := reflect.Select(cases)
			if chosen == len(cases)-1 {
				close(out)
				return
			}
			if !ok {
				cases[chosen].Chan = reflect.ValueOf(nil)
				allDone := true
				for _, c := range cases[:len(cases)-1] {
					if c.Chan.IsValid() {
						allDone = false
						break
					}
				}
				if allDone {
					close(out)
					return
				}
				continue
			}
			event := val.Interface().(Event)
			converted := ports.IOEvent{
				Type:        sources[chosen].eventType,
				ContainerID: event.ContainerID,
				Err:         event.Err,
				Timestamp:   event.Timestamp,
			}
			select {
			case out <- converted:
			case <-ctx.Done():
				// The subscriber's pump blocks on `out` until it closes
				// (its own context is the shim root, not this session
				// context), so the cancelled sender must close `out` here —
				// returning without closing leaks the pump goroutine.
				close(out)
				return
			}
		}
	})

	return out
}

func (s *eventStream) context() context.Context {
	if s == nil {
		return context.Background()
	}
	return contextx.OrBackground(s.ctx)
}

type ioEventTypeMapping struct {
	port    ports.IOEventType
	adapter EventType
}

var ioEventTypeMappingTable = [...]ioEventTypeMapping{
	{port: ports.IOEventExitCommand, adapter: ExitCommandDetected},
	{port: ports.IOEventError, adapter: IOError},
	{port: ports.IOEventTTYReady, adapter: TTYReady},
	{port: ports.IOEventStdinClosed, adapter: StdinClosed},
	{port: ports.IOEventDetach, adapter: DetachDetected},
	{port: ports.IOEventInterrupt, adapter: InterruptDetected},
	{port: ports.IOEventClientAttached, adapter: ClientAttached},
	{port: ports.IOEventClientDetached, adapter: ClientDetached},
}

func eventTypeToAdapter(eventType ports.IOEventType) (EventType, bool) {
	for _, mapping := range ioEventTypeMappingTable {
		if mapping.port == eventType {
			return mapping.adapter, true
		}
	}
	return 0, false
}

func eventTypeToPort(eventType EventType) (ports.IOEventType, bool) {
	for _, mapping := range ioEventTypeMappingTable {
		if mapping.adapter == eventType {
			return mapping.port, true
		}
	}
	return 0, false
}
