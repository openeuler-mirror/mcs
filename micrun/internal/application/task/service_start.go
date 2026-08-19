package task

import (
	"context"

	"micrun/internal/ports"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"

	"github.com/containerd/containerd/api/types/task"
)

func (s *Service) Start(ctx context.Context, runtime ports.TaskStartRuntime, in StartInput) (*StartOutput, error) {
	log.Tracef("[START METHOD] Start called: id=%s exec=%s", in.ID, in.ExecID)
	var err error
	ctx, err = s.prepareOperation(ctx, runtime)
	if err != nil {
		return nil, err
	}

	startSnapshot, err := s.snapshotTaskForStart(runtime, in.ID, in.ExecID)
	if err != nil {
		return nil, err
	}
	taskHandle := startSnapshot.taskHandle

	respPid := runtime.ShimPID()
	if startSnapshot.shouldReattach {
		// Reattach must still claim lifecycle: without it, Delete can tear
		// down the task/domain while EnsureAttach is opening TTYs, and Start
		// returns success for a task that no longer exists.
		if !s.claimLifecycle(in.ID) {
			return nil, er.Wrapf(er.ContainerNotReady, "task %s has a lifecycle operation in progress", in.ID)
		}
		defer s.releaseLifecycle(in.ID)

		status := taskStatusOf(runtime, taskHandle)
		if status != task.Status_RUNNING {
			return nil, er.Wrapf(er.InvalidState, "task %s cannot reattach in state %s", in.ID, status)
		}
		if err := s.attach.EnsureAttach(runtime, taskHandle); err != nil {
			return nil, err
		}
	} else {
		// Guard against double start: a RUNNING task must not be started
		// again (it would restart the guest domain and spawn a second exit
		// watcher, publishing duplicate TaskExit events), and a STOPPED/
		// PAUSED task is not startable either (PAUSED would be silently
		// overwritten to RUNNING, losing the pause and duplicating the
		// watcher). Only the initial CREATED state may be started.
		//
		// claimLifecycle closes the TOCTOU window between the CREATED check
		// and markTaskRunning: concurrent Start RPCs (client timeout retries)
		// must not both pass while status is still CREATED. It also excludes
		// Delete for the same id so teardown cannot race guest bring-up.
		// ContainerNotReady (Unavailable) so clients retry after the peer
		// finishes — InvalidState maps to InvalidArgument and stalls teardown.
		if !s.claimLifecycle(in.ID) {
			return nil, er.Wrapf(er.ContainerNotReady, "task %s has a lifecycle operation in progress", in.ID)
		}
		defer s.releaseLifecycle(in.ID)

		status := taskStatusOf(runtime, taskHandle)
		if status != task.Status_CREATED {
			return nil, er.Wrapf(er.AlreadyExists, "task %s already in state %s", in.ID, status)
		}
		if err := s.lifecycle.Start(ctx, runtime, taskHandle); err != nil {
			return nil, err
		}
	}

	withTaskLock(runtime, func() {
		if taskHandle.PID() != 0 {
			respPid = taskHandle.PID()
		} else {
			respPid = runtime.ShimPID()
		}
	})

	return &StartOutput{
		ContainerID: taskHandle.ID(),
		ExecID:      "",
		Pid:         respPid,
	}, nil
}

func taskStatusOf(runtime ports.TaskLocker, taskHandle ports.Task) task.Status {
	var status task.Status
	withTaskLock(runtime, func() {
		if taskHandle != nil {
			status = taskHandle.Status()
		}
	})
	return status
}
