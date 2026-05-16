package task

import (
	"context"
	"fmt"

	"micrun/internal/ports"
	"micrun/internal/support/validation"

	"github.com/containerd/containerd/api/types/task"
)

func (s *Service) State(ctx context.Context, runtime ports.TaskQueryRuntime, in StateInput) (*StateOutput, error) {
	var err error
	ctx, err = s.prepareOperation(ctx, runtime)
	if err != nil {
		return nil, err
	}

	taskState, err := s.snapshotTaskForState(runtime, in.ID, in.ExecID)
	if err != nil {
		return nil, err
	}

	if taskState.shouldRefresh {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.refreshTaskStatusForQuery(ctx, runtime, taskState.taskHandle)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	var output *StateOutput
	withTaskLock(runtime, func() {
		output = stateOutput(runtime, taskState.taskHandle, in.ExecID)
	})
	return output, nil
}

func shouldRefreshTaskStatusForQuery(runtime ports.TaskQueryRuntime, taskHandle ports.Task) bool {
	if taskHandle.Status() == task.Status_STOPPED {
		return false
	}
	if validation.IsNil(runtime.Sandbox()) {
		return false
	}
	return true
}

type taskQueryState struct {
	taskHandle    ports.Task
	shouldRefresh bool
}

func (s *Service) snapshotTaskForState(runtime ports.TaskQueryRuntime, id, execID string) (taskQueryState, error) {
	var snapshot taskQueryState

	_, err := s.snapshotPrimaryTaskWith(runtime, id, execID, func(taskHandle ports.Task) error {
		snapshot = taskQueryState{
			taskHandle:    taskHandle,
			shouldRefresh: shouldRefreshTaskStatusForQuery(runtime, taskHandle),
		}
		return nil
	})
	return snapshot, err
}

func (s *Service) refreshTaskStatusForQuery(ctx context.Context, runtime ports.TaskQueryRuntime, taskHandle ports.Task) {
	status, err := runtime.QueryTaskStatus(ctx, taskHandle.ID())
	if err != nil {
		status = task.Status_UNKNOWN
	}

	withTaskLock(runtime, func() {
		if taskHandle.Status() == task.Status_STOPPED {
			return
		}
		taskHandle.SetStatus(status)
	})
}

func (s *Service) Wait(ctx context.Context, runtime ports.TaskWaitRuntime, in WaitInput) (*WaitOutput, error) {
	if err := s.requireRuntime(runtime); err != nil {
		return nil, err
	}
	var err error
	ctx, err = activeTaskContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("wait canceled: %w", err)
	}

	taskState, err := s.snapshotTaskForWait(runtime, in.ID, in.ExecID)
	if err != nil {
		return nil, err
	}

	if !taskState.exited {
		if taskState.exitChan == nil {
			return nil, fmt.Errorf("wait failed: task %s has no exit channel", in.ID)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait canceled: %w", ctx.Err())
		case <-taskState.exitChan:
		}
	}

	var output *WaitOutput
	withTaskLock(runtime, func() {
		output = waitOutput(taskState.taskHandle)
	})
	return output, nil
}

type taskWaitState struct {
	taskHandle ports.Task
	exited     bool
	exitChan   chan struct{}
}

func (s *Service) snapshotTaskForWait(runtime ports.TaskWaitRuntime, id, execID string) (taskWaitState, error) {
	var snapshot taskWaitState

	_, err := s.snapshotPrimaryTaskWith(runtime, id, execID, func(taskHandle ports.Task) error {
		snapshot = taskWaitState{
			taskHandle: taskHandle,
			exited:     taskHandle.Status() == task.Status_STOPPED,
			exitChan:   taskHandle.ExitChan(),
		}
		return nil
	})
	return snapshot, err
}
