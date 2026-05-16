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
