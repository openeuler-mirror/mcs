package task

import (
	"context"

	"micrun/internal/ports"
	er "micrun/internal/support/errors"
)

func (s *Service) ResizePty(ctx context.Context, runtime ports.TaskIORuntime, in ResizePtyInput) error {
	var err error
	ctx, err = s.prepareOperation(ctx, runtime)
	if err != nil {
		return err
	}
	taskHandle, err := s.snapshotPrimaryTask(runtime, in.ID, in.ExecID)
	if err != nil {
		return err
	}

	// Claim lifecycle so a concurrent Delete cannot destroy the domain
	// between PrepareResize opening fresh TTYs and the session restart
	// wiring them — which would orphan a dangling TTY on an untracked
	// manager whose backing guest is already gone.
	if !s.claimLifecycle(in.ID) {
		return er.Wrapf(er.ContainerNotReady, "task %s has a lifecycle operation in progress", in.ID)
	}
	defer s.releaseLifecycle(in.ID)

	if err := s.attach.PrepareResize(ctx, runtime, taskHandle, in.Height, in.Width); err != nil {
		return err
	}
	withTaskLock(runtime, func() {
		taskHandle.SetAttached(true)
	})
	return nil
}

func (s *Service) CloseIO(ctx context.Context, runtime ports.TaskIORuntime, in CloseIOInput) error {
	var err error
	ctx, err = s.prepareOperation(ctx, runtime)
	if err != nil {
		return err
	}
	taskHandle, err := s.snapshotPrimaryTask(runtime, in.ID, in.ExecID)
	if err != nil {
		return err
	}
	return s.attach.CloseIO(ctx, runtime, taskHandle, in.CloseStdin)
}

func (s *Service) Update(ctx context.Context, runtime ports.TaskIORuntime, in UpdateInput) error {
	var err error
	ctx, err = s.prepareOperation(ctx, runtime)
	if err != nil {
		return err
	}
	target, err := s.snapshotTaskWithSandbox(runtime, in.ID)
	if err != nil {
		return err
	}

	return target.sandbox.UpdateContainer(ctx, target.taskID, in.Resources)
}
