package container

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"micrun/internal/support/cpuset"
	er "micrun/internal/support/errors"
)

func TestSetVcpuAffinityValidatesDependencies(t *testing.T) {
	cpuSet := cpuset.NewCPUSet(0)

	var nilContainer *Container
	if err := nilContainer.setVcpuAffinity(context.Background(), cpuSet); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("nil container setVcpuAffinity error = %v, want ContainerNotFound", err)
	}

	if err := (&Container{}).setVcpuAffinity(context.Background(), cpuSet); err == nil || !strings.Contains(err.Error(), "container config") {
		t.Fatalf("missing config setVcpuAffinity error = %v, want config error", err)
	}

	container := &Container{config: &ContainerConfig{ID: "container1"}}
	if err := container.setVcpuAffinity(context.Background(), cpuSet); err == nil || !strings.Contains(err.Error(), "guest executor") {
		t.Fatalf("missing guest executor setVcpuAffinity error = %v, want guest executor error", err)
	}
}

type pinningGuestExecutor struct {
	recordingGuestExecutor
	pinned []int
	err    error
}

func (p *pinningGuestExecutor) VCPUPin(ctx context.Context, cpus []int) error {
	p.pinned = append([]int(nil), cpus...)
	return p.err
}

func TestSetVcpuAffinityUpdatesContainerConfig(t *testing.T) {
	exec := &pinningGuestExecutor{}
	container := &Container{
		id:        "container1",
		config:    &ContainerConfig{ID: "container1"},
		guestExec: exec,
	}

	if err := container.setVcpuAffinity(context.Background(), cpuset.NewCPUSet(2, 0)); err != nil {
		t.Fatalf("setVcpuAffinity returned error: %v", err)
	}

	if !reflect.DeepEqual(exec.pinned, []int{0, 2}) {
		t.Fatalf("pinned CPUs = %v, want [0 2]", exec.pinned)
	}
	if container.config.VCPUNum != 2 {
		t.Fatalf("VCPUNum = %d, want 2", container.config.VCPUNum)
	}
	if got := container.config.CPUSet(); got != "0,2" {
		t.Fatalf("CPUSet() = %q, want 0,2", got)
	}
	if container.config.PCPUNum != 2 {
		t.Fatalf("PCPUNum = %d, want 2", container.config.PCPUNum)
	}
}

func TestSetVcpuAffinityDoesNotUpdateConfigOnPinFailure(t *testing.T) {
	pinErr := errors.New("pin failed")
	exec := &pinningGuestExecutor{err: pinErr}
	container := &Container{
		id: "container1",
		config: &ContainerConfig{
			ID:      "container1",
			VCPUNum: 1,
			PCPUNum: 1,
		},
		guestExec: exec,
	}

	if err := container.setVcpuAffinity(context.Background(), cpuset.NewCPUSet(0, 2)); !errors.Is(err, pinErr) {
		t.Fatalf("setVcpuAffinity error = %v, want pin failed", err)
	}

	if container.config.VCPUNum != 1 {
		t.Fatalf("VCPUNum = %d, want 1", container.config.VCPUNum)
	}
	if got := container.config.CPUSet(); got != "" {
		t.Fatalf("CPUSet() = %q, want empty", got)
	}
	if container.config.PCPUNum != 1 {
		t.Fatalf("PCPUNum = %d, want 1", container.config.PCPUNum)
	}
}
