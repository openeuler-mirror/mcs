package lifecycle

import (
	"context"
	"errors"
	"time"

	attachapp "micrun/internal/application/attach"
	"micrun/internal/ports"
	"micrun/internal/support/panicsafe"
	"micrun/internal/support/timex"
	"micrun/internal/support/validation"
)

// Service centralizes task launch and exit-wait orchestration so transport only
// maps runtime-v2 RPCs and forwards resulting events.
type Service struct {
	attach *attachapp.Service
	now    timex.Clock
}

type serviceConfig struct {
	now timex.Clock
}

var ErrAttachServiceRequired = errors.New("attach service is required")

// Option customizes lifecycle service dependencies without changing default
// production wiring.
type Option func(*serviceConfig)

func WithClock(now timex.Clock) Option {
	return func(config *serviceConfig) {
		config.now = now
	}
}

func NewServiceChecked(attach *attachapp.Service, opts ...Option) (*Service, error) {
	if attach == nil {
		return nil, ErrAttachServiceRequired
	}
	config := serviceConfig{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&config)
	}
	return &Service{
		attach: attach,
		now:    config.now,
	}, nil
}

func (s *Service) clockNow() time.Time {
	return timex.Now(s.now)
}

func (s *Service) AttachService() *attachapp.Service {
	if s == nil {
		return nil
	}
	return s.attach
}

// WatchExit spawns the exit-watcher goroutine for an already-running task
// (used by the recovery path, which restores RUNNING tasks without going
// through Start). Without this, a recovered task's exit channel is never
// closed when the guest exits, so Wait RPCs block forever.
//
// Auto-close is explicitly disabled: a recovered task has no IO session in
// this shim process, so IsAttached() is always false and the default 30s
// auto-close timer would kill a long-running task whose client simply hasn't
// reconnected yet.
func (s *Service) WatchExit(ctx context.Context, runtime ports.TaskLifecycleRuntime, taskHandle ports.Task) {
	if s == nil || validation.IsNil(runtime) || validation.IsNil(taskHandle) {
		return
	}
	eventCtx := lifecycleEventContext(ctx, runtime)
	panicsafe.Go("recovered task exit watcher", func() {
		tc := newTaskContext(eventCtx, runtime, taskHandle)
		_ = s.waitForExitWithPolicy(tc, waitPolicy{autoClose: false})
	})
}
