package attach

import (
	"context"
	"fmt"

	"micrun/internal/ports"
)

func (s *Service) sessionEventTypesSnapshot() []ports.IOEventType {
	if s == nil {
		return nil
	}
	return s.eventProfile.eventTypesSnapshot()
}

func (s *Service) subscribeSessionEvents(stream ports.IOEventStream) (ports.IOEventSubscriber, error) {
	if s == nil {
		return nil, fmt.Errorf("attach service is required")
	}
	if stream == nil {
		return nil, fmt.Errorf("IO event stream is required")
	}
	events := s.sessionEventTypesSnapshot()
	subscriber := stream.SubscribeMany(events...)
	if subscriber == nil {
		return nil, fmt.Errorf("IO event subscriber is required")
	}
	return subscriber, nil
}

func (s *Service) startSessionEventHandler(
	ctx context.Context,
	runtime ports.TaskAttachRuntime,
	taskHandle ports.Task,
	stream ports.IOEventStream,
) error {
	events, err := s.subscribeSessionEvents(stream)
	if err != nil {
		return err
	}
	go s.handleIOEvents(ctx, runtime, taskHandle, events)
	return nil
}
