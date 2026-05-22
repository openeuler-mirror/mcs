package attach

import (
	"context"

	"micrun/internal/ports"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"

	"github.com/containerd/containerd/api/types/task"
)

func (s *Service) PrepareResize(ctx context.Context, runtime ports.TaskAttachRuntime, taskHandle ports.Task, height, width uint32) error {
	if err := requireRuntime(runtime); err != nil {
		return err
	}
	if err := requireTask(taskHandle); err != nil {
		return err
	}
	snapshot := snapshotAttachSessionState(runtime, taskHandle)
	if validation.IsNil(snapshot.sandbox) {
		log.Debugf("[ATTACH] Sandbox is nil for %s, cannot resize PTY", taskHandle.ID())
		return er.Wrap(er.SandboxNotFound, "sandbox not found for "+taskHandle.ID())
	}

	if snapshot.status == task.Status_RUNNING && snapshot.attachInfo != nil && shouldRestartAttachForResize(snapshot.manager, snapshot.terminal, width > 0 || height > 0) {
		if err := s.restartSessionForResize(ctx, runtime, snapshot.sandbox, taskHandle, snapshot.attachTaskSnapshot); err != nil {
			return err
		}
	}

	return snapshot.sandbox.WinResize(ctx, taskHandle.ID(), height, width)
}

func shouldRestartAttachForResize(manager ports.IOManager, terminal bool, isRealAttach bool) bool {
	// Always restart when the current manager is absent or not running.
	// For non-terminal real attach sessions, a fresh manager is required to avoid
	// stale stdio paths from previous runs.
	isRunning := false
	if !validation.IsNil(manager) {
		isRunning = manager.IsRunning()
	}

	if validation.IsNil(manager) || !isRunning {
		return true
	}
	return isRealAttach && !terminal
}

func (s *Service) restartSessionForResize(
	ctx context.Context,
	runtime ports.TaskAttachRuntime,
	sandbox ports.Sandbox,
	taskHandle ports.Task,
	snapshot attachTaskSnapshot,
) error {
	log.Infof("[ATTACH] IO session not running for %s, restarting for attach", taskHandle.ID())
	factory, err := s.factoryForSessionRestart(snapshot.manager)
	if err != nil {
		return err
	}
	ttyHandles, err := openFreshTTYHandles(ctx, sandbox, taskHandle.ID())
	if err != nil {
		return err
	}
	updatedAttachInfo := buildAttachSessionInfo(attachSessionInfoRequest{
		factory:    factory,
		namespace:  runtime.Namespace(),
		taskID:     taskHandle.ID(),
		terminal:   snapshot.terminal,
		attachInfo: snapshot.attachInfo,
		freshTTY:   ttyHandles,
	})

	return s.restartOrBootstrapSession(sessionRestartRequest{
		ctx:          ctx,
		runtime:      runtime,
		taskHandle:   taskHandle,
		manager:      snapshot.manager,
		attachInfo:   updatedAttachInfo,
		freshTTY:     ttyHandles,
		errorContext: taskHandle.ID(),
	})
}

func (s *Service) factoryForSessionRestart(manager ports.IOManager) (ports.IOSessionFactory, error) {
	if validation.IsNil(manager) {
		return s.requireFactory()
	}
	return s.ioFactory, nil
}
