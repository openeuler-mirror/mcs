package task

import (
	"errors"
	"time"

	attachapp "micrun/internal/application/attach"
	lifecycleapp "micrun/internal/application/lifecycle"
	"micrun/internal/support/timex"
)

// Service implements application-layer task orchestration while depending only
// on abstract runtime/task ports.
type Service struct {
	attach    *attachapp.Service
	lifecycle *lifecycleapp.Service
	now       timex.Clock
}

type serviceConfig struct {
	now timex.Clock
}

var (
	ErrApplicationServicesRequired   = errors.New("attach and lifecycle services are required")
	ErrMismatchedApplicationServices = errors.New("application services injection is inconsistent")
)

// Option customizes task service dependencies while keeping default production
// construction unchanged.
type Option func(*serviceConfig)

func WithClock(now timex.Clock) Option {
	return func(config *serviceConfig) {
		config.now = now
	}
}

func NewServiceChecked(attach *attachapp.Service, lifecycle *lifecycleapp.Service, opts ...Option) (*Service, error) {
	if attach == nil || lifecycle == nil {
		return nil, ErrApplicationServicesRequired
	}
	config := serviceConfig{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&config)
	}
	if lifecycle.AttachService() != attach {
		return nil, ErrMismatchedApplicationServices
	}
	return &Service{
		attach:    attach,
		lifecycle: lifecycle,
		now:       config.now,
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

func (s *Service) LifecycleService() *lifecycleapp.Service {
	if s == nil {
		return nil
	}
	return s.lifecycle
}
