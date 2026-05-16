package container

import (
	"context"
	"fmt"
	defs "micrun/internal/support/definitions"
	er "micrun/internal/support/errors"
	"micrun/internal/support/lockutil"
	log "micrun/internal/support/logger"
	"os"
	"path/filepath"
)

type ContainerStorage struct {
	ID            string          `json:"id"`
	SandboxID     string          `json:"sandbox_id"`
	State         ContainerState  `json:"state"`
	Config        ContainerConfig `json:"config"`
	Mounts        []Mount         `json:"mounts"`
	ContainerPath string          `json:"container_path"`
}

func (c *Container) setContainerState(ctx context.Context, state StateString) error {
	if state == "" {
		return fmt.Errorf("state cannot be empty")
	}
	if !state.known() {
		return fmt.Errorf("unknown state: %s", state)
	}

	if err := c.requireSandbox(); err != nil {
		return err
	}

	c.state.State = state
	c.updateExitNotifier(state)
	if err := c.saveState(ctx); err != nil {
		log.Errorf("failed to save container state: %v", err)
		return err
	}
	if err := c.sandbox.StoreSandbox(ctx); err != nil {
		log.Errorf("failed to save sandbox state: %v", err)
		return err
	}
	return nil
}

func (c *Container) updateExitNotifier(state StateString) {
	lockutil.WithLock(&c.exitNotifierMu, func() {
		c.applyExitNotifierState(state)
	})
}

func (c *Container) applyExitNotifierState(state StateString) {
	switch state {
	case StateStopped, StateDown:
		if c.exitNotifier != nil {
			close(c.exitNotifier)
			c.exitNotifier = nil
		}
	default:
		if c.exitNotifier == nil {
			c.exitNotifier = make(chan struct{})
		}
	}
}

func (c *Container) exitNotifierForState(state StateString) chan struct{} {
	var notifier chan struct{}
	lockutil.WithLock(&c.exitNotifierMu, func() {
		c.applyExitNotifierState(state)
		notifier = c.exitNotifier
	})
	return notifier
}

func (c *Container) checkState() StateString {
	state, err := c.checkStateWithError()
	if err != nil {
		if c == nil {
			return StateDown
		}
		log.Warnf("failed to check container %s state: %v", c.id, err)
		return c.state.State
	}
	return state
}

func (c *Container) checkStateWithError() (StateString, error) {
	return c.checkStateWithContext(c.ctx)
}

func (c *Container) checkStateWithContext(ctx context.Context) (StateString, error) {
	if c == nil || c.id == "" {
		return StateDown, nil
	}

	if c.config != nil && c.config.IsInfra {
		return c.state.State, nil
	}

	if c.sandbox == nil || c.sandbox.guestControl == nil {
		return c.state.State, nil
	}

	ctx = queryContext(ctx)
	exists, err := c.sandbox.guestControl.Exists(ctx, c.id)
	if err != nil {
		return c.state.State, err
	}
	if !exists {
		if c.state.State != StateDown {
			if err := c.setContainerState(ctx, StateDown); err != nil {
				log.Warnf("failed to mark container %s as down: %v", c.id, err)
			}
		}
		return StateDown, nil
	}

	return c.state.State, nil
}

func (c *Container) SaveState() error {
	return c.saveState(c.ctx)
}

func (c *Container) saveState(ctx context.Context) error {
	repo, err := c.stateRepositoryChecked()
	if err != nil {
		return err
	}
	if err := repo.SaveContainer(ctx, c); err != nil {
		return fmt.Errorf("failed to save container state: %w", err)
	}
	return nil
}

func (c *Container) RestoreState() error {
	if c == nil {
		return er.ContainerNotFound
	}
	return c.restoreState(c.ctx)
}

func (c *Container) restoreState(ctx context.Context) error {
	if c == nil {
		return er.ContainerNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.normalizeContextFromSandbox()

	repo, err := c.stateRepositoryChecked()
	if err != nil {
		return err
	}
	storage, err := repo.LoadContainer(ctx, c.id, c.containerPath, c.legacyBundleStatePath())
	if err != nil {
		return err
	}
	return c.applyRestoredContainerState(storage)
}

func (c *Container) applyRestoredContainerState(storage *ContainerStorage) error {
	if storage == nil {
		return fmt.Errorf("container state storage is nil")
	}
	if storage.ID == "" {
		storage.ID = c.id
	} else if storage.ID != c.id {
		return fmt.Errorf("container ID mismatch: %v != %v", storage.ID, c.id)
	}
	if c.sandbox != nil && storage.SandboxID != "" && storage.SandboxID != c.sandbox.id {
		return fmt.Errorf("container sandbox ID mismatch: %v != %v", storage.SandboxID, c.sandbox.id)
	}
	if !storage.State.State.known() {
		return fmt.Errorf("container state invalid: %s", storage.State.State)
	}
	if storage.Config.ID == "" {
		storage.Config.ID = storage.ID
	} else if storage.Config.ID != storage.ID {
		return fmt.Errorf("container config ID mismatch: %v != %v", storage.Config.ID, storage.ID)
	}

	c.state = storage.State
	c.mounts = storage.Mounts
	c.config = &storage.Config
	c.containerPath = storage.ContainerPath
	if c.containerPath == "" && c.sandbox != nil {
		c.containerPath = filepath.Join(c.sandbox.id, c.id)
	}
	c.rootfs = c.config.Rootfs
	c.normalizeContextFromSandbox()
	c.normalizeGuestExecutorFromSandbox()
	c.updateExitNotifier(c.state.State)

	return nil
}

func (c *Container) normalizeContextFromSandbox() {
	if c != nil && c.ctx == nil && c.sandbox != nil {
		c.ctx = c.sandbox.ctx
	}
}

func (c *Container) normalizeGuestExecutorFromSandbox() {
	if c == nil || c.guestExec != nil || c.sandbox == nil {
		return
	}
	deps, err := c.sandbox.dependenciesChecked()
	if err != nil || deps.GuestExecutorFactory == nil {
		return
	}
	c.guestExec = deps.GuestExecutorFactory(c.id)
}

func (c *Container) DeleteState(ctx context.Context) error {
	repo, err := c.stateRepositoryChecked()
	if err != nil {
		return err
	}
	return repo.DeleteContainer(ctx, c.id, c.containerPath, c.legacyBundleStatePath())
}

func (c *Container) legacyBundleStatePath() string {
	if c == nil || c.containerPath == "" {
		return ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		log.Warnf("failed to get current working directory: %v", err)
		return ""
	}
	return filepath.Join(cwd, c.containerPath, defs.MicrunContainerStateFile)
}
