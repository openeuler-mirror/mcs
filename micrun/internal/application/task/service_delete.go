package task

import (
	"context"
	"time"

	"micrun/internal/application/exitstatus"
	"micrun/internal/ports"
	er "micrun/internal/support/errors"
	"micrun/internal/support/validation"

	"github.com/containerd/containerd/api/types/task"
)

func (s *Service) Delete(ctx context.Context, runtime ports.TaskDeleteRuntime, in DeleteInput) (*DeleteOutput, error) {
	var err error
	ctx, err = s.prepareOperation(ctx, runtime)
	if err != nil {
		return nil, err
	}

	snapshot, err := s.snapshotTaskForDelete(runtime, in.ID, in.ExecID)
	if err != nil {
		return nil, err
	}
	if !snapshot.found {
		var shimPID uint32
		withTaskLock(runtime, func() {
			shimPID = runtime.ShimPID()
		})
		return notFoundDeleteOutputAt(in.ID, shimPID, s.clockNow()), nil
	}

	// Exclude a concurrent Start for the same id: Start holds a sandbox
	// snapshot across the unlocked guest bring-up window, and Delete used to
	// remove the task then tear down while Start could still recreate the
	// domain afterward — leaving an untracked Xen/micad guest. Fail fast so
	// the client retries Delete after Start finishes (or fails).
	if !s.claimLifecycle(snapshot.taskHandle.ID()) {
		return nil, er.Wrapf(er.ContainerNotReady, "task %s has a lifecycle operation in progress", snapshot.taskHandle.ID())
	}
	defer s.releaseLifecycle(snapshot.taskHandle.ID())

	// Claim the task up front: a concurrent Delete for the same id (client
	// timeout retry while the first teardown is still running) must not
	// also run teardown — that would double-emit TaskDelete and race the
	// sandbox teardown. The entry is restored if the teardown fails so a
	// retry can still clean up.
	withTaskLock(runtime, func() {
		runtime.DeleteTask(snapshot.taskHandle.ID())
	})
	restoreTask := func() {
		withTaskLock(runtime, func() {
			runtime.SaveTask(snapshot.taskHandle.ID(), snapshot.taskHandle)
		})
	}

	if snapshot.isSandboxTask {
		if err := s.teardownSandbox(ctx, runtime); err != nil {
			restoreTask()
			return nil, err
		}
	}

	// CleanupTask tears down pod containers (Stop/Delete container). Must
	// succeed before forceStopIfActive closes the exit channel: that close
	// is irreversible (sync.Once), and restoreTask after a failed cleanup
	// would leave Wait broken while the domain may still be alive.
	if err := runtime.CleanupTask(ctx, snapshot.taskHandle); err != nil {
		restoreTask()
		return nil, err
	}

	withTaskLock(runtime, func() {
		s.forceStopIfActive(snapshot.taskHandle)
	})

	var output *DeleteOutput
	withTaskLock(runtime, func() {
		output = deleteOutput(runtime, snapshot.taskHandle)
	})
	return output, nil
}

type deleteTaskSnapshot struct {
	taskHandle    ports.Task
	isSandboxTask bool
	found         bool
}

func (s *Service) snapshotTaskForDelete(runtime ports.TaskDeleteRuntime, id, execID string) (deleteTaskSnapshot, error) {
	var snapshot deleteTaskSnapshot

	err := withTaskLockError(runtime, func() error {
		taskHandle, found := s.lookupTask(runtime, id)
		if found {
			snapshot.found = true
			snapshot.taskHandle = taskHandle
			if err := rejectExecID(execID); err != nil {
				return err
			}
			snapshot.isSandboxTask = taskHandle.CanBeSandbox()
		}
		return nil
	})
	return snapshot, err
}

func deleteOutput(runtime ports.TaskIdentity, taskHandle ports.Task) *DeleteOutput {
	return &DeleteOutput{
		ContainerID: taskHandle.ID(),
		ExitStatus:  taskHandle.ExitStatus(),
		ExitedAt:    taskHandle.ExitTime(),
		Pid:         taskPIDOrShim(runtime, taskHandle),
	}
}

func notFoundDeleteOutputAt(id string, shimPID uint32, exitedAt time.Time) *DeleteOutput {
	return &DeleteOutput{
		ContainerID: id,
		ExitStatus:  0,
		ExitedAt:    exitedAt,
		Pid:         shimPID,
		NotFound:    true,
	}
}

func (s *Service) forceStopIfActive(taskHandle ports.Task) {
	// PAUSED included: a Delete of a paused container must close its exit
	// channel too, otherwise the exit watcher stays blocked and later emits
	// a bogus TaskExit (fabricated 130) after the TaskDelete event.
	// PAUSING included: a Delete racing an in-flight Pause (whose slow guest
	// RPC runs unlocked) would otherwise leave the task in PAUSING forever —
	// exit channel never closed, Wait RPCs blocked, TaskDelete carrying a
	// zero exit time.
	switch taskHandle.Status() {
	case task.Status_RUNNING, task.Status_CREATED, task.Status_PAUSED, task.Status_PAUSING:
		s.finalizeTaskStopped(taskHandle)
	}
}

// finalizeTaskStopped transitions a non-STOPPED task to STOPPED with the full
// terminal-state contract (exit info recorded — fabricated Interrupt when no
// real exit path pre-wrote one — and the exit channel closed so blocked Wait
// RPCs unblock). Delegates to the canonical ports.FinalizeTaskStopped so
// every finalize path in the codebase shares one implementation.
// Caller must hold the task lock.
func (s *Service) finalizeTaskStopped(taskHandle ports.Task) {
	ports.FinalizeTaskStopped(taskHandle, exitstatus.Interrupt(), s.clockNow())
}

func (s *Service) teardownSandbox(ctx context.Context, runtime ports.TaskDeleteRuntime) error {
	var sandbox ports.Sandbox
	withTaskLock(runtime, func() {
		sandbox = runtime.Sandbox()
		runtime.SetSandbox(nil)
	})

	if err := stopAndDeleteSandbox(ctx, sandbox, "during delete"); err != nil {
		withTaskLock(runtime, func() {
			if validation.IsNil(runtime.Sandbox()) {
				runtime.SetSandbox(sandbox)
			}
		})
		return err
	}
	return nil
}
