package task

import (
	"context"
	"fmt"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	er "micrun/internal/support/errors"
	"micrun/internal/support/lockutil"
	"micrun/internal/support/validation"

	taskapi "github.com/containerd/containerd/api/types/task"
)

func (s *Service) requireSandbox(runtime ports.TaskSandboxAccess) (ports.Sandbox, error) {
	sandbox := runtime.Sandbox()
	if validation.IsNil(sandbox) {
		return nil, er.SandboxNotFound
	}
	return sandbox, nil
}

func (s *Service) requireRuntime(runtime any) error {
	return validation.RequireNotNil(runtime, "task runtime is required")
}

func (s *Service) prepareOperation(ctx context.Context, runtime any) (context.Context, error) {
	if err := s.requireRuntime(runtime); err != nil {
		return nil, err
	}
	return activeTaskContext(ctx)
}

func activeTaskContext(ctx context.Context) (context.Context, error) {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return ctx, err
	}
	return ctx, nil
}

func (s *Service) requireTask(runtime ports.TaskStore, id string) (ports.Task, error) {
	taskHandle, found := s.lookupTask(runtime, id)
	if !found {
		return nil, er.ContainerNotFound
	}
	return taskHandle, nil
}

func (s *Service) requirePrimaryTask(runtime ports.TaskStore, id, execID string) (ports.Task, error) {
	taskHandle, err := s.requireTask(runtime, id)
	if err != nil {
		return nil, err
	}
	if err := rejectExecID(execID); err != nil {
		return nil, err
	}
	return taskHandle, nil
}

func (s *Service) lookupTask(runtime ports.TaskStore, id string) (ports.Task, bool) {
	taskHandle, found := runtime.LookupTask(id)
	return taskHandle, !validation.IsNil(taskHandle) && found
}

func taskPIDOrShim(runtime ports.TaskIdentity, taskHandle ports.Task) uint32 {
	pid := taskHandle.PID()
	if pid <= 0 {
		return runtime.ShimPID()
	}
	return pid
}

func taskAlreadyAttached(taskHandle ports.Task) bool {
	return taskHandle.Status() == taskapi.Status_RUNNING && taskHandle.AttachInfo() != nil
}

func withTaskLock(runtime ports.TaskLocker, body func()) {
	lockutil.WithLock(runtime, body)
}

func withTaskLockError(runtime ports.TaskLocker, body func() error) error {
	return lockutil.WithLockError(runtime, body)
}

func (s *Service) reconcileTaskStatus(ctx context.Context, runtime ports.TaskStatusOps, taskHandle ports.Task) {
	status, err := runtime.QueryTaskStatus(ctx, taskHandle.ID())
	if err != nil {
		taskHandle.SetStatus(taskapi.Status_UNKNOWN)
		return
	}
	taskHandle.SetStatus(status)
}

func stopAndDeleteSandbox(ctx context.Context, sandbox ports.Sandbox, operation string) error {
	if validation.IsNil(sandbox) {
		return nil
	}
	if err := sandbox.Stop(ctx, true); err != nil {
		return fmt.Errorf("stop sandbox %s %s: %w", sandbox.SandboxID(), operation, err)
	}
	if err := sandbox.Delete(ctx); err != nil {
		return fmt.Errorf("delete sandbox %s %s: %w", sandbox.SandboxID(), operation, err)
	}
	return nil
}
