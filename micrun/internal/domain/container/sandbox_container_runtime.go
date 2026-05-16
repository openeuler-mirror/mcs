package container

import (
	"context"
	"fmt"

	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func (s *Sandbox) StartContainer(ctx context.Context, containerID string) (ContainerTraits, error) {
	c, err := s.containerByID(containerID)
	if err != nil {
		return nil, err
	}

	if err := c.start(ctx); err != nil {
		return nil, err
	}
	if err := s.persistSandboxState(ctx); err != nil {
		return nil, err
	}
	if err := s.checkVCPUsPinning(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) StopContainer(ctx context.Context, containerID string, force bool) (ContainerTraits, error) {
	c, err := s.containerByID(containerID)
	if err != nil {
		return nil, err
	}

	if err := c.stop(ctx, force); err != nil {
		return nil, err
	}
	if err := s.persistSandboxState(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) KillContainer(ctx context.Context, containerID string) (ContainerTraits, error) {
	c, err := s.containerByID(containerID)
	if err != nil {
		return nil, err
	}
	if s.guestControl == nil {
		return nil, fmt.Errorf("guest control is nil")
	}

	exists, err := s.guestControl.Exists(ctx, c.id)
	if err != nil {
		return nil, err
	}
	if !exists {
		return c, nil
	}
	if err := c.kill(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) PauseContainer(ctx context.Context, containerID string) error {
	c, err := s.containerByID(containerID)
	if err != nil {
		return err
	}

	if err := c.pause(ctx); err != nil {
		return err
	}
	return s.persistSandboxState(ctx)
}

func (s *Sandbox) ResumeContainer(ctx context.Context, containerID string) error {
	c, err := s.containerByID(containerID)
	if err != nil {
		return err
	}

	if err := c.resume(ctx); err != nil {
		return err
	}
	return s.persistSandboxState(ctx)
}

func (s *Sandbox) UpdateContainer(ctx context.Context, containerID string, resources specs.LinuxResources) error {
	log.Debugf("UpdateContainer: container=%s, resources=%+v", containerID, resources)

	if s == nil {
		return er.SandboxNotFound
	}
	if s.config == nil {
		return fmt.Errorf("sandbox config is nil")
	}
	if s.config.StaticResourceMgmt {
		log.Debugf("UpdateContainer ignored in static resource management mode")
		return nil
	}

	c, err := s.containerByID(containerID)
	if err != nil {
		return err
	}

	if err := c.update(ctx, resources); err != nil {
		return fmt.Errorf("update container %s resources: %w", containerID, err)
	}
	if err := s.checkVCPUsPinning(ctx); err != nil {
		return fmt.Errorf("update container %s CPU pinning: %w", containerID, err)
	}
	if err := s.persistSandboxState(ctx); err != nil {
		return fmt.Errorf("persist sandbox after updating container %s: %w", containerID, err)
	}
	return nil
}
