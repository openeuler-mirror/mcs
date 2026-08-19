package attach

import (
	"micrun/internal/ports"
	"micrun/internal/support/lockutil"
	"micrun/internal/support/validation"
)

func withTaskLock(runtime ports.TaskAttachRuntime, body func()) {
	lockutil.WithLock(runtime, body)
}

func withTaskLockIfAvailable(runtime ports.TaskAttachRuntime, body func()) bool {
	if validation.IsNil(runtime) {
		return false
	}
	withTaskLock(runtime, body)
	return true
}

// clearAttachedUnlessLive clears IsAttached only when no concurrent reattach
// left a running IO manager. Unconditional false would arm auto-close against
// a peer EnsureAttach/PrepareResize that already won the restart race.
func clearAttachedUnlessLive(runtime ports.TaskAttachRuntime, taskHandle ports.Task) {
	if validation.IsNil(taskHandle) {
		return
	}
	withTaskLockIfAvailable(runtime, func() {
		mgr := taskHandle.IOManager()
		if validation.IsNil(mgr) || !mgr.IsRunning() {
			taskHandle.SetAttached(false)
		}
	})
}
