package container

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	defs "micrun/internal/support/definitions"
	er "micrun/internal/support/errors"
	"micrun/internal/support/fs"
	log "micrun/internal/support/logger"
)

func (c *Container) start(ctx context.Context) error {
	currentState, err := c.ensureClientPresenceWithContext(ctx)
	if err != nil {
		return err
	}

	if c.isInfra() {
		return c.startInfra(ctx, currentState)
	}
	return c.startGuest(ctx, currentState)
}

func (c *Container) startInfra(ctx context.Context, currentState StateString) error {
	if currentState == StateRunning {
		return nil
	}
	if !canStartFrom(currentState) {
		return er.ContainerNotReady
	}
	if err := c.state.Transition(currentState, StateRunning); err != nil {
		return err
	}
	return c.setContainerState(ctx, StateRunning)
}

func (c *Container) startGuest(ctx context.Context, currentState StateString) error {
	if currentState == StateRunning {
		return fmt.Errorf("container %s is already running", c.id)
	}
	if !canStartFrom(currentState) {
		return er.ContainerNotReady
	}
	if err := c.requireGuestControl(); err != nil {
		return err
	}
	if err := c.state.Transition(currentState, StateRunning); err != nil {
		return err
	}
	if err := startClient(ctx, c); err != nil {
		log.Warnf("failed to start container: %v, stopping it", err)
		if stopErr := c.stop(ctx, true); stopErr != nil {
			log.Warn("failed to stop the container after start failed.")
			return errors.Join(err, fmt.Errorf("stop container after start failure: %w", stopErr))
		}
		return err
	}
	return c.setContainerState(ctx, StateRunning)
}

func canStartFrom(state StateString) bool {
	return state == StateReady || state == StateStopped
}

func (c *Container) create(ctx context.Context) error {
	if c.isInfra() {
		return c.setContainerState(ctx, StateReady)
	}

	if _, err := c.ensureClientPresenceWithContext(ctx); err != nil {
		return err
	}

	return c.setContainerState(ctx, StateReady)
}

func (c *Container) doStop(ctx context.Context, force bool) error {
	if c.isInfra() {
		return c.stopInfraProcess()
	}
	if err := c.requireGuestControl(); err != nil {
		return err
	}

	currentState, err := c.checkStateWithContext(ctx)
	if err != nil {
		return err
	}
	if currentState == StateStopped {
		log.Debugf("Container %s is already stopped.", c.id)
		return nil
	}
	if err := c.state.Transition(currentState, StateStopped); err != nil && !force {
		return err
	}
	return c.sandbox.guestControl.Stop(ctx, c.ID())
}

func (c *Container) stopInfraProcess() error {
	if c.infraCmd == nil || c.infraCmd.Process == nil {
		return nil
	}
	err := c.infraCmd.Process.Signal(syscall.SIGTERM)
	if err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func (c *Container) stop(ctx context.Context, force bool) error {
	if _, err := c.ensureClientPresenceWithContext(ctx); err != nil {
		return err
	}

	if err := c.doStop(ctx, force); err != nil {
		if force && isBenignForcedStopError(err) {
			log.Debugf("container %s was already exiting during forced stop: %v", c.id, err)
			return c.setContainerState(ctx, StateStopped)
		}
		log.Debugf("failed to stop container %s: %v", c.id, err)
		return err
	}
	log.Debugf("container %s stopped", c.id)

	return c.setContainerState(ctx, StateStopped)
}

func isBenignForcedStopError(err error) bool {
	return errors.Is(err, syscall.ECONNRESET)
}

func (c *Container) kill(ctx context.Context) error {
	if err := c.requireSandbox(); err != nil {
		return err
	}
	if c.sandbox.state.State != StateReady && c.sandbox.state.State != StateRunning {
		return er.SandboxNotReady
	}
	if err := c.requireGuestControl(); err != nil {
		return err
	}

	currentState, err := c.ensureClientPresenceWithContext(ctx)
	if err != nil {
		return err
	}
	log.Debugf("Container state is %s.", currentState)

	exists, err := c.sandbox.guestControl.Exists(ctx, c.id)
	if err != nil {
		return err
	}
	if !exists {
		return c.setContainerState(ctx, StateStopped)
	}
	if err := c.doStop(ctx, true); err != nil {
		log.Debugf("failed to stop container %s: %v", c.id, err)
		return err
	}
	log.Debugf("container %s stopped", c.id)

	return c.setContainerState(ctx, StateStopped)
}

func (c *Container) delete(ctx context.Context) error {
	if err := c.requireSandbox(); err != nil {
		return err
	}
	currentState, err := c.ensureClientPresenceWithContext(ctx)
	if err != nil {
		return err
	}
	if !canDeleteFrom(currentState) {
		return er.SandboxNotReady
	}

	if !c.isInfra() {
		if err := c.requireGuestControl(); err != nil {
			return err
		}
		if err := c.removeGuestClientForDelete(ctx, currentState); err != nil {
			log.Debugf("failed to remove container %s.", err)
			return err
		}
	}
	return c.cleanupAfterDelete(ctx)
}

func (c *Container) removeGuestClientForDelete(ctx context.Context, currentState StateString) error {
	removeCtx := ctx
	cancel := func() {}
	if currentState == StateStopped {
		removeCtx, cancel = context.WithTimeout(ctx, 2*time.Second)
	}
	defer cancel()

	if err := c.sandbox.guestControl.Remove(removeCtx, c.id); err != nil {
		if currentState == StateStopped {
			log.Debugf("best-effort remove for stopped container %s did not complete: %v", c.id, err)
			return nil
		}
		return err
	}
	return nil
}

func (c *Container) cleanupAfterDelete(ctx context.Context) error {
	if err := c.sandbox.removeContainer(c.id); err != nil {
		return err
	}
	var cleanupErr error
	if err := c.sandbox.StoreSandbox(ctx); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("failed to store sandbox after deleting container %s: %w", c.id, err))
	}
	if err := c.DeleteState(ctx); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("failed to delete container state: %w", err))
	}
	if err := fs.RemoveContainerCacheDirAt(c.cacheRoot(), c.id); err != nil {
		log.Warnf("failed to remove cache directory for container %s: %v", c.id, err)
	}
	return cleanupErr
}

