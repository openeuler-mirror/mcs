package task

import (
	"context"
	"fmt"
	"time"

	"micrun/internal/application/exitstatus"
	"micrun/internal/ports"
	er "micrun/internal/support/errors"
	"micrun/internal/support/validation"

	"github.com/containerd/containerd/api/types/task"
)

func (s *Service) Pause(ctx context.Context, runtime ports.TaskSignalRuntime, in SignalInput) (SignalOutput, error) {
	var err error
	ctx, err = s.prepareOperation(ctx, runtime)
	if err != nil {
		return SignalOutput{}, err
	}

	return s.runTaskSignalTransition(ctx, runtime, in.ID, taskSignalTransition{
		beforeStatus: task.Status_PAUSING,
		afterStatus:  task.Status_PAUSED,
		skip: func(status task.Status) bool {
			// Never rewrite a terminal STOPPED (or already-pausing) task.
			return status == task.Status_PAUSING || status == task.Status_PAUSED || status == task.Status_STOPPED
		},
		// Only a RUNNING task may pause. CREATED is reachable while Start still
		// holds the unlocked guest bring-up window: pausing then leaves the
		// task PAUSED after Start's markTaskRunning failure tears the domain
		// down, with no way to Start or Resume.
		require: func(status task.Status) bool {
			return status == task.Status_RUNNING
		},
		operate: func(ctx context.Context, sandbox ports.Sandbox, taskID string) error {
			return sandbox.PauseContainer(ctx, taskID)
		},
	})
}

func (s *Service) Resume(ctx context.Context, runtime ports.TaskSignalRuntime, in SignalInput) (SignalOutput, error) {
	var err error
	ctx, err = s.prepareOperation(ctx, runtime)
	if err != nil {
		return SignalOutput{}, err
	}

	return s.runTaskSignalTransition(ctx, runtime, in.ID, taskSignalTransition{
		afterStatus: task.Status_RUNNING,
		skip: func(status task.Status) bool {
			return status == task.Status_RUNNING || status == task.Status_STOPPED
		},
		// Only a PAUSED task may resume. PAUSING is reachable while Pause's
		// unlocked guest RPC is in flight: resuming then lets Pause's success
		// path overwrite RUNNING back to PAUSED while the guest stays up.
		require: func(status task.Status) bool {
			return status == task.Status_PAUSED
		},
		requireErr: er.ContainerNotPaused,
		operate: func(ctx context.Context, sandbox ports.Sandbox, taskID string) error {
			return sandbox.ResumeContainer(ctx, taskID)
		},
	})
}

type taskSignalTransition struct {
	beforeStatus task.Status
	afterStatus  task.Status
	skip         func(task.Status) bool
	require      func(task.Status) bool
	requireErr   error
	operate      func(context.Context, ports.Sandbox, string) error
}

func (s *Service) requireTransitionStatus(taskHandle ports.Task, transition taskSignalTransition) error {
	if transition.require == nil || transition.require(taskHandle.Status()) {
		return nil
	}
	fail := transition.requireErr
	if fail == nil {
		fail = er.ContainerNotRunning
	}
	return er.Wrapf(fail, "task %s cannot transition from state %s", taskHandle.ID(), taskHandle.Status())
}

// commitTransitionStatus reports whether the transition was actually
// committed; callers must not publish the transition event when it was not.
func (s *Service) commitTransitionStatus(taskHandle ports.Task, transition taskSignalTransition) bool {
	// Don't clobber a terminal STOPPED set by a concurrent Kill during the
	// unlocked operate call. Mirrors reconcileTaskStatus.
	if taskHandle.Status() == task.Status_STOPPED {
		return false
	}
	// When beforeStatus was set (Pause: RUNNING→PAUSING), only commit if we
	// are still in that intermediate state. A concurrent Resume may have
	// already moved PAUSING→RUNNING after bringing the guest back; forcing
	// afterStatus (PAUSED) would leave task PAUSED while the guest runs.
	if transition.beforeStatus != task.Status_UNKNOWN && taskHandle.Status() != transition.beforeStatus {
		return false
	}
	taskHandle.SetStatus(transition.afterStatus)
	return true
}

