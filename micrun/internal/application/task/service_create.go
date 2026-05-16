package task

import (
	"context"
	"fmt"

	"micrun/internal/ports"
	er "micrun/internal/support/errors"
	"micrun/internal/support/fs"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"

	"github.com/containerd/containerd/api/types/task"
)

func (s *Service) Create(ctx context.Context, runtime ports.TaskCreateRuntime, in CreateInput) (*CreateOutput, error) {
	var err error
	ctx, err = s.prepareOperation(ctx, runtime)
	if err != nil {
		return nil, err
	}

	req := in.Request
	log.Tracef("creating task %s (bundle: %s, terminal: %v)", req.ID, req.Bundle, req.Terminal)
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}

	taskHandle, err := createTaskHandle(ctx, runtime, req)
	if err != nil {
		return nil, err
	}

	withTaskLock(runtime, func() {
		registerCreatedTask(runtime, req.ID, taskHandle)
	})

	return &CreateOutput{Pid: taskPIDOrShim(runtime, taskHandle)}, nil
}

func validateCreateRequest(req ports.TaskCreateRequest) error {
	if err := fs.ValidContainerID(req.ID); err != nil {
		return fmt.Errorf("%w: %v", er.InvalidCID, err)
	}
	return nil
}

func createTaskHandle(ctx context.Context, runtime ports.TaskFactory, req ports.TaskCreateRequest) (ports.Task, error) {
	taskHandle, err := runtime.CreateTask(ctx, req)
	if err != nil {
		return nil, err
	}
	if validation.IsNil(taskHandle) {
		return nil, fmt.Errorf("created task handle is nil")
	}
	if taskHandle.ID() != req.ID {
		return nil, fmt.Errorf("created task id mismatch: %s != %s", taskHandle.ID(), req.ID)
	}
	return taskHandle, nil
}

func registerCreatedTask(runtime ports.TaskStore, id string, taskHandle ports.Task) {
	taskHandle.SetStatus(task.Status_CREATED)
	runtime.SaveTask(id, taskHandle)
}
