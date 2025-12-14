package oci

import (
	"testing"

	defs "micrun/definitions"
	cntr "micrun/pkg/micantainer"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func TestApplyContainerRuntimeDefaultsMaxVcpu(t *testing.T) {
	limit := int64(128 * 1024 * 1024)
	reservation := int64(16 * 1024 * 1024)
	cfg := &cntr.ContainerConfig{
		Resources: &specs.LinuxResources{
			Memory: &specs.LinuxMemory{
				Limit:       &limit,
				Reservation: &reservation,
			},
		},
	}
	rc := &RuntimeConfig{
		MaxContainerVCPUs: 6,
		MinContainerMemMB: 32,
		MaxContainerMemMB: 512,
	}
	annotations := map[string]string{
		defs.ContainerMaxVcpuNum: "4",
	}

	applyContainerRuntimeDefaults(cfg, annotations, rc)

	if cfg.MaxVcpuNum != 4 {
		t.Fatalf("MaxVcpuNum = %d, want 4", cfg.MaxVcpuNum)
	}
	if cfg.MemoryThresholdMB != 256 {
		t.Fatalf("memory threshold = %d, want 256", cfg.MemoryThresholdMB)
	}
}

func TestApplyContainerRuntimeDefaultsMemoryFallback(t *testing.T) {
	cfg := &cntr.ContainerConfig{}
	rc := &RuntimeConfig{
		MinContainerMemMB: 24,
	}

	applyContainerRuntimeDefaults(cfg, nil, rc)

	if cfg.MemoryReservationMiB() != 24 {
		t.Fatalf("MemoryReservationMiB = %d, want 24", cfg.MemoryReservationMiB())
	}
	if cfg.MemoryThresholdMB != 48 {
		t.Fatalf("memory threshold = %d, want 48", cfg.MemoryThresholdMB)
	}
	if cfg.MaxVcpuNum != defaultMaxContainerVCPUs {
		t.Fatalf("MaxVcpuNum = %d, want default %d", cfg.MaxVcpuNum, defaultMaxContainerVCPUs)
	}
}