func (s *Service) runTaskSignalTransition(ctx context.Context, runtime ports.TaskSignalRuntime, id string, transition taskSignalTransition) (SignalOutput, error) {
	var (
		taskHandle ports.Task
		taskID     string
		sandbox    ports.Sandbox
		skipped    bool
	)

	err := withTaskLockError(runtime, func() error {
		var snapshotErr error
		taskHandle, snapshotErr = s.requireTask(runtime, id)
		if snapshotErr != nil {
			return snapshotErr
		}

		sandbox, snapshotErr = s.requireSandbox(runtime)
		if snapshotErr != nil {
			return snapshotErr
		}
		if transition.skip != nil && transition.skip(taskHandle.Status()) {
			skipped = true
			taskID = taskHandle.ID()
			return nil
		}
		if err := s.requireTransitionStatus(taskHandle, transition); err != nil {
			return err
		}
		if transition.beforeStatus != task.Status_UNKNOWN {
			taskHandle.SetStatus(transition.beforeStatus)
		}
		taskID = taskHandle.ID()
		return nil
	})
	if err != nil {
		return SignalOutput{}, err
	}
	if skipped {
		return SignalOutput{ContainerID: taskID}, nil
	}

	sandboxErr := transition.operate(ctx, sandbox, taskID)
	if sandboxErr != nil {
		// The failed Pause/Resume owns its PAUSING rollback.
		s.reconcileTaskStatus(ctx, runtime, taskHandle, false)
		return SignalOutput{ContainerID: taskID}, sandboxErr
	}

	// Only emit TaskPaused/TaskResumed when the status commit actually
	// happened: a concurrent Kill/Delete may have finalized STOPPED during
	// the unlocked operate call, and its TaskExit/TaskDelete events must not
	// be followed by a stale pause/resume event for a dead task.
	committed := false
	withTaskLock(runtime, func() {
		committed = s.commitTransitionStatus(taskHandle, transition)
	})
	return SignalOutput{ContainerID: taskID, EmitEvent: committed}, nil
}

