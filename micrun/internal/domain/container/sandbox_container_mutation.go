package container

import (
	"context"
	"errors"
	"fmt"

	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
)

func (s *Sandbox) CreateContainer(ctx context.Context, config ContainerConfig) (_ ContainerTraits, err error) {
	id := config.ID
	if s == nil {
		return nil, er.SandboxNotFound
	}
	if id == "" {
		return nil, er.EmptyContainerID
	}
	if s.config == nil {
		return nil, fmt.Errorf("sandbox config is nil")
	}
	if s.containers == nil {
		s.containers = make(map[string]*Container)
	}
	if _, ok := s.containers[id]; ok {
		log.Errorf("container %s already exists", id)
		return nil, er.AlreadyExists
	}

	if s.config.ContainerConfigs == nil {
		s.config.ContainerConfigs = make(map[string]*ContainerConfig)
	}
	s.config.ContainerConfigs[id] = &config
	if s.config.InfraOnly && !config.IsInfra {
		s.config.InfraOnly = false
	}
	newc := s.config.ContainerConfigs[id]

	defer func() {
		if err != nil && s.config != nil && len(s.config.ContainerConfigs) > 0 {
			delete(s.config.ContainerConfigs, id)
		}
	}()

	c, err := newContainerWithContext(ctx, s, newc)
	if err != nil {
		return nil, err
	}
	if err := c.validateMicaContainer(); err != nil {
		return nil, fmt.Errorf("invalid mica container %s: %w", c.ID(), err)
	}
	if err = c.create(ctx); err != nil {
		return nil, err
	}
	if err = s.addContainer(c); err != nil {
		return nil, err
	}

	defer func() {
		if err == nil {
			return
		}
		log.Errorf("failed to create container %s: %v", id, err)
		if cleanupErr := s.cleanupFailedContainerCreate(ctx, c); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()

	if err = s.checkVCPUsPinning(ctx); err != nil {
		return nil, err
	}
	if err = s.persistSandboxState(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) cleanupFailedContainerCreate(ctx context.Context, c *Container) error {
	if c == nil {
		return nil
	}

	var errs []error
	if errStop := c.stop(ctx, true); errStop != nil {
		log.Errorf("failed to stop container %s after creation failure: %v", c.id, errStop)
		errs = append(errs, fmt.Errorf("stop container %s after creation failure: %w", c.id, errStop))
	}
	log.Debug("remove stopped container from sandbox")
	if errRemove := s.removeContainer(c.id); errRemove != nil {
		log.Errorf("failed to remove container %s after creation failure: %v", c.id, errRemove)
		errs = append(errs, fmt.Errorf("remove container %s after creation failure: %w", c.id, errRemove))
	}
	return errors.Join(errs...)
}

func (s *Sandbox) DeleteContainer(ctx context.Context, containerID string) (ContainerTraits, error) {
	log.Debugf("delete container %s from sandbox", containerID)
	c, err := s.containerByID(containerID)
	if err != nil {
		return nil, err
	}
	if err := c.delete(ctx); err != nil {
		return nil, err
	}

	s.removeContainerResources(containerID)
	if err := s.updateResources(ctx); err != nil {
		log.Debugf("ignore updateResources error after delete %s: %v", containerID, err)
	}
	if err := s.checkVCPUsPinning(ctx); err != nil {
		return nil, err
	}
	if err := s.persistSandboxState(ctx); err != nil {
		return nil, err
	}
	return c, nil
}
