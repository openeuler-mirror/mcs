package task

import (
	"context"
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
		operate: func(ctx context.Context, sandbox ports.Sandbox, taskID string) error {
			return sandbox.ResumeContainer(ctx, taskID)
		},
	})
}

type taskSignalTransition struct {
	beforeStatus task.Status
	afterStatus  task.Status
	skip         func(task.Status) bool
	operate      func(context.Context, ports.Sandbox, string) error
}

func (s *Service) runTaskSignalTransition(ctx context.Context, runtime ports.TaskSignalRuntime, id string, transition taskSignalTransition) (SignalOutput, error) {
	var (
		taskHandle ports.Task
		taskID     string
		sandbox    ports.Sandbox
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
		if transition.beforeStatus != task.Status_UNKNOWN {
			taskHandle.SetStatus(transition.beforeStatus)
		}
		taskID = taskHandle.ID()
		return nil
	})
	if err != nil {
		return SignalOutput{}, err
	}

	sandboxErr := transition.operate(ctx, sandbox, taskID)
	withTaskLock(runtime, func() {
		if sandboxErr == nil {
			taskHandle.SetStatus(transition.afterStatus)
		} else {
			s.reconcileTaskStatus(ctx, runtime, taskHandle)
		}
	})

	if sandboxErr == nil {
		return SignalOutput{ContainerID: taskID, EmitEvent: true}, nil
	}
	return SignalOutput{ContainerID: taskID}, sandboxErr
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
		withTaskLock(runtime, func() {
			s.reconcileTaskStatus(ctx, runtime, taskHandle)
		})
		return err
	}
	withTaskLock(runtime, func() {
		taskHandle.SetStatus(transition.afterStatus)
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
	return nil
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
	if err := stopAndDeleteSandbox(ctx, sandbox, "during kill"); err != nil {
		return err
	}
	runtime.MarkKilledByAPI()
	withTaskLock(runtime, func() {
		runtime.SetSandbox(nil)
		markKilledTask(taskHandle, exitStatus, s.clockNow())
	})
	return nil
}

func (s *Service) killPodContainer(ctx context.Context, runtime ports.TaskSignalRuntime, taskHandle ports.Task, sandbox ports.Sandbox, taskID string, exitStatus uint32) error {
	if err := sandbox.KillContainer(ctx, taskID); err != nil {
		withTaskLock(runtime, func() {
			s.reconcileTaskStatus(ctx, runtime, taskHandle)
		})
		return err
	}
	runtime.MarkKilledByAPI()
	withTaskLock(runtime, func() {
		markKilledTask(taskHandle, exitStatus, s.clockNow())
	})
	return nil
}

func markKilledTask(taskHandle ports.Task, exitStatus uint32, exitedAt time.Time) {
	taskHandle.SetStatus(task.Status_STOPPED)
	taskHandle.SetExitInfo(exitStatus, exitedAt)
	taskHandle.IOExit()
	closeTaskExitSignal(taskHandle)
}

func (s *Service) pauseBySignal(ctx context.Context, runtime ports.TaskSignalRuntime, taskHandle ports.Task) error {
	return s.runTaskSignalTransitionForHandle(ctx, runtime, taskHandle, taskSignalTransition{
		beforeStatus: task.Status_PAUSING,
		afterStatus:  task.Status_PAUSED,
		skip: func(status task.Status) bool {
			return status == task.Status_PAUSING || status == task.Status_PAUSED || status == task.Status_STOPPED
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
		operate: func(ctx context.Context, sandbox ports.Sandbox, taskID string) error {
			return sandbox.ResumeContainer(ctx, taskID)
		},
	})
}
