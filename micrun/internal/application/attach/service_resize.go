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
	// Restart only when there is no live session. Production
	// Session.RestartWithTTYs rejects already-started managers ("already
	// started"), so a non-TTY running session must not force a restart —
	// WinResize alone is enough for an active manager.
	_ = terminal
	_ = isRealAttach
	if validation.IsNil(manager) {
		return true
	}
	return !manager.IsRunning()
}

func (s *Service) restartSessionForResize(
	ctx context.Context,
	runtime ports.TaskAttachRuntime,
	sandbox ports.Sandbox,
	taskHandle ports.Task,
	snapshot attachTaskSnapshot,
) error {
	log.Infof("[ATTACH] IO session not running for %s, restarting for attach", taskHandle.ID())

	// Mark attached before the slow restart (open TTY, start session) so the
	// auto-close timer resets during ResizePty reattach instead of racing
	// internalKill — same contract as EnsureAttach.
	withTaskLockIfAvailable(runtime, func() {
		taskHandle.SetAttached(true)
	})

	factory, err := s.factoryForSessionRestart(snapshot.manager)
	if err != nil {
		clearAttachedUnlessLive(runtime, taskHandle)
		return err
	}
	// Detach from the short-lived ResizePty RPC context so a client timeout
	// cannot abort TTY reopen mid-reattach (mirrors EnsureAttach).
	sessionCtx := attachSessionContext(context.Background(), runtime)
	ttyHandles, err := openFreshTTYHandles(sessionCtx, sandbox, taskHandle.ID())
	if err != nil {
		clearAttachedUnlessLive(runtime, taskHandle)
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

	if err := s.restartOrBootstrapSession(sessionRestartRequest{
		ctx:          sessionCtx,
		runtime:      runtime,
		taskHandle:   taskHandle,
		manager:      snapshot.manager,
		attachInfo:   updatedAttachInfo,
		freshTTY:     ttyHandles,
		errorContext: taskHandle.ID(),
	}); err != nil {
		clearAttachedUnlessLive(runtime, taskHandle)
		return err
	}
	return nil
}

func (s *Service) factoryForSessionRestart(manager ports.IOManager) (ports.IOSessionFactory, error) {
	if validation.IsNil(manager) {
		return s.requireFactory()
	}
	return s.ioFactory, nil
}
