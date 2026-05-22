package container

import (
	"context"
	"fmt"

	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"

	"github.com/hashicorp/go-multierror"
)

func (s *Sandbox) Start(ctx context.Context) error {
	if s == nil {
		return er.SandboxNotFound
	}
	s.lifecycleLock.Lock()
	defer s.lifecycleLock.Unlock()

	cur := s.state.State
	log.Debugf("current sandbox state=%s", cur)

	if cur == StateCreating {
		if err := s.setSandboxState(StateReady); err != nil {
			return err
		}
		cur = s.state.State
	}

	if cur == StateRunning {
		return s.restartContainersIfStopped(ctx)
	}
	return s.transitionToRunning(ctx, cur)
}

func (s *Sandbox) restartContainersIfStopped(ctx context.Context) error {
	log.Debugf("sandbox %s already running, checking containers", s.id)
	containers, err := s.lifecycleContainers()
	if err != nil {
		return err
	}
	for _, c := range containers {
		state, err := c.checkStateWithContext(ctx)
		if err != nil {
			return err
		}
		if state != StateRunning {
			if err := c.start(ctx); err != nil {
				return err
			}
		}
	}
	return s.StoreSandbox(ctx)
}

func (s *Sandbox) transitionToRunning(ctx context.Context, cur StateString) error {
	containers, err := s.lifecycleContainers()
	if err != nil {
		return err
	}
	if err := s.state.Transition(cur, StateRunning); err != nil {
		log.Debugf("transition error: from=%s to=%s", cur, StateRunning)
		return err
	}

	oldState := cur
	if err := s.setSandboxState(StateRunning); err != nil {
		return fmt.Errorf("set Sandbox state error: %w", err)
	}
	log.Debugf("sandbox state: %s -> %s", oldState, s.state.State)

	var startErr error
	defer func() {
		if startErr != nil {
			if rollbackErr := s.setSandboxState(oldState); rollbackErr != nil {
				log.Warnf("failed to rollback sandbox state to %s: %v", oldState, rollbackErr)
			}
		}
	}()

	for _, c := range containers {
		if startErr = c.start(ctx); startErr != nil {
			return startErr
		}
	}
	return s.StoreSandbox(ctx)
}

func (s *Sandbox) Stop(ctx context.Context, force bool) error {
	if s == nil {
		return er.SandboxNotFound
	}
	s.lifecycleLock.Lock()
	defer s.lifecycleLock.Unlock()

	if s.state.State == StateStopped {
		return nil
	}

	originalState := s.state.State
	if err := s.state.Transition(originalState, StateStopped); err != nil {
		return err
	}

	if err := s.stopContainers(ctx, force); err != nil {
		return err
	}

	log.Debug("stop monitor and console")

	if err := s.removeNetwork(); err != nil && !force {
		return err
	}

	if err := s.setSandboxState(StateStopped); err != nil {
		return err
	}
	log.Debugf("sandbox state: %s -> %s", originalState, StateStopped)

	if err := s.StoreSandbox(ctx); err != nil {
		return fmt.Errorf("save sandbox state during Stop: %w", err)
	}

	return nil
}

func (s *Sandbox) Delete(ctx context.Context) error {
	if s == nil {
		return er.SandboxNotFound
	}
	s.lifecycleLock.Lock()
	defer s.lifecycleLock.Unlock()

	if s.state.State != StateReady &&
		s.state.State != StatePaused &&
		s.state.State != StateStopped {
		return er.SandboxNotReady
	}

	var result *multierror.Error
	containers, err := s.lifecycleContainers()
	if err != nil {
		result = multierror.Append(result, err)
	}
	for _, c := range containers {
		if err := c.delete(ctx); err != nil {
			log.Errorf("failed to delete container %s: %v", c.id, err)
			result = multierror.Append(result, fmt.Errorf("delete container %s: %w", c.id, err))
		}
	}

	if err := s.removeNetwork(); err != nil {
		log.Warnf("failed to remove network for sandbox %s: %v", s.id, err)
		result = multierror.Append(result, fmt.Errorf("remove sandbox network: %w", err))
	}

	if err := s.cleanSandboxStorage(ctx); err != nil {
		result = multierror.Append(result, err)
	}
	return result.ErrorOrNil()
}

func (s *Sandbox) removeNetwork() error {
	if s == nil {
		return er.SandboxNotFound
	}
	if s.config == nil {
		return nil
	}

	// Idempotent: Stop() and Delete() can both call removeNetwork().
	if s.networkCleaned {
		return nil
	}

	log.Infof("remove network for sandbox %s", s.id)
	if err := s.config.NetworkConfig.NetworkCleanup(s.id); err != nil {
		return err
	}
	s.networkCleaned = true
	return nil
}

func (s *Sandbox) stopContainers(ctx context.Context, force bool) error {
	if s == nil {
		return er.SandboxNotFound
	}
	log.Infof("stopping client os in sandbox %s", s.id)
	containers, err := s.lifecycleContainers()
	if err != nil {
		return err
	}
	for _, c := range containers {
		if err := c.stop(ctx, force); err != nil {
			log.Errorf("failed to stop container %s: %v", c.id, err)
			return err
		}
	}
	return nil
}

func (s *Sandbox) lifecycleContainers() ([]*Container, error) {
	entries, err := s.containerEntries()
	if err != nil {
		return nil, err
	}

	containers := make([]*Container, 0, len(entries))
	for _, entry := range entries {
		containers = append(containers, entry.container)
	}
	return containers, nil
}
