package container

import (
	"context"
	"fmt"

	er "micrun/internal/support/errors"
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
	if c.sandbox.state.State != StateRunning {
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

	applyLinuxResourceConfig(c.config.Resources, resources)

	if err := c.sandbox.updateResources(ctx); err != nil {
		return fmt.Errorf("update sandbox resources for %s: %w", c.id, err)
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

	limit := c.config.memoryLimitMB()
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
