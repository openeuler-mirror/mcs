package attach

import (
	"context"
	"fmt"
	"io"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"

	"github.com/containerd/containerd/api/types/task"
)

func (s *Service) StartInitialSession(
	ctx context.Context,
	runtime ports.TaskAttachRuntime,
	taskHandle ports.Task,
	ttyIn io.WriteCloser,
	ttyOut io.Reader,
	ttyErr io.Reader,
) error {
	if err := requireRuntime(runtime); err != nil {
		return err
	}
	if err := requireTask(taskHandle); err != nil {
		return err
	}
	log.Debugf("[ATTACH] StartInitialSession for %s", taskHandle.ID())
	return s.bootstrapSession(ctx, runtime, taskHandle, ports.AttachInfo{
		Stdin:    taskHandle.StdinPath(),
		Stdout:   taskHandle.StdoutPath(),
		Stderr:   taskHandle.StderrPath(),
		Terminal: taskHandle.Terminal(),
		TTYIn:    ttyIn,
		TTYOut:   ttyOut,
		TTYErr:   ttyErr,
	})
}

func (s *Service) EnsureAttach(runtime ports.TaskAttachRuntime, taskHandle ports.Task) error {
	if err := requireRuntime(runtime); err != nil {
		return err
	}
	if err := requireTask(taskHandle); err != nil {
		return err
	}
	snapshot := snapshotAttachSessionState(runtime, taskHandle)
	log.Infof("[ATTACH] Checking attach scenario for %s: attachInfo=%v, ioManager=%v",
		taskHandle.ID(), snapshot.attachInfo != nil, !validation.IsNil(snapshot.manager))

	if snapshot.attachInfo == nil {
		return er.Wrap(er.InvalidAttachInfo, "missing attach info for "+taskHandle.ID())
	}

	isRunning := false
	if !validation.IsNil(snapshot.manager) {
		isRunning = snapshot.manager.IsRunning()
	}
	log.Infof("[ATTACH] IsRunning check for %s: ioManager=%v, isRunning=%v",
		taskHandle.ID(), !validation.IsNil(snapshot.manager), isRunning)
	if !validation.IsNil(snapshot.manager) && isRunning {
		return nil
	}

	log.Infof("[ATTACH] Restarting IO session for %s", taskHandle.ID())

	factory, err := s.requireFactory()
	if err != nil {
		return err
	}

	infoRequest := attachSessionInfoRequest{
		factory:    factory,
		namespace:  runtime.Namespace(),
		taskID:     taskHandle.ID(),
		terminal:   snapshot.terminal,
		attachInfo: snapshot.attachInfo,
	}
	updatedAttachInfo := buildAttachSessionInfo(infoRequest)

	sessionCtx := attachSessionContext(nil, runtime)
	var ttyHandles freshTTYHandles
	if updatedAttachInfo.Terminal {
		if validation.IsNil(snapshot.sandbox) {
			return er.Wrap(er.SandboxNotFound, "sandbox not found for "+taskHandle.ID())
		}
		var ttyErr error
		ttyHandles, ttyErr = openFreshTTYHandles(sessionCtx, snapshot.sandbox, taskHandle.ID())
		if ttyErr != nil {
			return ttyErr
		}
		infoRequest.freshTTY = ttyHandles
		updatedAttachInfo = buildAttachSessionInfo(infoRequest)
	}

	return s.restartOrBootstrapSession(sessionRestartRequest{
		ctx:        sessionCtx,
		runtime:    runtime,
		taskHandle: taskHandle,
		manager:    snapshot.manager,
		attachInfo: updatedAttachInfo,
		freshTTY:   ttyHandles,
	})
}

type attachTaskSnapshot struct {
	status     task.Status
	attachInfo *ports.AttachInfo
	manager    ports.IOManager
	terminal   bool
}

func newAttachTaskSnapshot(taskHandle ports.Task) attachTaskSnapshot {
	return attachTaskSnapshot{
		status:     taskHandle.Status(),
		attachInfo: cloneAttachInfoPtr(taskHandle.AttachInfo()),
		manager:    taskHandle.IOManager(),
		terminal:   taskHandle.Terminal(),
	}
}

func (s *Service) bootstrapSession(
	ctx context.Context,
	runtime ports.TaskAttachRuntime,
	taskHandle ports.Task,
	attachInfo ports.AttachInfo,
) error {
	return s.startManagedSession(ctx, runtime, taskHandle, ioSessionConfigFromAttachInfo(taskHandle.ID(), attachInfo), &attachInfo)
}

func cloneAttachInfo(src *ports.AttachInfo) ports.AttachInfo {
	if src == nil {
		return ports.AttachInfo{}
	}
	return *src
}

func cloneAttachInfoPtr(src *ports.AttachInfo) *ports.AttachInfo {
	if src == nil {
		return nil
	}
	clone := cloneAttachInfo(src)
	return &clone
}

func sessionAttachInfoWithTTY(src *ports.AttachInfo, ttyIn io.WriteCloser, ttyOut io.Reader) ports.AttachInfo {
	attachInfo := cloneAttachInfo(src)
	if ttyIn != nil {
		attachInfo.TTYIn = ttyIn
		attachInfo.TTYOut = ttyOut
		attachInfo.TTYErr = ttyOut
	}
	return attachInfo
}

func (s *Service) requireFactory() (ports.IOSessionFactory, error) {
	if s.ioFactory == nil {
		return nil, er.FactoryNotConfigured
	}
	return s.ioFactory, nil
}

func (s *Service) startManagedSession(
	ctx context.Context,
	runtime ports.TaskAttachRuntime,
	taskHandle ports.Task,
	config ports.IOSessionConfig,
	attachInfo *ports.AttachInfo,
) error {
	factory, err := s.requireFactory()
	if err != nil {
		return err
	}
	if attachRuntimeBackgroundContext(runtime) == nil {
		if err := contextx.OrBackground(ctx).Err(); err != nil {
			return err
		}
	}

	sessionCtx := attachSessionContext(ctx, runtime)
	if err := sessionCtx.Err(); err != nil {
		return err
	}

	manager, eventStream, err := factory.NewSession(sessionCtx, config)
	if err != nil {
		return err
	}
	if validation.IsNil(manager) {
		return fmt.Errorf("IO session manager is required")
	}
	cleanupManager := func() {
		manager.Stop()
	}
	defer func() {
		if cleanupManager != nil {
			cleanupManager()
		}
	}()

	events, err := s.subscribeSessionEvents(eventStream)
	if err != nil {
		return err
	}

	if err := manager.Start(); err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}

	withTaskLock(runtime, func() {
		taskHandle.SetIOManager(manager)
		taskHandle.SetAttachInfo(attachInfo)
	})
	cleanupManager = nil

	go s.handleIOEvents(sessionCtx, runtime, taskHandle, events)
	log.Infof("[ATTACH] Saved attach info for %s: terminal=%v", taskHandle.ID(), attachInfo.Terminal)
	return nil
}
