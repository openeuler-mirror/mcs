package attach

import (
	"context"

	"micrun/internal/ports"
	log "micrun/internal/support/logger"
)

func closeTaskStdin(ctx context.Context, runtime ports.TaskAttachRuntime, taskHandle ports.Task) error {
	// CloseIO(closeStdin=true) means the client no longer writes stdin.
	// containerd already closed the stdin FIFO write end, so the copier's
	// copyStdin observes EOF and stops its stdin path on its own — the shim
	// must NOT close anything here.
	//
	// In particular, the recorded StdinPipe is the guest TTY fd the copier
	// is still actively using for output: closing it would make the output
	// path fail (EBADF → session teardown), double-close on copier teardown,
	// and leave a dangling fd for a later reattach.
	_ = ctx
	// Unset the reference so a later CloseIO is a no-op and the handle is
	// not reused once the session is gone.
	withTaskLock(runtime, func() {
		taskHandle.SetStdinPipe(nil)
	})
	log.Debugf("CloseIO: stdin handle unset for %s (copier observes FIFO EOF)", taskHandle.ID())
	return nil
}
