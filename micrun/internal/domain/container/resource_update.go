package container

import (
	"context"
	"fmt"
	"strings"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	"micrun/internal/support/fs"
	log "micrun/internal/support/logger"
)

func updateContainerResource(ctx context.Context, c *Container, updated *ResourceChanges) error {
	if c == nil {
		return fmt.Errorf("missing container reference when updating resources")
	}
	if updated == nil {
		return nil
	}
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	exec := c.guestExec
	if err := requireResourceExecutor(exec); err != nil {
		return err
	}
	old := exec.ReadResource()
	if old == nil {
		old = &ports.ResourceSnapshot{}
	}

	log.Debugf("Resource update for container %s: old=%s, new=%s",
		c.id, formatSnapshotForLog(old), formatChangesForLog(updated))

	return applyResourceUpdatePlan(ctx, resourceUpdatePlan(ctx, exec, c.id, old, updated))
}

type resourceUpdateStep struct {
	name string
	run  func() error
}

func resourceUpdatePlan(ctx context.Context, exec ports.GuestExecutor, containerID string, old *ports.ResourceSnapshot, updated *ResourceChanges) []resourceUpdateStep {
	if old == nil {
		old = &ports.ResourceSnapshot{}
	}
	return []resourceUpdateStep{
		{name: "cpu capacity", run: func() error { return updateCPUCapacity(ctx, exec, containerID, updated) }},
		{name: "memory limit", run: func() error { return updateMemoryLimit(ctx, exec, containerID, updated) }},
		{name: "cpu set", run: func() error { return updateCPUSet(ctx, exec, old.ClientCPUSet, updated.ClientCPUSet) }},
		{name: "cpu weight", run: func() error { return updateCPUWeight(ctx, exec, containerID, updated) }},
		{name: "vcpu count", run: func() error { return updateVCPUCount(ctx, exec, containerID, updated) }},
	}
}

func applyResourceUpdatePlan(ctx context.Context, plan []resourceUpdateStep) error {
	ctx = contextx.OrBackground(ctx)
	for _, step := range plan {
		if err := ctx.Err(); err != nil {
			return err
		}
		if step.run == nil {
			continue
		}
		if err := step.run(); err != nil {
			log.Debugf("resource update step %s failed: %v", step.name, err)
			return err
		}
	}
	return nil
}

func updateCPUCapacity(ctx context.Context, exec ports.GuestExecutor, containerID string, updated *ResourceChanges) error {
	if updated == nil || updated.CPUCapacity == nil {
		return nil
	}
	if err := requireResourceExecutor(exec); err != nil {
		return err
	}
	if !exec.NeedUpdateCPUCap(ctx, *updated.CPUCapacity) {
		return nil
	}
	if err := exec.UpdateCPUCapacity(ctx, *updated.CPUCapacity); err != nil {
		return fmt.Errorf("failed to update cpu capacity of %s: %w", containerID, err)
	}
	if *updated.CPUCapacity == 0 {
		log.Infof("container %s's cpu capacity is unlimited", containerID)
	}
	return nil
}

func updateMemoryLimit(ctx context.Context, exec ports.GuestExecutor, containerID string, updated *ResourceChanges) error {
	if updated == nil || updated.MemoryMaxMB == nil {
		return nil
	}
	if err := requireResourceExecutor(exec); err != nil {
		return err
	}
	if !exec.NeedUpdateMemLimit(*updated.MemoryMaxMB) {
		return nil
	}
	if err := exec.EnsureMemoryLimit(ctx, *updated.MemoryMaxMB); err != nil {
		return fmt.Errorf("failed to update max memory of %s: %w", containerID, err)
	}
	return nil
}

func updateCPUSet(ctx context.Context, exec ports.GuestExecutor, oldSet, newSet string) error {
	if oldSet == newSet {
		return nil
	}
	if err := requireResourceExecutor(exec); err != nil {
		return err
	}
	if !exec.NeedUpdateCPUSet(oldSet, newSet) {
		return nil
	}
	if err := exec.UpdatePCPUConstraints(ctx, newSet); err != nil {
		return fmt.Errorf("failed to update cpuset of vcpu: %w", err)
	}
	return nil
}

func updateCPUWeight(ctx context.Context, exec ports.GuestExecutor, containerID string, updated *ResourceChanges) error {
	if updated == nil || updated.CPUWeight == nil {
		return nil
	}
	if err := requireResourceExecutor(exec); err != nil {
		return err
	}
	if !exec.NeedUpdateCPUWeight(*updated.CPUWeight) {
		return nil
	}
	if err := exec.UpdateCPUWeight(ctx, *updated.CPUWeight); err != nil {
		return fmt.Errorf("failed to set a different cpu weight for %s: %w", containerID, err)
	}
	return nil
}

func updateVCPUCount(ctx context.Context, exec ports.GuestExecutor, containerID string, updated *ResourceChanges) error {
	if updated == nil || updated.VCPU == nil {
		return nil
	}
	if err := requireResourceExecutor(exec); err != nil {
		return err
	}
	if !exec.NeedUpdateVCPUs(ctx, *updated.VCPU) {
		return nil
	}
	oldV, newV, err := exec.UpdateVCPUNum(ctx, *updated.VCPU)
	if err != nil {
		return fmt.Errorf("failed to update vcpu number for %s: %w", containerID, err)
	} else if oldV != newV {
		log.Infof("update vcpu number from %d to %d", oldV, newV)
	}
	return nil
}

func requireResourceExecutor(exec ports.GuestExecutor) error {
	if exec == nil {
		return fmt.Errorf("guest executor is nil")
	}
	return nil
}

func formatResourceFields(cpuCap, cpuWeight *uint32, cpuSet string, vcpu, memMB *uint32) string {
	var parts []string
	if cpuCap != nil {
		parts = append(parts, fmt.Sprintf("CPUCapacity=%d", *cpuCap))
	}
	if cpuWeight != nil {
		parts = append(parts, fmt.Sprintf("CPUWeight=%d", *cpuWeight))
	}
	if cpuSet != "" {
		parts = append(parts, fmt.Sprintf("ClientCpuSet=%s", cpuSet))
	}
	if vcpu != nil {
		parts = append(parts, fmt.Sprintf("VCPU=%d", *vcpu))
	}
	if memMB != nil {
		parts = append(parts, fmt.Sprintf("MemoryLimitMB=%d", *memMB))
	}
	if len(parts) == 0 {
		return "<empty>"
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func formatSnapshotForLog(res *ports.ResourceSnapshot) string {
	if res == nil {
		return "<nil>"
	}
	return formatResourceFields(res.CPUCapacity, res.CPUWeight, res.ClientCPUSet, res.VCPU, res.MemoryMaxMB)
}

func formatChangesForLog(res *ResourceChanges) string {
	if res == nil {
		return "<nil>"
	}
	return formatResourceFields(res.CPUCapacity, res.CPUWeight, res.ClientCPUSet, res.VCPU, res.MemoryMaxMB)
}

func ensureFirmwarePath(firmwarePath string) error {
	absPath, err := fs.EnsureRegularFilePath(firmwarePath)
	if err != nil {
		return err
	}

	log.Debugf("firmware path validated: %s", absPath)
	return nil
}
