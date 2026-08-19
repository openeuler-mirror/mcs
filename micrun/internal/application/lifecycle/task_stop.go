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
	// The guest-domain teardown (sandbox.Stop/Delete/StopContainer) is the
	// cleanup for whatever made us decide to stop. If that trigger was the
	// context being canceled (e.g. shim shutdown fires tc.Context.Done(),
	// which routes through handleWaitCanceled→internalKill→here), the
	// inherited ctx is already done and every guest RPC would fail
	// immediately — leaving the Xen domain alive as an orphan. Detach from
	// the parent cancellation so the teardown actually completes.
	stopCtx := context.WithoutCancel(ctx)
	if taskHandle.CanBeSandbox() {
		stopSandboxTask(stopCtx, runtime, sandbox, taskHandle, options)
		return
	}
	stopContainerTask(stopCtx, sandbox, taskHandle, options)
}

func stopSandboxTask(ctx context.Context, runtime ports.TaskLifecycleRuntime, sandbox ports.Sandbox, taskHandle ports.Task, options taskStopOptions) {
	if validation.IsNil(sandbox) {
		log.Debugf("sandbox already deleted, skipping sandbox stop for %s", taskHandle.ID())
		return
	}
	stopFailed := false
	if err := sandbox.Stop(ctx, true); err != nil {
		logTaskStopFailure(options, "failed to stop sandbox %s: %v", sandbox.SandboxID(), err)
		stopFailed = true
	}
	if options.deleteSandbox {
		if err := sandbox.Delete(ctx); err != nil {
			logTaskStopFailure(options, "failed to delete sandbox %s: %v", sandbox.SandboxID(), err)
			stopFailed = true
		}
	}
	// Keep the sandbox reference when teardown failed: clearing it would
	// orphan the guest domain with nothing tracking it (a later Delete
	// retry or shim restart would see no sandbox to clean up). Only clear
	// once the domain is confirmed gone.
	if options.clearSandbox && !stopFailed && !validation.IsNil(runtime) {
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
