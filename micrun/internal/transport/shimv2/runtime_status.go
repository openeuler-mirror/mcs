package shim

import (
	"context"

	cntr "micrun/internal/domain/container"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"

	"github.com/containerd/containerd/api/types/task"
)

func (s *shimService) getContainerStatus(ctx context.Context, id string) (task.Status, error) {
	sandbox, ok := s.currentSandbox()
	if !ok {
		log.Debugf("Sandbox is nil, cannot get status for container %s", id)
		return task.Status_UNKNOWN, er.SandboxNotFound
	}
	cs, err := sandbox.StatusContainer(ctx, id)
	if err != nil {
		return task.Status_UNKNOWN, err
	}

	return taskStatusFromContainerState(cs.State.State), nil
}

func taskStatusFromContainerState(state cntr.StateString) task.Status {
	switch state {
	case cntr.StateReady:
		return task.Status_CREATED
	case cntr.StateRunning:
		return task.Status_RUNNING
	case cntr.StatePaused:
		return task.Status_PAUSED
	case cntr.StateStopped:
		return task.Status_STOPPED
	default:
		return task.Status_UNKNOWN
	}
}