func (s *Service) runTaskSignalTransitionForHandle(ctx context.Context, runtime ports.TaskSignalRuntime, taskHandle ports.Task, transition taskSignalTransition) error {
	var (
		sandbox ports.Sandbox
		taskID  string
	)

	err := withTaskLockError(runtime, func() error {
		if transition.skip != nil && transition.skip(taskHandle.Status()) {
			return nil
		}
		if err := s.requireTransitionStatus(taskHandle, transition); err != nil {
			return err
		}

		var err error
		sandbox, err = s.requireSandbox(runtime)
		if err != nil {
			return err
		}
		taskID = taskHandle.ID()
		if transition.beforeStatus != task.Status_UNKNOWN {
			taskHandle.SetStatus(transition.beforeStatus)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if taskID == "" {
		return nil
	}

	if err := transition.operate(ctx, sandbox, taskID); err != nil {
		// The failed Pause/Resume owns its PAUSING rollback.
		s.reconcileTaskStatus(ctx, runtime, taskHandle, false)
		return err
	}
	withTaskLock(runtime, func() {
		s.commitTransitionStatus(taskHandle, transition)
	})
	return nil
}

func (s *Service) Kill(ctx context.Context, runtime ports.TaskSignalRuntime, in KillInput) error {
	var err error
	ctx, err = s.prepareOperation(ctx, runtime)
	if err != nil {
		return err
	}

	taskHandle, err := s.snapshotPrimaryTask(runtime, in.ID, in.ExecID)
	if err != nil {
		return err
	}
	action := classifyKillSignal(in.Signal)

	if handler, ok := killSignalHandlers[action]; ok {
		return handler(s, ctx, runtime, taskHandle, in.Signal)
	}
	if in.Signal == 0 {
		// Signal 0 is a presence probe; an alive task answers success.
		return nil
	}
	// The signal was never delivered to the guest. Reporting success here
	// would make kubelet wait out the whole termination grace period
	// believing the workload got a chance to shut down gracefully. Report
	// it instead, mirroring Container.Signal which rejects unmapped signals
	// rather than returning a misleading nil.
	return fmt.Errorf("%w: kill signal %d is not mapped to a guest action", er.NotSupported, in.Signal)
}

type killSignalHandler func(*Service, context.Context, ports.TaskSignalRuntime, ports.Task, uint32) error

var killSignalHandlers = map[killSignalAction]killSignalHandler{
	killSignalStopTask:   handleKillSignalStopTask,
	killSignalPauseTask:  handleKillSignalPauseTask,
	killSignalResumeTask: handleKillSignalResumeTask,
}

func handleKillSignalStopTask(s *Service, ctx context.Context, runtime ports.TaskSignalRuntime, taskHandle ports.Task, signal uint32) error {
	return s.killContainer(ctx, runtime, taskHandle, signal)
}

func handleKillSignalPauseTask(s *Service, ctx context.Context, runtime ports.TaskSignalRuntime, taskHandle ports.Task, _ uint32) error {
	return s.pauseBySignal(ctx, runtime, taskHandle)
}

func handleKillSignalResumeTask(s *Service, ctx context.Context, runtime ports.TaskSignalRuntime, taskHandle ports.Task, _ uint32) error {
	return s.resumeBySignal(ctx, runtime, taskHandle)
}

func (s *Service) killContainer(ctx context.Context, runtime ports.TaskSignalRuntime, taskHandle ports.Task, signal uint32) error {
	exitStatus := exitstatus.FromSignal(signal)
	taskID := taskHandle.ID()
	canBeSandbox := taskHandle.CanBeSandbox()
	var sandbox ports.Sandbox
	var alreadyStopped bool

	lockErr := withTaskLockError(runtime, func() error {
		if taskHandle.Status() == task.Status_STOPPED {
			alreadyStopped = true
			return nil
		}

		sandbox = runtime.Sandbox()
		if !canBeSandbox && validation.IsNil(sandbox) {
			return er.SandboxNotFound
		}
		return nil
	})
	if lockErr != nil {
		return lockErr
	}
	if alreadyStopped {
		return nil
	}

	if canBeSandbox {
		return s.killSandboxTask(ctx, runtime, taskHandle, sandbox, exitStatus)
	}
	return s.killPodContainer(ctx, runtime, taskHandle, sandbox, taskID, exitStatus)
}

func (s *Service) killSandboxTask(ctx context.Context, runtime ports.TaskSignalRuntime, taskHandle ports.Task, sandbox ports.Sandbox, exitStatus uint32) error {
	// Record the requested exit status BEFORE stopping the domain: the exit
	// watcher may observe the guest going down during the stop and would
	// otherwise fabricate 130 (completeExitedTask's guest-exit policy),
	// making the reported code nondeterministic (130 vs 128+signal).
	prewrote := false
	withTaskLock(runtime, func() {
		if taskHandle.ExitTime().IsZero() {
			taskHandle.SetExitInfo(exitStatus, s.clockNow())
			prewrote = true
		}
	})
	// Split Stop and Delete: Stop destroys the domain; Delete only cleans
	// persisted state. Rolling back the pre-write on a combined error would
	// clear a legitimate signal code when Stop succeeded but Delete failed
	// (domain already gone, watcher not yet STOPPED).
	if err := stopSandboxOnly(ctx, sandbox, "during kill"); err != nil {
		// Stop may have torn down the domain and still returned error
		// (e.g. StoreSandbox failure after containers stopped). Reconcile
		// before clearing the pre-write — same contract as killPodContainer.
		// holdTransitional: a concurrent Pause owns PAUSING; this failed
		// Kill must not steal its commit.
		s.reconcileTaskStatus(ctx, runtime, taskHandle, true)
		if prewrote {
			withTaskLock(runtime, func() {
				if taskHandle.Status() != task.Status_STOPPED {
					taskHandle.SetExitInfo(0, time.Time{})
				}
			})
		}
		return err
	}
	runtime.MarkKilledByAPI()
	withTaskLock(runtime, func() {
		markKilledTask(taskHandle, exitStatus, s.clockNow())
	})
	// Delete after the kill is finalized. Only clear the runtime sandbox
	// pointer once Delete succeeds so a later task Delete can retry cleanup
	// of persisted state. Exit code stays finalized either way.
	if err := deleteSandboxOnly(ctx, sandbox, "during kill"); err != nil {
		return err
	}
	withTaskLock(runtime, func() {
		runtime.SetSandbox(nil)
	})
	return nil
}

func (s *Service) killPodContainer(ctx context.Context, runtime ports.TaskSignalRuntime, taskHandle ports.Task, sandbox ports.Sandbox, taskID string, exitStatus uint32) error {
	// Pre-write the exit status BEFORE KillContainer destroys the guest,
	// mirroring killSandboxTask: the exit watcher's completeExitedTask races
	// with this path, and without a pre-write it sees hadExitInfo==false and
	// fabricates Interrupt(130), clobbering the real signal code (e.g. 137
	// for SIGKILL). markKilledTask's ExitTime guard would then keep the
	// wrong 130.
	prewrote := false
	withTaskLock(runtime, func() {
		if taskHandle.ExitTime().IsZero() {
			taskHandle.SetExitInfo(exitStatus, s.clockNow())
			prewrote = true
		}
	})

	// Detach from RPC cancellation so a client timeout cannot abort domain
	// kill after partial progress (same contract as stopSandboxOnly).
	killCtx := context.WithoutCancel(ctx)
	if err := sandbox.KillContainer(killCtx, taskID); err != nil {
		// Reconcile first: KillContainer may have destroyed the domain and
		// still returned an error. Only clear the pre-write when the task is
		// still non-terminal after reconcile — a partial success must keep
		// the signal code for the exit watcher.
		// holdTransitional: same protection as the killSandboxTask path.
		s.reconcileTaskStatus(ctx, runtime, taskHandle, true)
		if prewrote {
			withTaskLock(runtime, func() {
				if taskHandle.Status() != task.Status_STOPPED {
					taskHandle.SetExitInfo(0, time.Time{})
				}
			})
		}
		return err
	}
	runtime.MarkKilledByAPI()
	withTaskLock(runtime, func() {
		markKilledTask(taskHandle, exitStatus, s.clockNow())
	})
	return nil
}

func markKilledTask(taskHandle ports.Task, exitStatus uint32, exitedAt time.Time) {
	// Exit info already recorded (by the Kill pre-write in killSandboxTask,
	// or by the exit watcher when the guest went down during Kill's stop) is
	// preserved by the canonical finalize: the TaskExit event carries that
	// value, so overwriting would leave the event and the Wait/State/Delete
	// RPCs disagreeing.
	ports.FinalizeTaskStopped(taskHandle, exitStatus, exitedAt)
}

func (s *Service) pauseBySignal(ctx context.Context, runtime ports.TaskSignalRuntime, taskHandle ports.Task) error {
	return s.runTaskSignalTransitionForHandle(ctx, runtime, taskHandle, taskSignalTransition{
		beforeStatus: task.Status_PAUSING,
		afterStatus:  task.Status_PAUSED,
		skip: func(status task.Status) bool {
			return status == task.Status_PAUSING || status == task.Status_PAUSED || status == task.Status_STOPPED
		},
		require: func(status task.Status) bool {
			return status == task.Status_RUNNING
		},
		operate: func(ctx context.Context, sandbox ports.Sandbox, taskID string) error {
			return sandbox.PauseContainer(ctx, taskID)
		},
	})
}

func (s *Service) resumeBySignal(ctx context.Context, runtime ports.TaskSignalRuntime, taskHandle ports.Task) error {
	return s.runTaskSignalTransitionForHandle(ctx, runtime, taskHandle, taskSignalTransition{
		afterStatus: task.Status_RUNNING,
		skip: func(status task.Status) bool {
			return status == task.Status_RUNNING || status == task.Status_STOPPED
		},
		require: func(status task.Status) bool {
			return status == task.Status_PAUSED
		},
		requireErr: er.ContainerNotPaused,
		operate: func(ctx context.Context, sandbox ports.Sandbox, taskID string) error {
			return sandbox.ResumeContainer(ctx, taskID)
		},
	})
}
