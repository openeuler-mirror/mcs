package container

import (
	"context"
	"fmt"

	er "micrun/internal/support/errors"
	"micrun/internal/support/lockutil"
	log "micrun/internal/support/logger"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func (c *Container) update(ctx context.Context, resources specs.LinuxResources) error {
	if c == nil {
		return er.ContainerNotFound
	}
	if c.config != nil && c.config.IsInfra {
		return nil
	}
	if err := c.requireSandbox(); err != nil {
		return err
	}
	if c.sandbox.GetState() != StateRunning {
		return er.SandboxDown
	}
	operational, err := c.operationalWithContext(ctx)
	if err != nil {
		return err
	}
	if !operational {
		return fmt.Errorf("container not ready or running, cannot update")
	}
	if err := c.validateUpdate(); err != nil {
		return err
	}

	changes, hasUpdates := c.extractChanges(resources)
	if !hasUpdates {
		return nil
	}
	if c.guestExec == nil {
		return fmt.Errorf("guest executor is nil")
	}

	return c.applyChanges(ctx, changes, resources)
}

func (c *Container) validateUpdate() error {
	if c.config == nil {
		return fmt.Errorf("container config is nil")
	}
	// Initialize shared Resource pointers under containersLock so concurrent
	// readers (getSandboxCpusetStr, calculations, StoreSandbox) don't see a
	// half-published nil→non-nil transition.
	c.sandbox.containersLock.Lock()
	defer c.sandbox.containersLock.Unlock()
	if c.config.Resources == nil {
		c.config.Resources = &specs.LinuxResources{}
	}
	if c.config.Resources.CPU == nil {
		c.config.Resources.CPU = &specs.LinuxCPU{}
	}
	if c.config.Resources.Memory == nil {
		c.config.Resources.Memory = &specs.LinuxMemory{}
	}
	return nil
}

func (c *Container) extractChanges(resources specs.LinuxResources) (*ResourceChanges, bool) {
	return newLinuxResourceUpdate(resources).changes()
}

func (c *Container) applyChanges(ctx context.Context, changes *ResourceChanges, resources specs.LinuxResources) error {
	if c == nil {
		return er.ContainerNotFound
	}
	if c.config == nil {
		return fmt.Errorf("container config is nil")
	}
	if c.guestExec == nil {
		return fmt.Errorf("guest executor is nil")
	}
	if err := c.requireSandbox(); err != nil {
		return err
	}
	if err := updateContainerResource(ctx, c, changes); err != nil {
		return err
	}

	// ContainerConfig.Resources is shared with sandbox-wide readers; protect
	// the write under containersLock to avoid torn reads of CPU.Cpus (a Go
	// string is a non-atomic pointer+length pair).
	c.sandbox.containersLock.Lock()
	applyLinuxResourceConfig(c.config.Resources, resources)
	// Sync the derived vCPU count into the shared config: the live guest
	// already got it (updateVCPUCount), and without this a restart would
	// recreate the domain with the stale count, silently reverting the
	// update. PCPUNum follows VCPUNum (no explicit pCPU pinning here).
	if changes.VCPU != nil && *changes.VCPU > 0 {
		c.config.VCPUNum = *changes.VCPU
		c.config.PCPUNum = int(*changes.VCPU)
	}
	c.sandbox.containersLock.Unlock()

	if err := c.sandbox.updateResources(ctx); err != nil {
		return fmt.Errorf("update sandbox resources for %s: %w", c.id, err)
	}

	// Persist the updated config (combined sandbox document) so a later shim
	// restart restores the new values. Without this, the update only lives
	// in memory and is lost on recovery (restore reloads the stale
	// snapshot). A persistence failure is still reported: the guest already
	// got the new values, so the caller can retry idempotently.
	if err := c.saveState(ctx); err != nil {
		return fmt.Errorf("failed to persist container state after update for %s: %w", c.id, err)
	}

	return nil
}

func applyLinuxResourceConfig(res *specs.LinuxResources, resources specs.LinuxResources) {
	newLinuxResourceUpdate(resources).applyTo(res)
}

func (c *Container) setupMemory(ctx context.Context) error {
	if c == nil || c.config == nil || c.config.IsInfra {
		return nil
	}

	if c.config.PedestalType != PedestalXen {
		return nil
	}

	// Read the mutable memory limit under containersLock to avoid racing
	// with a concurrent UpdateContainer writing Resources.Memory.Limit.
	limit := lockutil.WithReadLockValue(&c.sandbox.containersLock, func() uint32 {
		return c.config.memoryLimitMB()
	})
	if limit == 0 {
		return nil
	}
	if c.guestExec == nil {
		return fmt.Errorf("guest executor is nil")
	}

	if c.guestExec.CurrentMaxMem() == limit && c.guestExec.MemoryThresholdMB() >= limit {
		return nil
	}

	log.Tracef("setting mem threshold to %d MB", limit)
	if err := c.guestExec.UpdateMemoryThreshold(ctx, limit); err != nil {
		return fmt.Errorf("failed to set new memory threshold to %d MB for %s: %w", limit, c.id, err)
	}
	if err := c.guestExec.UpdateMemory(ctx, limit); err != nil {
		return fmt.Errorf("failed to set memory to %d MB for %s: %w", limit, c.id, err)
	}

	c.guestExec.RecordMemoryState(limit, limit)
	return nil
}
