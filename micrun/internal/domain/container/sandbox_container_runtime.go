package container

import (
	"context"
	"errors"
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

	// Serialize with Sandbox Stop/Delete (same lifecycleLock as
	// CreateContainer): task-level claimLifecycle is per task id, so a pod
	// container Start is not serialized with the sandbox task's teardown.
	// Without this lock, start's ensureClientPresence can re-create the
	// guest domain after Stop/Delete already destroyed it, leaving an
	// untracked Xen/micad domain on a sandbox that reports Stopped.
	s.lifecycleLock.Lock()
	defer s.lifecycleLock.Unlock()
	if s.notOperational() {
		return nil, er.SandboxNotReady
	}

	if err := c.start(ctx); err != nil {
		return nil, err
	}
	// pin/persist run after guest is already Running. On failure, stop the
	// container so task stays CREATED without an orphaned Running domain —
	// a bare error would leave Start non-retryable (startGuest returns
	// "already running") while the task never reaches RUNNING.
	if err := s.persistSandboxState(ctx); err != nil {
		rollbackCtx := context.WithoutCancel(ctx)
		if stopErr := c.stop(rollbackCtx, true); stopErr != nil {
			return nil, errors.Join(err, fmt.Errorf("stop container %s after persist failure: %w", containerID, stopErr))
		}
		return nil, err
	}
	if err := s.checkVCPUsPinning(ctx); err != nil {
		rollbackCtx := context.WithoutCancel(ctx)
		if stopErr := c.stop(rollbackCtx, true); stopErr != nil {
			return nil, errors.Join(err, fmt.Errorf("stop container %s after pin failure: %w", containerID, stopErr))
		}
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

	// Delegate the exists-check and state transition to c.kill, which already
	// marks the container Stopped when the guest domain is gone. A separate
	// pre-check here would short-circuit and leave a stale RUNNING state.
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
