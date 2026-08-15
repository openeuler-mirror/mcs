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

// markTaskRunning sets Status=RUNNING unless a concurrent path already
// finalized the task as STOPPED. Returns false when the task is terminal
// (caller must not spawn the exit watcher or report Start success).
func markTaskRunning(runtime ports.TaskLifecycleRuntime, taskHandle ports.Task) bool {
	var oldStatus task.Status
	var marked bool
	withTaskLock(runtime, func() {
		oldStatus = taskHandle.Status()
		// A concurrent State RPC's refresh (refreshTaskStatusForQuery) may
		// have already observed the just-started domain as Running and
		// written Status_RUNNING onto the task during Start's unlocked
		// setupIO window. That is a legitimate, idempotent transition — the
		// domain IS running — not the Kill/Delete/Pause this guard rejects.
		// Treating it as success avoids tearing down a working domain and
		// orphaning the task (RUNNING with no domain/watcher).
		if oldStatus == task.Status_RUNNING {
			marked = true
			return
		}
		// Only transition from CREATED to RUNNING. A concurrent Kill/Delete
		// (STOPPED) or Pause (PAUSED/PAUSING) during the start window must
		// not be overwritten — the other operation already communicated its
		// result to the client.
		if oldStatus != task.Status_CREATED {
			return
		}
		taskHandle.SetStatus(task.Status_RUNNING)
		marked = true
	})
	if !marked {
		log.Warnf("task %s reached %s during start; not marking RUNNING", taskHandle.ID(), oldStatus)
		return false
	}
	if oldStatus != task.Status_RUNNING {
		log.Debugf("container status from %s => %s", oldStatus, task.Status_RUNNING)
	}
	return true
}

func markTaskStopped(runtime ports.TaskLifecycleRuntime, taskHandle ports.Task, exitStatus uint32, exitedAt time.Time) {
	withTaskLock(runtime, func() {
		// Canonical finalize: preserves exit info already written by a
		// concurrent exit path or a Kill pre-write (e.g. 137 for SIGKILL)
		// and closes the exit channel atomically with the terminal status.
		ports.FinalizeTaskStopped(taskHandle, exitStatus, exitedAt)
	})
}

// snapshotTaskExitInfo reads the task's exit status/time in one lock
// acquisition and reports whether any path has recorded a real exit (a
// non-zero timestamp). A zero timestamp means the task never exited through
// a normal path (e.g. the guest domain crashed), so a default 0 exit status
// is fabricated and must be replaced. The fallback time is used only for
// the reported timestamp, never for the had flag.
func snapshotTaskExitInfo(runtime ports.TaskLocker, taskHandle ports.Task, fallback time.Time) (uint32, time.Time, bool) {
	var exitStatus uint32
	var exitedAt time.Time
	withTaskLock(runtime, func() {
		exitStatus = taskHandle.ExitStatus()
		exitedAt = taskHandle.ExitTime()
	})
	had := !exitedAt.IsZero()
	if exitedAt.IsZero() {
		exitedAt = fallback
	}
	return exitStatus, exitedAt, had
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
