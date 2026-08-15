package lifecycle

import (
	"micrun/internal/application/exitstatus"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"

	"github.com/containerd/containerd/api/types/task"
)

func (s *Service) internalKill(tc *taskContext, reason string) {
	if tc == nil || validation.IsNil(tc.Runtime) || validation.IsNil(tc.Task) {
		return
	}

	log.Infof("[INTERNAL_KILL] Stopping container %s (reason: %s)", tc.Task.ID(), reason)
	sandbox := snapshotRuntimeSandbox(tc.Runtime)
	stopLifecycleTask(tc.Context, tc.Runtime, sandbox, tc.Task, taskStopOptions{
		reason:        reason,
		deleteSandbox: true,
		clearSandbox:  true,
		// Failures here are real (a domain that stays alive is an orphan);
		// log at Error, not Debug.
		debugFailures: false,
	})

	now := s.clockNow()

	// Read exit info AND check/write in a single lock acquisition. A prior
	// version used snapshotTaskExitInfo (separate lock) followed by this
	// withTaskLock — but killSandboxTask/killPodContainer pre-write
	// SetExitInfo WITHOUT touching Status, so a snapshot taken in a separate
	// lock can observe stale exitStatus==0 while a pre-written code (e.g.
	// 137 for SIGKILL) already exists. Reading inside the same lock makes
	// the pre-written code visible so it is preserved instead of being
	// overwritten with Interrupt (130).
	withTaskLock(tc.Runtime, func() {
		if tc.Task.Status() == task.Status_STOPPED {
			// A concurrent exit path (IO exit, Kill API, forceStopIfActive)
			// already wrote a legitimate exit status. Do not clobber it.
			return
		}
		exitStatus := tc.Task.ExitStatus()
		exitedAt := tc.Task.ExitTime()
		// Only fabricate Interrupt when no path has pre-written a real exit
		// code. A non-zero ExitTime means a pre-write set a legitimate code
		// that must be preserved (e.g. 137 from killPodContainer).
		if exitStatus == exitstatus.Success && exitedAt.IsZero() {
			exitStatus = exitstatus.Interrupt()
		}
		if exitedAt.IsZero() {
			exitedAt = now
		}
		tc.Task.SetStatus(task.Status_STOPPED)
		tc.Task.SetExitInfo(exitStatus, exitedAt)
	})
	signalTaskIOExit(tc.Task)
	log.Infof("[INTERNAL_KILL] Container %s stopped (reason: %s)", tc.Task.ID(), reason)
}
