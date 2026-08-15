package task

import (
	"errors"
	"sync"
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

	// lifecycleClaims serializes Start and Delete for the same task id.
	// Start↔Start: two CREATED checks must not both pass before markTaskRunning.
	// Start↔Delete: Delete must not tear down the task while Start still holds
	// a sandbox snapshot and can recreate the guest after Delete finished —
	// that leaves an untracked Xen/micad domain with no task entry.
	lifecycleClaimsMu sync.Mutex
	lifecycleClaims   map[string]struct{}
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
		attach:          attach,
		lifecycle:       lifecycle,
		now:             config.now,
		lifecycleClaims: make(map[string]struct{}),
	}, nil
}

// claimLifecycle reserves Start/Delete for id. Returns false if another
// Start or Delete is already in flight for the same task.
func (s *Service) claimLifecycle(id string) bool {
	if s == nil {
		return false
	}
	s.lifecycleClaimsMu.Lock()
	defer s.lifecycleClaimsMu.Unlock()
	if s.lifecycleClaims == nil {
		s.lifecycleClaims = make(map[string]struct{})
	}
	if _, ok := s.lifecycleClaims[id]; ok {
		return false
	}
	s.lifecycleClaims[id] = struct{}{}
	return true
}

func (s *Service) releaseLifecycle(id string) {
	if s == nil {
		return
	}
	s.lifecycleClaimsMu.Lock()
	delete(s.lifecycleClaims, id)
	s.lifecycleClaimsMu.Unlock()
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
