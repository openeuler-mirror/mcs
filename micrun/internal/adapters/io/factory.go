package io

import (
	"context"
	"sync"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
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
	ctx context.Context
	bus *EventBus
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

	var wg sync.WaitGroup
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

	wg.Add(len(sources))
	for _, source := range sources {
		source := source
		go func() {
			defer wg.Done()
			for event := range source.events {
				converted := ports.IOEvent{
					Type:        source.eventType,
					ContainerID: event.ContainerID,
					Err:         event.Err,
					Timestamp:   event.Timestamp,
				}
				select {
				case out <- converted:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func (s *eventStream) context() context.Context {
	if s == nil {
		return contextx.OrBackground(nil)
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
