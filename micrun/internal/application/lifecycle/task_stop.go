package lifecycle

import (
	"context"
	"fmt"

	"micrun/internal/ports"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"
)

type taskStopOptions struct {
	reason        string
	deleteSandbox bool
	clearSandbox  bool
	debugFailures bool
}

func stopLifecycleTask(ctx context.Context, runtime ports.TaskLifecycleRuntime, sandbox ports.Sandbox, taskHandle ports.Task, options taskStopOptions) {
	if validation.IsNil(taskHandle) {
		return
	}
	if taskHandle.CanBeSandbox() {
		stopSandboxTask(ctx, runtime, sandbox, taskHandle, options)
		return
	}
	stopContainerTask(ctx, sandbox, taskHandle, options)
}

func stopSandboxTask(ctx context.Context, runtime ports.TaskLifecycleRuntime, sandbox ports.Sandbox, taskHandle ports.Task, options taskStopOptions) {
	if validation.IsNil(sandbox) {
		log.Debugf("sandbox already deleted, skipping sandbox stop for %s", taskHandle.ID())
		return
	}
	if err := sandbox.Stop(ctx, true); err != nil {
		logTaskStopFailure(options, "failed to stop sandbox %s: %v", sandbox.SandboxID(), err)
	}
	if options.deleteSandbox {
		if err := sandbox.Delete(ctx); err != nil {
			logTaskStopFailure(options, "failed to delete sandbox %s: %v", sandbox.SandboxID(), err)
		}
	}
	if options.clearSandbox && !validation.IsNil(runtime) {
		clearRuntimeSandbox(runtime)
	}
}

func stopContainerTask(ctx context.Context, sandbox ports.Sandbox, taskHandle ports.Task, options taskStopOptions) {
	if validation.IsNil(sandbox) {
		log.Debugf("sandbox already deleted, skipping container stop for %s", taskHandle.ID())
		return
	}
	if err := sandbox.StopContainer(ctx, taskHandle.ID(), true); err != nil {
		logTaskStopFailure(options, "failed to stop container %s: %v", taskHandle.ID(), err)
	}
}

func logTaskStopFailure(options taskStopOptions, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if options.reason != "" {
		message = fmt.Sprintf("%s (%s)", message, options.reason)
	}
	if options.debugFailures {
		log.Debug(message)
		return
	}
	log.Error(message)
}
