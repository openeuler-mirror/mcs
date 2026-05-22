package lifecycle

import (
	"io"
	"time"

	"micrun/internal/ports"
	"micrun/internal/support/lockutil"
	log "micrun/internal/support/logger"

	"github.com/containerd/containerd/api/types/task"
)

func withTaskLock(runtime ports.TaskLocker, body func()) {
	lockutil.WithLock(runtime, body)
}

func snapshotRuntimeSandbox(runtime ports.TaskLifecycleRuntime) ports.Sandbox {
	var sandbox ports.Sandbox
	withTaskLock(runtime, func() {
		sandbox = runtime.Sandbox()
	})
	return sandbox
}

func clearRuntimeSandbox(runtime ports.TaskLifecycleRuntime) {
	withTaskLock(runtime, func() {
		runtime.SetSandbox(nil)
	})
}

func markTaskRunning(runtime ports.TaskLifecycleRuntime, taskHandle ports.Task) {
	var oldStatus task.Status
	withTaskLock(runtime, func() {
		oldStatus = taskHandle.Status()
		taskHandle.SetStatus(task.Status_RUNNING)
	})
	log.Debugf("container status from %s => %s", oldStatus, task.Status_RUNNING)
}

func markTaskStopped(runtime ports.TaskLifecycleRuntime, taskHandle ports.Task, exitStatus uint32, exitedAt time.Time) {
	withTaskLock(runtime, func() {
		taskHandle.SetStatus(task.Status_STOPPED)
		taskHandle.SetExitInfo(exitStatus, exitedAt)
	})
}

func clearTaskAttachInfo(runtime ports.TaskLocker, taskHandle ports.Task) {
	withTaskLock(runtime, func() {
		taskHandle.SetAttachInfo(nil)
	})
}

func snapshotTaskExitInfo(runtime ports.TaskLocker, taskHandle ports.Task, fallback time.Time) (uint32, time.Time) {
	var exitStatus uint32
	var exitedAt time.Time
	withTaskLock(runtime, func() {
		exitStatus = taskHandle.ExitStatus()
		exitedAt = taskHandle.ExitTime()
	})
	if exitedAt.IsZero() {
		exitedAt = fallback
	}
	return exitStatus, exitedAt
}

func snapshotTaskAndUnsetStdinPipe(runtime ports.TaskLocker, taskHandle ports.Task) io.WriteCloser {
	var stdin io.WriteCloser
	withTaskLock(runtime, func() {
		stdin = taskHandle.StdinPipe()
		taskHandle.SetStdinPipe(nil)
	})
	return stdin
}

func setTaskStdinPipe(runtime ports.TaskLocker, taskHandle ports.Task, stdin io.WriteCloser) {
	withTaskLock(runtime, func() {
		taskHandle.SetStdinPipe(stdin)
	})
}
