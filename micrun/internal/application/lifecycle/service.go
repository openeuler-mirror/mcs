package lifecycle

import (
	"errors"
	"time"

	attachapp "micrun/internal/application/attach"
	"micrun/internal/support/timex"
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
