package container

import (
	"context"
	"errors"
	"fmt"
)

func (s *Sandbox) initContainers(ctx context.Context) error {
	entries, err := s.containerConfigEntries()
	if err != nil {
		return err
	}

	for _, entry := range entries {
		cc := entry.config
		if s.config.InfraOnly && !cc.IsInfra {
			s.config.InfraOnly = false
		}

		c, err := newContainerWithContext(ctx, s, cc)
		if err != nil {
			return err
		}
		if err := c.validateMicaContainer(); err != nil {
			return fmt.Errorf("invalid mica container %s: %w", c.ID(), err)
		}
		if err := c.create(ctx); err != nil {
			// Mirror the CreateContainer path (sandbox_container_mutation.go):
			// registerClient may have created the guest domain and persisted
			// state already, so the failed container must be torn down too —
			// Sandbox.Delete only iterates the containers map and would never
			// see it, leaking the live domain as an orphan.
			if cleanupErr := s.cleanupFailedContainerCreate(ctx, c); cleanupErr != nil {
				err = errors.Join(err, cleanupErr)
			}
			return err
		}
		if err := s.addContainer(c); err != nil {
			return err
		}
	}

	if err := s.updateResources(ctx); err != nil {
		return err
	}
	if err := s.checkVCPUsPinning(ctx); err != nil {
		return err
	}
	if err := s.persistSandboxState(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Sandbox) loadContainersToSandbox(ctx context.Context) error {
	entries, err := s.containerConfigEntries()
	if err != nil {
		return err
	}

	for _, entry := range entries {
		cc := entry.config
		c, err := newContainerWithContext(ctx, s, cc)
		if err != nil {
			return err
		}
		if err := s.addContainer(c); err != nil {
			return err
		}
	}

	return nil
}
