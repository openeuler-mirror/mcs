package micantainer

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
	// Unsupported yet
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
	return *s != StateDown
}

func (s *StateString) transition(old StateString, new StateString) error {
	if *s != old {
		return fmt.Errorf("mismatched state: %s (expecting: %v)", *s, old)
	}

	switch *s {
	case StateCreating:
		if new == StateStopped {
			return nil
		}
	case StateReady:
		if new == StateRunning || new == StateStopped {
			return nil
		}

	case StateRunning:
		if new == StatePaused || new == StateStopped {
			return nil
		}

	case StatePaused:
		if new == StateRunning || new == StateStopped {
			return nil
		}

	case StateStopped:
		if new == StateRunning {
			return nil
		}

	case StateDown:
		if new == StateReady || new == StateRunning || new == StateStopped || new == StateCreating {
			return nil
		}
	}

	return fmt.Errorf("cannot transition from state %v to %v", s, new)
}

// Valid checks if the container state is valid.
func (s *ContainerState) Valid() bool {
	return s.State.valid()
}

// ValidTransition checks if a state transition is valid.
func (s *ContainerState) Transition(old StateString, new StateString) error {
	return s.State.transition(old, new)
}