func (c *Container) cacheRoot() string {
	if c != nil && c.config != nil && c.config.CacheRoot != "" {
		return c.config.CacheRoot
	}
	return defs.DefaultMicaContainersRoot
}

func canDeleteFrom(state StateString) bool {
	return state == StateReady || state == StatePaused || state == StateStopped
}

func (c *Container) isInfra() bool {
	return c.config != nil && c.config.IsInfra
}

func (c *Container) pause(ctx context.Context) error {
	currentState, err := c.ensureClientPresenceWithContext(ctx)
	if err != nil {
		return err
	}
	if currentState != StateRunning {
		return er.ContainerNotRunning
	}
	if c.config != nil && c.config.IsInfra {
		return c.setContainerState(ctx, StatePaused)
	}
	if err := c.requireGuestControl(); err != nil {
		return err
	}
	if err := c.sandbox.guestControl.Pause(ctx, c.id); err != nil {
		return er.MicadOpFailed
	}
	return c.setContainerState(ctx, StatePaused)
}

func (c *Container) resume(ctx context.Context) error {
	if err := c.requireSandbox(); err != nil {
		return err
	}

	currentState, err := c.ensureClientPresenceWithContext(ctx)
	if err != nil {
		return err
	}
	if currentState != StatePaused && c.sandbox.state.State != StateStopped {
		return er.ContainerNotPaused
	}
	if c.config != nil && c.config.IsInfra {
		return c.setContainerState(ctx, StateRunning)
	}
	if err := c.requireGuestControl(); err != nil {
		return err
	}

	log.Debugf("resuming container %s (restarting RTOS)", c.id)
	if err := c.sandbox.guestControl.Resume(ctx, c.id); err != nil {
		return er.MicadOpFailed
	}
	return c.setContainerState(ctx, StateRunning)
}

func (c *Container) Signal(ctx context.Context, signal syscall.Signal) error {
	if err := c.requireSandbox(); err != nil {
		return err
	}
	if c.sandbox.notOperational() {
		return er.SandboxNotReady
	}
	if !c.isInfra() {
		if err := c.requireGuestControl(); err != nil {
			return err
		}
	}

	currentState, err := c.ensureClientPresenceWithContext(ctx)
	if err != nil {
		return err
	}
	if currentState != StateRunning && currentState != StateReady && currentState != StatePaused {
		return er.GuestNotReady
	}

	return nil
}

func (c *Container) requireGuestControl() error {
	if err := c.requireSandbox(); err != nil {
		return err
	}
	if c.sandbox.guestControl == nil {
		return fmt.Errorf("guest control is nil")
	}
	return nil
}
