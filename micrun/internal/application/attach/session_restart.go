package attach

import (
	"context"
	"fmt"

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

	if hasFreshTTY {
		if err := request.manager.RestartWithTTYs(request.freshTTY.stdin, request.freshTTY.stdout); err != nil {
			request.freshTTY.close()
			return request.wrapError("restart IO manager", err)
		}
	} else if err := request.manager.Restart(); err != nil {
		return request.wrapError("restart IO manager", err)
	}

	if err := s.startSessionEventHandler(
		attachSessionContext(request.ctx, request.runtime),
		request.runtime,
		request.taskHandle,
		request.manager.EventStream(),
	); err != nil {
		request.manager.Stop()
		return request.wrapError("subscribe restarted IO session events", err)
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
