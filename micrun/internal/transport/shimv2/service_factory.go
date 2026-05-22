package shim

import (
	"context"
	"errors"
	"fmt"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"

	"github.com/containerd/containerd/namespaces"
	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

var (
	errTaskServiceRequired     = errors.New("task service is required")
	errRecoveryServiceRequired = errors.New("recovery service is required")
)

type recoveryApplication interface {
	Recover(ctx context.Context, runtime ports.RecoveryRuntime, backend ports.RecoveryBackend, taskFactory func(spec ports.RecoveredTask) ports.Task) error
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

func initializeDaemonMode(ctx context.Context, service *shimService, publisher shimv2.Publisher, recovery recoveryApplication) error {
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
	service.publisher = publisher

	go service.listenAndReportExits()

	forwarder := service.newEventsForwarder(ctx, publisher)
	go forwarder.forward()

	if err := service.recoverDaemonState(ctx, recovery); err != nil {
		log.Tracef("no existing sandbox to restore: %v", err)
	}

	return nil
}

func (s *shimService) recoverDaemonState(ctx context.Context, recovery recoveryApplication) error {
	return recovery.Recover(ctx, s, s.recoveryBackend(), s.makeRecoveredTask)
}
