package lifecycle

import (
	"context"
	"fmt"

	"micrun/internal/ports"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"
)

func (s *Service) Start(ctx context.Context, runtime ports.TaskLifecycleRuntime, taskHandle ports.Task) error {
	if err := validation.RequireNotNil(runtime, "lifecycle runtime is required"); err != nil {
		return err
	}
	if validation.IsNil(taskHandle) {
		return er.InvalidTaskHandle
	}

	tc := newTaskContext(ctx, runtime, taskHandle)
	ctx = tc.Context

	sandbox := snapshotRuntimeSandbox(runtime)
	if validation.IsNil(sandbox) {
		return er.Wrap(er.SandboxNotFound, "sandbox not found for "+taskHandle.ID())
	}

	if err := startSandboxOrContainer(ctx, sandbox, taskHandle); err != nil {
		return err
	}

	if err := s.setupIO(tc, sandbox); err != nil {
		cleanupTaskIOAfterStartFailure(runtime, taskHandle)
		stopTaskAfterFailedStart(ctx, runtime, sandbox, taskHandle)
		return err
	}

	markTaskRunning(runtime, taskHandle)

	eventCtx := lifecycleEventContext(ctx, runtime)
	go func() {
		_ = s.waitForExit(newTaskContext(eventCtx, runtime, taskHandle))
	}()
	return nil
}

func startSandboxOrContainer(ctx context.Context, sandbox ports.Sandbox, taskHandle ports.Task) error {
	if taskHandle.CanBeSandbox() {
		if err := sandbox.Start(ctx); err != nil {
			return fmt.Errorf("failed to start sandbox for %s: %w", taskHandle.ID(), err)
		}
	} else {
		if err := sandbox.StartContainer(ctx, taskHandle.ID()); err != nil {
			return fmt.Errorf("failed to start container %s: %w", taskHandle.ID(), err)
		}
	}
	return nil
}

func stopTaskAfterFailedStart(ctx context.Context, runtime ports.TaskLifecycleRuntime, sandbox ports.Sandbox, taskHandle ports.Task) {
	stopLifecycleTask(ctx, runtime, sandbox, taskHandle, taskStopOptions{
		reason:        "after start failure",
		deleteSandbox: true,
		clearSandbox:  true,
	})
}

func (s *Service) setupIO(tc *taskContext, sandbox ports.Sandbox) error {
	streams, err := openTaskIOStreams(tc.Context, sandbox, tc.Task)
	if err != nil {
		return err
	}

	log.Debugf("[LIFECYCLE] FIFO paths for %s: stdin=%q, stdout=%q, stderr=%q",
		tc.Task.ID(), tc.Task.StdinPath(), tc.Task.StdoutPath(), tc.Task.StderrPath())

	if taskHasAttachPaths(tc.Task) {
		if err := s.attach.StartInitialSession(tc.Context, tc.Runtime, tc.Task, streams.stdin, streams.stdout, streams.stderr); err != nil {
			streams.closeForTask(tc.Task.ID())
			return fmt.Errorf("failed to start IO session: %w", err)
		}
		recordTaskStdin(tc.Runtime, tc.Task, streams.stdin)
		return nil
	}

	recordTaskStdin(tc.Runtime, tc.Task, streams.stdin)
	completeTaskWithoutAttach(tc.Task)
	return nil
}
