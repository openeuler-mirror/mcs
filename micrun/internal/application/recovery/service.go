package recovery

import (
	"context"

	"micrun/internal/ports"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"

	"github.com/containerd/containerd/api/types/task"
)

// Service coordinates orphan cleanup and sandbox/task reconstruction during shim startup.
type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Recover(ctx context.Context, runtime ports.RecoveryRuntime, backend ports.RecoveryBackend, taskFactory func(spec ports.RecoveredTask) ports.Task) error {
	operation, ok, err := newRecoveryOperation(ctx, runtime, backend, taskFactory)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return operation.run()
}

func restoreRecoveredTasks(ctx context.Context, runtime ports.RecoveryRuntime, restoredTasks []ports.RecoveredTask, taskFactory func(spec ports.RecoveredTask) ports.Task) error {
	for _, spec := range restoredTasks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validRecoveredTaskSpec(spec) {
			log.Debugf("skipping recovered task with invalid id %q", spec.ID)
			continue
		}
		taskHandle := recoveredTaskHandle(spec, taskFactory)
		if validation.IsNil(taskHandle) {
			continue
		}
		runtime.SaveTask(spec.ID, taskHandle)
	}

	return nil
}

func validRecoveredTaskSpec(spec ports.RecoveredTask) bool {
	return validation.IsSinglePathSegment(spec.ID)
}

func recoveredTaskHandle(spec ports.RecoveredTask, taskFactory func(spec ports.RecoveredTask) ports.Task) ports.Task {
	taskHandle := taskFactory(spec)
	if validation.IsNil(taskHandle) {
		return nil
	}
	taskHandle.SetStatus(recoveredTaskStatus(spec))
	return taskHandle
}

func recoveredTaskStatus(spec ports.RecoveredTask) task.Status {
	if spec.IsRunning {
		return task.Status_RUNNING
	}
	return task.Status_CREATED
}
