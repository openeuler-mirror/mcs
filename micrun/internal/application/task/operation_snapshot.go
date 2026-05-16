package task

import (
	"micrun/internal/ports"
	er "micrun/internal/support/errors"
)

type lockedTaskStore interface {
	ports.TaskLocker
	ports.TaskStore
}

type lockedTaskSandboxStore interface {
	lockedTaskStore
	ports.TaskSandboxAccess
}

type startTaskSnapshot struct {
	taskHandle     ports.Task
	shouldReattach bool
}

type taskSandboxSnapshot struct {
	taskID  string
	sandbox ports.Sandbox
}

func (s *Service) snapshotTaskForStart(runtime lockedTaskStore, id, execID string) (startTaskSnapshot, error) {
	var snapshot startTaskSnapshot
	_, err := s.snapshotPrimaryTaskWith(runtime, id, execID, func(taskHandle ports.Task) error {
		snapshot = startTaskSnapshot{
			taskHandle:     taskHandle,
			shouldReattach: taskAlreadyAttached(taskHandle),
		}
		return nil
	})
	return snapshot, err
}

func (s *Service) snapshotPrimaryTask(runtime lockedTaskStore, id, execID string) (ports.Task, error) {
	return s.snapshotPrimaryTaskWith(runtime, id, execID, nil)
}

func (s *Service) snapshotPrimaryTaskWith(runtime lockedTaskStore, id, execID string, capture func(ports.Task) error) (ports.Task, error) {
	var taskHandle ports.Task
	err := withTaskLockError(runtime, func() error {
		var lookupErr error
		taskHandle, lookupErr = s.requirePrimaryTask(runtime, id, execID)
		if lookupErr != nil {
			return lookupErr
		}
		if capture == nil {
			return nil
		}
		return capture(taskHandle)
	})
	return taskHandle, err
}

func (s *Service) snapshotTaskWithSandbox(runtime lockedTaskSandboxStore, id string) (taskSandboxSnapshot, error) {
	var snapshot taskSandboxSnapshot
	err := withTaskLockError(runtime, func() error {
		taskHandle, found := s.lookupTask(runtime, id)
		if !found {
			return er.ContainerNotFound
		}
		sandbox, err := s.requireSandbox(runtime)
		if err != nil {
			return err
		}
		snapshot = taskSandboxSnapshot{
			taskID:  taskHandle.ID(),
			sandbox: sandbox,
		}
		return nil
	})
	return snapshot, err
}
