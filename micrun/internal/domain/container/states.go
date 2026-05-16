package container

import (
	"fmt"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
)

type StateString string

const (
	StateReady    StateString = "ready"
	StateRunning  StateString = "running"
	StateStopped  StateString = "stopped"
	StateCreating StateString = "creating"
	StatePaused   StateString = "paused"
	StateDown     StateString = "down"
)

// ContainerState represents the state of a container.
type ContainerState struct {
	State StateString
}

// ContainerStatus represents the status of a container.
type ContainerStatus struct {
	Spec        *specs.Spec
	CreatedAt   time.Time
	State       ContainerState
	ID          string
	Rootfs      string
	Pid         int // The shim pid.
	Annotations map[string]string
}

type SandboxState struct {
	State   StateString
	Ped     string
	Version uint
}

// SandboxState public methods

func (s *SandboxState) Valid() bool {
	return s.State.valid()
}

func (s *SandboxState) Transition(old StateString, new StateString) error {
	if !s.Valid() {
		return fmt.Errorf("invalid state: %v", s)
	}

	return s.State.transition(old, new)
}

func (s *StateString) valid() bool {
	return s.known() && *s != StateDown
}

func (s *StateString) known() bool {
	switch *s {
	case StateReady, StateRunning, StateStopped, StateCreating, StatePaused, StateDown:
		return true
	default:
		return false
	}
}

// validTransitions defines the legal state machine edges.
// Key = source state, Value = set of allowed target states.
var validTransitions = map[StateString]map[StateString]struct{}{
	StateCreating: {StateStopped: {}},
	StateReady:    {StateRunning: {}, StateStopped: {}},
	StateRunning:  {StatePaused: {}, StateStopped: {}},
	StatePaused:   {StateRunning: {}, StateStopped: {}},
	StateStopped:  {StateRunning: {}},
	StateDown:     {StateReady: {}, StateRunning: {}, StateStopped: {}, StateCreating: {}},
}

func (s *StateString) transition(old StateString, new StateString) error {
	if *s != old {
		return fmt.Errorf("mismatched state: %s (expecting: %v)", *s, old)
	}

	allowed, ok := validTransitions[*s]
	if !ok {
		return fmt.Errorf("cannot transition from unknown state %v", *s)
	}
	if _, ok := allowed[new]; !ok {
		return fmt.Errorf("cannot transition from state %v to %v", *s, new)
	}
	return nil
}

// Valid checks if the container state is valid.
func (s *ContainerState) Valid() bool {
	return s.State.valid()
}

// ValidTransition checks if a state transition is valid.
func (s *ContainerState) Transition(old StateString, new StateString) error {
	return s.State.transition(old, new)
}
