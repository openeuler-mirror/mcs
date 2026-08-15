package shim

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
	"micrun/internal/support/panicsafe"
	"micrun/internal/support/validation"

	"github.com/containerd/containerd/namespaces"
	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

var (
	errTaskServiceRequired     = errors.New("task service is required")
	errRecoveryServiceRequired = errors.New("recovery service is required")
)

type recoveryApplication interface {
	Recover(ctx context.Context, runtime ports.RecoveryRuntime, backend ports.RecoveryBackend, taskFactory func(spec ports.RecoveredTask) ports.Task, exitWatcher func(task ports.Task)) error
}

type lifecycleApplication interface {
	WatchExit(ctx context.Context, runtime ports.TaskLifecycleRuntime, task ports.Task)
}

func isOneShotAction(action string) bool {
	switch action {
	case "start", "delete":
		return true
	default:
		return false
	}
}

func newOneShotShimService(id string, deps runtimeDependencies, action string) *shimService {
	service := newBaseShimService(id, deps)
	// One-shot commands (notably `delete`/Cleanup) run without any Create
	// request, so like recovery they cannot see a CRI-ConfigPath-configured
	// state_dir. Adopt the recorded pointer so cleanup removes state from
	// where it actually lives instead of only the default /run/micrun
	// (scan item 2.5). Best-effort: on failure the one-shot proceeds with
	// whatever paths the base service bound.
	if err := configureRuntimePaths(deps.containerDeps, adoptStateDirPointer("")); err != nil {
		log.Warnf("[ONESHOT] failed to adopt state-dir pointer for '%s': %v", action, err)
	}
	log.Infof("[ONESHOT] shimService initialized for '%s' command (one-shot, will exit after completion)", action)
	return service
}

func newDaemonShimService(ctx context.Context, id string, shutdown func(), task taskApplication, deps runtimeDependencies) (*shimService, error) {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if validation.IsNil(task) {
		return nil, errTaskServiceRequired
	}
	if err := deps.validate(); err != nil {
		return nil, err
	}
	namespace, err := namespaceFromContext(ctx)
	if err != nil {
		return nil, err
	}

	service := newBaseShimService(id, deps)
	if err := service.configureDaemonRuntime(ctx, namespace, shutdown, task); err != nil {
		return nil, err
	}
	return service, nil
}

func newBaseShimService(id string, deps runtimeDependencies) *shimService {
	return &shimService{
		id:          id,
		runtimeDeps: deps,
		processID:   processIDProviderFrom(deps),
		shutdown:    shutdownEffectsFrom(deps),
		now:         deps.now,
	}
}

func namespaceFromContext(ctx context.Context) (string, error) {
	namespace, ok := namespaces.Namespace(ctx)
	if !ok {
		return "", fmt.Errorf("namespace is required")
	}
	return normalizeShimNamespace(namespace)
}

func (s *shimService) configureDaemonRuntime(ctx context.Context, namespace string, shutdown func(), task taskApplication) error {
	s.shimPid = s.processID.PID()
	s.namespace = namespace
	s.ctx = ctx
	s.ss = shutdown
	s.containers = make(map[string]*shimContainer)
	s.events, s.ec = newShimEventChannels()
	manager, err := newTaskManager(taskManagerDepsFromShimService(s), task)
	if err != nil {
		return err
	}
	s.tm = manager
	return nil
}

func newShimEventChannels() (chan shimEvent, chan exitEvent) {
	return make(chan shimEvent, channelSize), make(chan exitEvent, channelSize)
}

func initializeDaemonMode(ctx context.Context, service *shimService, publisher shimv2.Publisher, recovery recoveryApplication, lifecycle lifecycleApplication) error {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if validation.IsNil(recovery) {
		return errRecoveryServiceRequired
	}

	micadPid, err := getMicadPid()
	if err != nil {
		return fmt.Errorf("micad is not running: %w", err)
	}
	log.Infof("[DAEMON] shimService initialized, micad PID: %d", micadPid)

	// Apply host micrun config (state_dir, etc.) before recovery. Create is
	// the only other caller of loadRuntimeConfig; without this, a custom
	// state_dir is still unknown at restore time and recovery looks under
	// /run/micrun while live guests were persisted elsewhere.
	if err := service.applyHostRuntimeConfig(); err != nil {
		return fmt.Errorf("failed to apply host runtime config: %w", err)
	}

	panicsafe.Go("exit event listener", service.listenAndReportExits)

	forwarder := service.newEventsForwarder(ctx, publisher)
	panicsafe.Go("event forwarder", forwarder.forward)

	exitWatcher := func(task ports.Task) {
		if validation.IsNil(lifecycle) {
			return
		}
		lifecycle.WatchExit(ctx, service, task)
	}
	if err := service.recoverDaemonState(ctx, recovery, exitWatcher); err != nil {
		// A missing persisted sandbox is the normal first-boot path: the
		// repository surfaces it as fs.ErrNotExist (runtime snapshot) or
		// er.SandboxNotFound (legacy fallback). Any other error means
		// recovery failed while a guest domain may still be alive —
		// proceeding with empty state would leak the domain and confuse a
		// later Create for the same id. Surface it loudly instead of hiding
		// it behind a misleading "no existing sandbox" trace.
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, er.SandboxNotFound) {
			log.Debugf("no existing sandbox to restore: %v", err)
		} else {
			return fmt.Errorf("failed to restore sandbox state: %w", err)
		}
	}

	return nil
}

func (s *shimService) recoverDaemonState(ctx context.Context, recovery recoveryApplication, exitWatcher func(task ports.Task)) error {
	return recovery.Recover(ctx, s, s.recoveryBackend(), s.makeRecoveredTask, exitWatcher)
}

// applyHostRuntimeConfig resolves the host micrun config stack (files /
// MICRUN_CONF_FILE) and rebinds state paths before recovery runs. The result
// is a host-only baseline: configFromCreate stays false so the first Create
// re-resolves the full stack with the pod's annotations/options instead of
// short-circuiting on this pre-set config.
func (s *shimService) applyHostRuntimeConfig() error {
	if s == nil {
		return fmt.Errorf("shim service is nil")
	}
	s.Lock()
	defer s.Unlock()
	if s.config != nil {
		return nil
	}
	cfg, err := s.runtimeDeps.runtimeResolver.Resolve(nil, ports.TaskCreateRequest{}, nil)
	if err != nil {
		return err
	}
	if cfg == nil {
		return fmt.Errorf("runtime config resolver returned nil")
	}
	// The host-only resolve cannot see a state_dir configured via the CRI
	// RuntimeClass ConfigPath (it arrives only with Create requests). Adopt
	// the pointer recorded by the previous binding so recovery reads the
	// directory where live guests were actually persisted (scan item 2.5).
	stateDir := adoptStateDirPointer(cfg.StateDir)
	if err := configureRuntimePaths(s.runtimeDeps.containerDeps, stateDir); err != nil {
		return err
	}
	cfg.StateDir = stateDir
	s.config = cfg
	log.Debugf("applyHostRuntimeConfig: state_dir=%s", cfg.StateDir)
	return nil
}
