package attach

import (
	"context"
	"fmt"

	log "micrun/internal/support/logger"

	"micrun/internal/ports"
	"micrun/internal/support/validation"
)

type sessionRestartRequest struct {
	ctx          context.Context
	runtime      ports.TaskAttachRuntime
	taskHandle   ports.Task
	manager      ports.IOManager
	attachInfo   ports.AttachInfo
	freshTTY     freshTTYHandles
	errorContext string
}

func (s *Service) restartOrBootstrapSession(request sessionRestartRequest) error {
	hasFreshTTY := request.freshTTY.present()

	if validation.IsNil(request.manager) {
		if err := s.bootstrapSession(request.ctx, request.runtime, request.taskHandle, request.attachInfo); err != nil {
			if hasFreshTTY {
				request.freshTTY.close()
			}
			return request.wrapError("bootstrap IO session", err)
		}
		return nil
	}

	// Subscribe to the NEW event bus BEFORE the copier starts publishing,
	// closing the window in which a self-stopping control event (detach/exit)
	// could be permanently lost. The hook fires after renewContext creates
	// the new bus but before copier.Start.
	subscribe := func(stream ports.IOEventStream) {
		if err := s.startSessionEventHandler(
			attachSessionContext(request.ctx, request.runtime),
			request.runtime,
			request.taskHandle,
			stream,
		); err != nil {
			// The hook runs inside Session's lock; we cannot return an error
			// from here, but subscribeSessionEvents only fails on nil stream
			// which cannot happen post-renew. Log as a safety net.
			log.Warnf("[ATTACH] subscribe hook failed for %s: %v", request.taskHandle.ID(), err)
		}
	}

	if hasFreshTTY {
		if err := request.manager.RestartWithSubscriber(request.freshTTY.stdin, request.freshTTY.stdout, subscribe); err != nil {
			request.freshTTY.close()
			return request.wrapError("restart IO manager", err)
		}
	} else {
		if err := request.manager.RestartWithSubscriber(nil, nil, subscribe); err != nil {
			return request.wrapError("restart IO manager", err)
		}
	}

	withTaskLock(request.runtime, func() {
		request.taskHandle.SetAttachInfo(&request.attachInfo)
	})
	return nil
}

func (request sessionRestartRequest) wrapError(action string, err error) error {
	if request.errorContext == "" || err == nil {
		return err
	}
	return fmt.Errorf("%s for %s: %w", action, request.errorContext, err)
}
