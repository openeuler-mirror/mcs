package container

import (
	"context"
	"fmt"
	"micrun/internal/ports"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
	"micrun/internal/support/timex"
)

func startClient(ctx context.Context, c *Container) error {
	if c == nil {
		return er.ContainerNotFound
	}
	if c.config == nil {
		return fmt.Errorf("container config is nil")
	}
	if err := c.requireGuestControl(); err != nil {
		return err
	}
	if c.guestExec == nil {
		return fmt.Errorf("guest executor is nil")
	}
	if _, err := c.ensureClientPresenceWithContext(ctx); err != nil {
		return err
	}

	start := timex.Now(c.clock())
	if err := c.sandbox.guestControl.Start(ctx, c.id); err != nil {
		log.Errorf("startClient: Start failed: %v", err)
		return err
	}

	if err := c.applyInitialCPUSettings(ctx); err != nil {
		return err
	}
	if err := c.setupMemory(ctx); err != nil {
		return err
	}
	log.Infof("startClient: Start OK in %s", timex.Now(c.clock()).Sub(start))

	return nil
}

func (c *Container) clock() timex.Clock {
	if c == nil || c.sandbox == nil || c.sandbox.deps == nil {
		return nil
	}
	return c.sandbox.deps.Now
}

func (c *Container) applyInitialCPUSettings(ctx context.Context) error {
	if c == nil || c.config == nil {
		return nil
	}
	if err := c.requireSandbox(); err != nil {
		return err
	}
	if c.guestExec == nil {
		return fmt.Errorf("guest executor is nil")
	}

	targetVCPUs := c.config.VCPUNum
	if targetVCPUs == 0 {
		targetVCPUs = 1
	}
	if _, _, err := c.guestExec.UpdateVCPUNum(ctx, targetVCPUs); err != nil {
		hc := c.sandbox.hypervisorControl
		if hc == nil || hc.Type() != ports.HypervisorXen {
			return fmt.Errorf("failed to apply initial vcpu count for %s: %w", c.id, err)
		}
		log.Warnf("mica vcpu update failed for %s, falling back to xl: %v", c.id, err)
		if xlErr := hc.SetVCPUCount(ctx, c.id, targetVCPUs); xlErr != nil {
			return fmt.Errorf("failed to apply initial vcpu count for %s via mica: %w (xl also failed: %w)", c.id, err, xlErr)
		}
	}

	return nil
}

func createMicaClientConf(container *Container) (GuestClientConfig, error) {
	if container == nil {
		return GuestClientConfig{}, er.ContainerNotFound
	}
	if container.config == nil {
		return GuestClientConfig{}, fmt.Errorf("container config is nil")
	}
	config := container.config
	pedType := config.PedestalType
	cpus := container.GetClientCPU()
	cpuCap := config.cpuCapacity()
	vcpus := config.VCPUNum
	if vcpus == 0 {
		vcpus = 1
	}
	memMB := config.containerMaxMemMB()
	if err := ensureFirmwarePath(config.ImageAbsPath); err != nil {
		return GuestClientConfig{}, fmt.Errorf("firmware validation failed: %w", err)
	}

	return GuestClientConfig{
		CPU:             cpus,
		CPUCapacity:     cpuCap,
		CPUWeight:       ShareToWeight(config.cpuShares()),
		VCPUs:           vcpus,
		MaxVCPUs:        config.MaxVcpuNum,
		MemoryMB:        memMB,
		MemoryThreshold: config.MemoryThresholdMB,
		Name:            container.id,
		Path:            config.ImageAbsPath,
		Ped:             pedType.String(),
		PedCfg:          config.PedestalConf,
	}, nil
}
