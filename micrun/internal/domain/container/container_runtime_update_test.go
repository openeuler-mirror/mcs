package container

import (
	"context"
	"errors"
	"strings"
	"testing"

	er "micrun/internal/support/errors"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func TestContainerUpdateReturnsSandboxResourceError(t *testing.T) {
	shares := uint64(2048)
	container := &Container{
		id:        "container-update",
		config:    &ContainerConfig{ID: "container-update"},
		guestExec: recordingGuestExecutor{},
		state:     ContainerState{State: StateRunning},
		sandbox: &Sandbox{
			state: SandboxState{State: StateRunning},
		},
	}

	err := container.update(context.Background(), specs.LinuxResources{
		CPU: &specs.LinuxCPU{Shares: &shares},
	})
	if err == nil {
		t.Fatal("update returned nil error")
	}
	if !strings.Contains(err.Error(), "update sandbox resources") {
		t.Fatalf("update error = %v, want sandbox resource context", err)
	}
}

func TestContainerUpdateValidatesDependencies(t *testing.T) {
	shares := uint64(2048)
	if err := (*Container)(nil).update(context.Background(), specs.LinuxResources{}); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("nil update error = %v, want ContainerNotFound", err)
	}

	container := &Container{
		id:     "container-update",
		config: &ContainerConfig{ID: "container-update"},
		state:  ContainerState{State: StateRunning},
		sandbox: &Sandbox{
			state: SandboxState{State: StateRunning},
		},
	}

	err := container.update(context.Background(), specs.LinuxResources{
		CPU: &specs.LinuxCPU{Shares: &shares},
	})
	if err == nil || !strings.Contains(err.Error(), "guest executor") {
		t.Fatalf("update error = %v, want guest executor error", err)
	}
}

func TestContainerUpdateUsesOperationContextForOperationalCheck(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	guest := &runtimeControlGuest{}
	container := &Container{
		ctx:       context.Background(),
		id:        "container-update-context",
		config:    &ContainerConfig{ID: "container-update-context"},
		state:     ContainerState{State: StateRunning},
		guestExec: recordingGuestExecutor{},
		sandbox: &Sandbox{
			ctx:          context.Background(),
			state:        SandboxState{State: StateRunning},
			guestControl: guest,
		},
	}

	err := container.update(ctx, specs.LinuxResources{})
	if err != nil {
		t.Fatalf("update returned error: %v", err)
	}
	if guest.existsCtx != ctx {
		t.Fatal("operational check did not receive operation context")
	}
}

func TestApplyChangesValidatesDependencies(t *testing.T) {
	if err := (*Container)(nil).applyChanges(context.Background(), NewResourceChanges(), specs.LinuxResources{}); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("nil applyChanges error = %v, want ContainerNotFound", err)
	}

	container := &Container{id: "container-update"}
	if err := container.applyChanges(context.Background(), NewResourceChanges(), specs.LinuxResources{}); err == nil || !strings.Contains(err.Error(), "container config") {
		t.Fatalf("applyChanges missing config error = %v, want config error", err)
	}

	container.config = &ContainerConfig{ID: "container-update"}
	if err := container.applyChanges(context.Background(), NewResourceChanges(), specs.LinuxResources{}); err == nil || !strings.Contains(err.Error(), "guest executor") {
		t.Fatalf("applyChanges missing guest executor error = %v, want guest executor error", err)
	}
}

func TestExtractChangesUsesExplicitUpdateDeltas(t *testing.T) {
	shares := uint64(2048)
	changes, hasUpdates := (&Container{}).extractChanges(specs.LinuxResources{
		CPU: &specs.LinuxCPU{Shares: &shares},
	})

	if !hasUpdates {
		t.Fatal("extractChanges() hasUpdates = false, want true")
	}
	if changes.CPUCapacity != nil {
		t.Fatalf("CPUCapacity = %v, want nil for shares-only update", *changes.CPUCapacity)
	}
	if changes.VCPU != nil {
		t.Fatalf("VCPU = %v, want nil for shares-only update", *changes.VCPU)
	}
	if changes.CPUWeight == nil || *changes.CPUWeight != ShareToWeight(shares) {
		t.Fatalf("CPUWeight = %v, want %d", changes.CPUWeight, ShareToWeight(shares))
	}
}

func TestExtractChangesDerivesCPUCapacityAndVCPUFromQuota(t *testing.T) {
	quota := int64(250000)
	period := uint64(100000)
	changes, hasUpdates := (&Container{}).extractChanges(specs.LinuxResources{
		CPU: &specs.LinuxCPU{
			Quota:  &quota,
			Period: &period,
		},
	})

	if !hasUpdates {
		t.Fatal("extractChanges() hasUpdates = false, want true")
	}
	if changes.CPUCapacity == nil || *changes.CPUCapacity != 250 {
		t.Fatalf("CPUCapacity = %v, want 250", changes.CPUCapacity)
	}
	if changes.VCPU == nil || *changes.VCPU != 3 {
		t.Fatalf("VCPU = %v, want 3", changes.VCPU)
	}
}

func TestApplyLinuxResourceConfigCopiesUpdatedPointers(t *testing.T) {
	period := uint64(100000)
	quota := int64(50000)
	shares := uint64(2048)
	limit := int64(128 << 20)
	res := &specs.LinuxResources{}

	applyLinuxResourceConfig(res, specs.LinuxResources{
		CPU: &specs.LinuxCPU{
			Period: &period,
			Quota:  &quota,
			Shares: &shares,
			Cpus:   "0,1",
		},
		Memory: &specs.LinuxMemory{Limit: &limit},
	})

	period = 1
	quota = 2
	shares = 3
	limit = 4

	if *res.CPU.Period != 100000 || *res.CPU.Quota != 50000 || *res.CPU.Shares != 2048 {
		t.Fatalf("CPU resource pointers were not copied: %+v", res.CPU)
	}
	if res.CPU.Cpus != "0,1" {
		t.Fatalf("CPU set = %q, want 0,1", res.CPU.Cpus)
	}
	if *res.Memory.Limit != 128<<20 {
		t.Fatalf("memory limit = %d, want %d", *res.Memory.Limit, int64(128<<20))
	}
}

func TestSetupMemoryRequiresGuestExecutorForXenLimit(t *testing.T) {
	limit := int64(128 << 20)
	container := &Container{
		id: "container-memory",
		config: &ContainerConfig{
			ID:           "container-memory",
			PedestalType: PedestalXen,
			Resources: &specs.LinuxResources{
				Memory: &specs.LinuxMemory{Limit: &limit},
			},
		},
	}

	err := container.setupMemory(context.Background())

	if err == nil || !strings.Contains(err.Error(), "guest executor") {
		t.Fatalf("setupMemory error = %v, want guest executor error", err)
	}
}
