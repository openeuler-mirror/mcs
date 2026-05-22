package container

import (
	"context"
	"errors"
	"testing"

	er "micrun/internal/support/errors"
)

func TestNilContainerReadOnlyTraitsReturnZeroValues(t *testing.T) {
	var container *Container

	if got := container.ID(); got != "" {
		t.Fatalf("nil ID() = %q, want empty", got)
	}
	if got := container.GetAnnotations(); got != nil {
		t.Fatalf("nil GetAnnotations() = %v, want nil", got)
	}
	if got := container.GetPid(); got != 0 {
		t.Fatalf("nil GetPid() = %d, want 0", got)
	}
	if got := container.GetMemoryLimit(); got != 0 {
		t.Fatalf("nil GetMemoryLimit() = %d, want 0", got)
	}
	if got := container.Sandbox(); got != nil {
		t.Fatalf("nil Sandbox() = %v, want nil", got)
	}
	if got := container.Status(); got != StateDown {
		t.Fatalf("nil Status() = %s, want %s", got, StateDown)
	}
	state := container.State()
	if state == nil || state.State != StateDown {
		t.Fatalf("nil State() = %#v, want StateDown", state)
	}
	if got := container.getFirmware(); got != "" {
		t.Fatalf("nil getFirmware() = %q, want empty", got)
	}
	if got := container.getPedConf(); got != "" {
		t.Fatalf("nil getPedConf() = %q, want empty", got)
	}
	if got := container.os(); got != "" {
		t.Fatalf("nil os() = %q, want empty", got)
	}
	if !container.cpuUnset() {
		t.Fatal("nil cpuUnset() = false, want true")
	}
}

func TestContainerOperationalReturnsStateProbeError(t *testing.T) {
	expected := errors.New("exists failed")
	container := &Container{
		ctx:    context.Background(),
		id:     "container1",
		config: &ContainerConfig{ID: "container1"},
		state:  ContainerState{State: StateReady},
		sandbox: &Sandbox{
			guestControl: &stubGuestControl{existsErr: expected},
		},
	}

	_, err := container.operational()
	if !errors.Is(err, expected) {
		t.Fatalf("operational error = %v, want %v", err, expected)
	}
}

func TestContainerStateSnapshotReturnsStateProbeError(t *testing.T) {
	expected := errors.New("exists failed")
	container := &Container{
		ctx:    context.Background(),
		id:     "container1",
		config: &ContainerConfig{ID: "container1"},
		state:  ContainerState{State: StateReady},
		sandbox: &Sandbox{
			guestControl: &stubGuestControl{existsErr: expected},
		},
	}

	state, err := container.StateSnapshot()
	if !errors.Is(err, expected) {
		t.Fatalf("StateSnapshot error = %v, want %v", err, expected)
	}
	if state.State != StateReady {
		t.Fatalf("StateSnapshot state = %s, want retained StateReady", state.State)
	}
}

func TestContainerReadOnlyTraitsHandleMissingConfig(t *testing.T) {
	container := &Container{id: "container1"}

	if got := container.GetAnnotations(); got != nil {
		t.Fatalf("missing config GetAnnotations() = %v, want nil", got)
	}
	if got := container.GetPid(); got != 0 {
		t.Fatalf("missing config GetPid() = %d, want 0", got)
	}
	if got := container.GetMemoryLimit(); got != 0 {
		t.Fatalf("missing config GetMemoryLimit() = %d, want 0", got)
	}
	if got := container.getFirmware(); got != "" {
		t.Fatalf("missing config getFirmware() = %q, want empty", got)
	}
	if got := container.getPedConf(); got != "" {
		t.Fatalf("missing config getPedConf() = %q, want empty", got)
	}
	if got := container.os(); got != "" {
		t.Fatalf("missing config os() = %q, want empty", got)
	}
	if !container.cpuUnset() {
		t.Fatal("missing config cpuUnset() = false, want true")
	}
}

func TestNilContainerRequireSandboxReturnsNotFound(t *testing.T) {
	var container *Container
	if err := container.requireSandbox(); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("requireSandbox error = %v, want ContainerNotFound", err)
	}
}
