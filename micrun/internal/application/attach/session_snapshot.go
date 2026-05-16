package attach

import "micrun/internal/ports"

type attachSessionSnapshot struct {
	attachTaskSnapshot
	sandbox ports.Sandbox
}

func snapshotAttachSessionState(runtime ports.TaskAttachRuntime, taskHandle ports.Task) attachSessionSnapshot {
	var snapshot attachSessionSnapshot
	withTaskLock(runtime, func() {
		snapshot = attachSessionSnapshot{
			attachTaskSnapshot: newAttachTaskSnapshot(taskHandle),
			sandbox:            runtime.Sandbox(),
		}
	})
	return snapshot
}
