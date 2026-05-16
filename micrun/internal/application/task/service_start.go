package task

import (
	"context"

	"micrun/internal/ports"
	log "micrun/internal/support/logger"
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
		if err := s.attach.EnsureAttach(runtime, taskHandle); err != nil {
			return nil, err
		}
	} else {
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
