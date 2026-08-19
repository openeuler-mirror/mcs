package lifecycle

import (
	"micrun/internal/ports"
	"micrun/internal/support/validation"
)

// signalTaskIOExit closes the task's exit channel via IOExit(), which uses
// sync.Once. Do NOT call channels.Close on ExitChan() directly — that would
// bypass the Once and cause a subsequent IOExit() to panic on close-of-
// closed-channel.
func signalTaskIOExit(taskHandle ports.Task) {
	if validation.IsNil(taskHandle) {
		return
	}
	taskHandle.IOExit()
}

// stopTaskIOSession tears down the task's attach/IO session (copier
// goroutines, FIFO/TTY fds) so containerd-side FIFO copies observe EOF.
// Stop is idempotent and bounded (copierStopTimeout), so calling it on a
// session another teardown path already stopped is safe.
func stopTaskIOSession(taskHandle ports.Task) {
	if validation.IsNil(taskHandle) {
		return
	}
	mgr := taskHandle.IOManager()
	if validation.IsNil(mgr) {
		return
	}
	mgr.Stop()
}
