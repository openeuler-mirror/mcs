package ports

import (
	"time"

	"github.com/containerd/containerd/api/types/task"
)

// legalTaskTransitions is the canonical task status state machine. Every
// status write on a Task handle must be a legal edge here; the handle
// implementation enforces it as a final safety net, so a racing writer that
// slipped past its service-level guards cannot corrupt the lifecycle (the
// recurring bug class: one operation's unlocked guest-RPC window overwritten
// by a concurrent query or signal path).
//
// Rules encoded:
//   - UNKNOWN seeds anything (recovery restores persisted states).
//   - Forward-only: a started task never goes back to CREATED (containerd
//     semantics; a guest re-registering as Ready must not un-start the task).
//   - PAUSING may roll back to RUNNING (failed pause reconcile) or commit to
//     PAUSED; queries hold PAUSING via service-level policy.
//   - STOPPED is terminal and absorbing.
var legalTaskTransitions = map[task.Status]map[task.Status]bool{
	task.Status_UNKNOWN: {
		task.Status_CREATED: true,
		task.Status_RUNNING: true,
		task.Status_PAUSING: true,
		task.Status_PAUSED:  true,
		task.Status_STOPPED: true,
	},
	task.Status_CREATED: {
		task.Status_RUNNING: true,
		// Query convergence: a guest observed paused before Start finished
		// (crash-recovery edges) may surface as PAUSED on a CREATED task.
		task.Status_PAUSED:  true,
		task.Status_STOPPED: true,
	},
	task.Status_RUNNING: {
		task.Status_PAUSING: true,
		// Direct RUNNING→PAUSED: checkState converges a Suspended guest
		// without an in-flight Pause RPC (crash between pause and persist).
		task.Status_PAUSED:  true,
		task.Status_STOPPED: true,
	},
	task.Status_PAUSING: {
		task.Status_PAUSED:  true,
		task.Status_RUNNING: true,
		task.Status_STOPPED: true,
	},
	task.Status_PAUSED: {
		task.Status_RUNNING: true,
		task.Status_STOPPED: true,
	},
	task.Status_STOPPED: {},
}

// CanTransitTaskStatus reports whether a task status write from `from` to
// `to` is a legal lifecycle transition. Same-status rewrites are allowed
// (idempotent no-ops).
func CanTransitTaskStatus(from, to task.Status) bool {
	if from == to {
		return true
	}
	return legalTaskTransitions[from][to]
}

// TaskStatusTerminal reports whether the status is terminal (absorbing).
func TaskStatusTerminal(status task.Status) bool {
	return status == task.Status_STOPPED
}

// FinalizeTaskStopped drives a task to STOPPED with the full terminal-state
// contract in one place: exit info recorded (fallback used only when no real
// exit path pre-wrote one — Kill pre-writes e.g. 137 must be preserved) and
// the exit channel closed so blocked Wait RPCs unblock. IOExit is idempotent
// and runs even for already-STOPPED tasks so a finalize path that lost the
// race still guarantees the channel is closed.
//
// Caller must hold the task lock.
func FinalizeTaskStopped(t Task, fallbackExit uint32, exitedAt time.Time) {
	if t.Status() != task.Status_STOPPED {
		if t.ExitTime().IsZero() {
			t.SetExitInfo(fallbackExit, exitedAt)
		}
		t.SetStatus(task.Status_STOPPED)
	}
	t.IOExit()
}
