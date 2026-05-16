package task

import (
	"context"
	"time"

	"micrun/internal/application/exitstatus"
	"micrun/internal/ports"
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

	if snapshot.isSandboxTask {
		if err := s.teardownSandbox(ctx, runtime); err != nil {
			return nil, err
		}
	}

	if err := runtime.CleanupTask(ctx, snapshot.taskHandle); err != nil {
		return nil, err
	}

	var output *DeleteOutput
	withTaskLock(runtime, func() {
		output = deleteOutput(runtime, snapshot.taskHandle)
		runtime.DeleteTask(snapshot.taskHandle.ID())
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
			s.forceStopIfActive(taskHandle)
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
	if taskHandle.Status() == task.Status_RUNNING || taskHandle.Status() == task.Status_CREATED {
		taskHandle.SetStatus(task.Status_STOPPED)
		taskHandle.SetExitInfo(exitstatus.Interrupt(), s.clockNow())
		closeTaskExitSignal(taskHandle)
	}
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
