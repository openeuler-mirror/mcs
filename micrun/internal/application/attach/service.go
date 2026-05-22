package attach

import (
	"time"

	"micrun/internal/ports"
	"micrun/internal/support/timex"
)

// Service centralizes attach, detach, resize, and stdio-session orchestration.
// It keeps the application layer focused on session semantics while the concrete
// FIFO/event implementation stays behind IO ports.
type Service struct {
	ioFactory    ports.IOSessionFactory
	now          timex.Clock
	eventProfile ioEventProfile
}

type serviceConfig struct {
	now        timex.Clock
	ioPolicies ioEventPolicySet
	err        error
}

// Option customizes an attach service while preserving the default production
// wiring for callers that do not need injection.
type Option func(*serviceConfig)

func WithClock(now timex.Clock) Option {
	return func(config *serviceConfig) {
		config.now = now
	}
}

// WithIOEventPolicies overlays custom IO event policies onto existing policies.
// A policy with an existing event type replaces the previous policy for that type.
// If the same event type appears multiple times in one option call, the last one
// in that call wins.
// A nil or empty policy list keeps default policies unchanged.
func WithIOEventPolicies(policies []ioEventPolicy) Option {
	return func(config *serviceConfig) {
		if config.err != nil {
			return
		}
		if len(policies) == 0 {
			return
		}
		merged, err := mergeIOEventPoliciesFromOptionsChecked(config.ioPolicies, policies)
		if err != nil {
			config.err = err
			return
		}
		config.ioPolicies = merged
	}
}

func NewServiceChecked(ioFactory ports.IOSessionFactory, opts ...Option) (*Service, error) {
	config := serviceConfig{
		now:        nil,
		ioPolicies: cloneIOEventPolicySet(defaultIOEventPolicySet),
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&config)
	}
	if config.err != nil {
		return nil, config.err
	}
	eventProfile, err := newIOEventProfileChecked(config.ioPolicies)
	if err != nil {
		return nil, err
	}
	return &Service{
		ioFactory:    ioFactory,
		now:          config.now,
		eventProfile: eventProfile,
	}, nil
}

func (s *Service) clockNow() time.Time {
	return timex.Now(s.now)
}
