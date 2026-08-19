package recovery

import (
	"context"
	"time"

	"micrun/internal/application/exitstatus"
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

// Recover restores persisted sandbox/task state. exitWatcher, when non-nil,
// is invoked for each recovered RUNNING task so the shim can observe guest
// exit for tasks that were alive when the shim restarted (the normal
// lifecycle.Start path spawns this watcher; recovery must do the same or
// Wait RPCs on recovered running tasks would block forever).
func (s *Service) Recover(ctx context.Context, runtime ports.RecoveryRuntime, backend ports.RecoveryBackend, taskFactory func(spec ports.RecoveredTask) ports.Task, exitWatcher func(task ports.Task)) error {
	operation, ok, err := newRecoveryOperation(ctx, runtime, backend, taskFactory)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return operation.runWithExitWatcher(exitWatcher)
}

func restoreRecoveredTasks(ctx context.Context, runtime ports.RecoveryRuntime, restoredTasks []ports.RecoveredTask, taskFactory func(spec ports.RecoveredTask) ports.Task, exitWatcher func(task ports.Task)) error {
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
		// Spawn the exit watcher for recovered live tasks (Running or Paused)
		// so guest exit closes the exit channel; otherwise Wait hangs forever.
		if exitWatcher != nil && (spec.IsRunning || spec.IsPaused) && !spec.IsStopped {
			exitWatcher(taskHandle)
		}
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
	// A recovered STOPPED task has no recorded exit info (persisted state
	// only carries container state). Fabricate a non-zero interrupted status
	// at recovery time, mirroring completeExitedTask's guest-exit policy, so
	// Wait/Delete/State do not report a clean exit with a zero timestamp —
	// kubelet would mistake a crashed container for a clean stop.
	if spec.IsStopped {
		taskHandle.SetExitInfo(exitstatus.Interrupt(), time.Now())
	}
	return taskHandle
}

func recoveredTaskStatus(spec ports.RecoveredTask) task.Status {
	if spec.IsStopped {
		return task.Status_STOPPED
	}
	if spec.IsPaused {
		return task.Status_PAUSED
	}
	if spec.IsRunning {
		return task.Status_RUNNING
	}
	return task.Status_CREATED
}
