package container

import (
	"context"
	"fmt"

	er "micrun/internal/support/errors"
	"micrun/internal/support/lockutil"
)

func (c *Container) stats(ctx context.Context) (*ContainerStats, error) {
	if err := c.requireSandbox(); err != nil {
		return nil, err
	}
	if c.config == nil {
		return nil, fmt.Errorf("container config is nil")
	}
	if c.guestExec == nil {
		return nil, fmt.Errorf("guest executor is nil")
	}
	if c.sandbox.GetState() != StateRunning {
		return nil, er.SandboxDown
	}
	deps, err := c.sandbox.dependenciesChecked()
	if err != nil {
		return nil, err
	}

	totalUsec, err := c.cpuUsageUsec(ctx, deps)
	if err != nil {
		return nil, err
	}

	curMB := c.guestExec.CurrentMaxMem()
	thrMB := c.guestExec.MemoryThresholdMB()
	if thrMB == 0 {
		// Read the mutable memory limit under containersLock to avoid racing
		// with a concurrent UpdateContainer writing Resources.Memory.Limit.
		thrMB = lockutil.WithReadLockValue(&c.sandbox.containersLock, func() uint32 {
			return c.config.memoryLimitMB()
		})
	}
	// RTOS guests do not expose real RSS. CurrentMaxMem returns the configured
	// memory limit (records.memoryMB), NOT actual usage. Reporting it as Usage
	// makes kubelet/cadvisor see a constant 100% memory occupancy (usage ==
	// limit), risking false eviction decisions. Report 0 usage until real
	// guest-side telemetry is available; the Limit is still correct.
	_ = curMB
	usageBytes := uint64(0)
	limitBytes := uint64(thrMB) << 20

	st := &ContainerStats{
		ResourceStats: &ResourceStats{
			CPUStats: CPUStats{
				TotalUsage: totalUsec,
			},
			MemoryStats: MemoryStats{
				Usage: MemoryEntry{
					Limit:   limitBytes,
					MaxEver: limitBytes,
					Usage:   usageBytes,
				},
				Stats: map[string]uint64{},
			},
		},
		NetworkStats: nil,
	}
	return st, nil
}

func (c *Container) cpuUsageUsec(ctx context.Context, deps *Dependencies) (uint64, error) {
	if deps == nil || deps.VCPUStats == nil {
		return 0, fmt.Errorf("vcpu stats dependency is nil")
	}
	vcpuInfo, err := deps.VCPUStats(ctx)
	if err != nil {
		return 0, fmt.Errorf("read vcpu stats: %w", err)
	}
	return totalVCPUUsageUsec(c.id, vcpuInfo), nil
}

func totalVCPUUsageUsec(containerID string, info *VCPUUsageInfo) uint64 {
	if info == nil {
		return 0
	}

	var total uint64
	for _, entry := range info.DomainVCPUMap[containerID] {
		if entry.TimeSeconds > 0 {
			total += uint64(entry.TimeSeconds * 1_000_000.0)
		}
	}
	return total
}
