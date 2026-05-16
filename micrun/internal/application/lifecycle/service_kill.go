package lifecycle

import (
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"
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
		debugFailures: true,
	})

	exitStatus, exitedAt := snapshotTaskExitInfo(tc.Runtime, tc.Task, s.clockNow())
	markTaskStopped(tc.Runtime, tc.Task, exitStatus, exitedAt)
	signalTaskIOExit(tc.Task)
	log.Infof("[INTERNAL_KILL] Container %s stopped (reason: %s)", tc.Task.ID(), reason)
}
