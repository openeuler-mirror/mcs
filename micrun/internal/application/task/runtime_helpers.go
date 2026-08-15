package task

import (
	"context"
	"fmt"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	er "micrun/internal/support/errors"
	"micrun/internal/support/lockutil"
	"micrun/internal/support/perf"
	"micrun/internal/support/validation"

	taskapi "github.com/containerd/containerd/api/types/task"
)

func (s *Service) requireSandbox(runtime ports.TaskSandboxAccess) (ports.Sandbox, error) {
	sandbox := runtime.Sandbox()
	if validation.IsNil(sandbox) {
		return nil, er.SandboxNotFound
	}
	return sandbox, nil
}

func (s *Service) requireRuntime(runtime any) error {
	return validation.RequireNotNil(runtime, "task runtime is required")
}

func (s *Service) prepareOperation(ctx context.Context, runtime any) (context.Context, error) {
	if err := s.requireRuntime(runtime); err != nil {
		return nil, err
	}
	return activeTaskContext(ctx)
}

func activeTaskContext(ctx context.Context) (context.Context, error) {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return ctx, err
	}
	return ctx, nil
}

func (s *Service) requireTask(runtime ports.TaskStore, id string) (ports.Task, error) {
	taskHandle, found := s.lookupTask(runtime, id)
	if !found {
		return nil, er.ContainerNotFound
	}
	return taskHandle, nil
}

func (s *Service) requirePrimaryTask(runtime ports.TaskStore, id, execID string) (ports.Task, error) {
	taskHandle, err := s.requireTask(runtime, id)
	if err != nil {
		return nil, err
	}
	if err := rejectExecID(execID); err != nil {
		return nil, err
	}
	return taskHandle, nil
}

func (s *Service) lookupTask(runtime ports.TaskStore, id string) (ports.Task, bool) {
	taskHandle, found := runtime.LookupTask(id)
	return taskHandle, !validation.IsNil(taskHandle) && found
}

func taskPIDOrShim(runtime ports.TaskIdentity, taskHandle ports.Task) uint32 {
	pid := taskHandle.PID()
	if pid <= 0 {
		return runtime.ShimPID()
	}
	return pid
}

func taskAlreadyAttached(taskHandle ports.Task) bool {
	return taskHandle.Status() == taskapi.Status_RUNNING && taskHandle.AttachInfo() != nil
}

func withTaskLock(runtime ports.TaskLocker, body func()) {
	lockutil.WithLock(runtime, body)
}

func withTaskLockError(runtime ports.TaskLocker, body func() error) error {
	return lockutil.WithLockError(runtime, body)
}

// reconcileTaskStatus re-syncs the task handle with the live guest status
// after a failed control operation. holdTransitional=false (the
// failed-Pause/Resume rollback path) also rolls an in-flight PAUSING back
// to the guest truth; holdTransitional=true (the Kill failure paths)
// preserves a concurrent Pause's PAUSING so the Pause owner still commits —
// a failed Kill must not steal the transition and leave the guest PAUSED
// while the task reports RUNNING (Resume would then silently skip).
func (s *Service) reconcileTaskStatus(ctx context.Context, runtime ports.TaskSignalRuntime, taskHandle ports.Task, holdTransitional bool) {
	// Query the guest OUTSIDE the task lock: QueryTaskStatus chains into a
	// blocking guest RPC (StatusContainer → ensureClientPresence) that can
	// stall until the context deadline. Holding the shared task lock during
	// that call would freeze every concurrent task RPC on the shim. Mirror
	// refreshTaskStatusForQuery: query unlocked, then set under lock.
	//
	// Detach from the RPC context: reconcile runs after a failed (and
	// non-cancelable) guest stop/kill that the caller already executed with
	// context.WithoutCancel. If the client's RPC deadline expired during
	// that slow call, passing the canceled ctx here makes QueryTaskStatus
	// fail immediately. The caller then clears the Kill pre-write (or leaves
	// the task stuck in PAUSING) without ever learning the true guest status
	// — so a container that was actually stopped by the partial kill is later
	// reported with a fabricated Interrupt(130) instead of the requested
	// signal code (e.g. 137 for SIGKILL).
	status, err := runtime.QueryTaskStatus(context.WithoutCancel(ctx), taskHandle.ID())
	if err != nil {
		// Do not overwrite RUNNING/PAUSED with UNKNOWN on transient RPC
		// failure — that would desync skip guards and State reporting.
		return
	}
	s.applyQueriedTaskStatus(runtime, taskHandle, status, holdTransitional)
}

// applyQueriedTaskStatus applies a guest-derived status to the task handle
// under the task lock. This is the single convergence policy shared by the
// read-only State refresh and the post-failure reconcile — the two previously
// diverged in subtle ways, which is exactly how transition races slip in:
//   - a terminal STOPPED task is never touched;
//   - UNKNOWN ("could not determine") never demotes a known status;
//   - a STOPPED report takes the full terminal transition (exit info + exit
//     channel) so a crashed guest is not later reported as a clean exit 0;
//   - holdTransitional preserves an in-flight Pause's PAUSING: read-only
//     queries must not steal the transition owner's commit, while reconcile
//     after a failed Pause/Resume passes false so the rollback to the real
//     guest state lands.
func (s *Service) applyQueriedTaskStatus(runtime ports.TaskLocker, taskHandle ports.Task, status taskapi.Status, holdTransitional bool) {
	withTaskLock(runtime, func() {
		if taskHandle.Status() == taskapi.Status_STOPPED {
			return
		}
		if status == taskapi.Status_UNKNOWN {
			return
		}
		if status == taskapi.Status_STOPPED {
			s.finalizeTaskStopped(taskHandle)
			return
		}
		if holdTransitional && taskHandle.Status() == taskapi.Status_PAUSING {
			return
		}
		taskHandle.SetStatus(status)
	})
}

func stopAndDeleteSandbox(ctx context.Context, sandbox ports.Sandbox, operation string) error {
	if err := stopSandboxOnly(ctx, sandbox, operation); err != nil {
		return err
	}
	return deleteSandboxOnly(ctx, sandbox, operation)
}

func stopSandboxOnly(ctx context.Context, sandbox ports.Sandbox, operation string) error {
	if validation.IsNil(sandbox) {
		return nil
	}
	// Kill/Delete RPC cancellation must not abort domain teardown mid-way
	// (micaCtl returns ctx.Err() immediately). Mirror lifecycle.task_stop.
	ctx = context.WithoutCancel(ctx)
	perfT := perf.Start(nil, "stop_sandbox", sandbox.SandboxID())
	if err := sandbox.Stop(ctx, true); err != nil {
		return fmt.Errorf("stop sandbox %s %s: %w", sandbox.SandboxID(), operation, err)
	}
	perfT.Total()
	return nil
}

func deleteSandboxOnly(ctx context.Context, sandbox ports.Sandbox, operation string) error {
	if validation.IsNil(sandbox) {
		return nil
	}
	ctx = context.WithoutCancel(ctx)
	if err := sandbox.Delete(ctx); err != nil {
		return fmt.Errorf("delete sandbox %s %s: %w", sandbox.SandboxID(), operation, err)
	}
	return nil
}
