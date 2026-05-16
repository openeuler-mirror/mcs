package container

import (
	"context"
	"errors"
	"fmt"
	"sync"

	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
)

func (s *Sandbox) StoreSandbox(ctx context.Context) error {
	repo, err := s.stateRepositoryChecked()
	if err != nil {
		return err
	}
	if err := repo.SaveSandbox(ctx, s); err != nil {
		log.Errorf("StoreSandbox: failed to save sandbox %s: %v", s.id, err)
		return err
	}
	return nil
}

func (s *Sandbox) cleanSandboxStorage(ctx context.Context) error {
	if s.id == "" {
		return er.EmptySandboxID
	}
	repo, err := s.stateRepositoryChecked()
	if err != nil {
		return err
	}
	return repo.DeleteSandbox(ctx, s.id)
}

func (s *Sandbox) restore() error {
	repo, err := s.stateRepositoryChecked()
	if err != nil {
		return err
	}
	ss, err := repo.LoadSandbox(s.ctx, s.id)

	if err != nil {
		if errors.Is(err, er.SandboxNotFound) {
			log.Debugf("sandbox state not found: %v", err)
			return nil
		}
		return fmt.Errorf("failed to restore sandbox state: %w", err)
	}

	if ss != nil {
		if err := s.applyRestoredSandboxState(ss, repo); err != nil {
			return err
		}
	}

	return nil
}

func (s *Sandbox) applyRestoredSandboxState(ss *SandboxStorage, repo stateRepository) error {
	if ss.ID != s.id {
		log.Tracef("sandbox ID mismatch: %v != %v", ss.ID, s.id)
		log.Pretty("%v", ss)
		return fmt.Errorf("sandbox ID mismatch: %v != %v", ss.ID, s.id)
	}
	if !ss.State.Valid() {
		return fmt.Errorf("sandbox state invalid: %s", ss.State.State)
	}
	if ss.Config.ID == "" {
		ss.Config.ID = ss.ID
	} else if ss.Config.ID != ss.ID {
		return fmt.Errorf("sandbox config ID mismatch: %v != %v", ss.Config.ID, ss.ID)
	}

	s.state.Ped = ss.State.Ped
	s.state.Version = ss.State.Version
	s.state.State = ss.State.State
	s.config = &ss.Config
	s.config.NetworkConfig = ss.Network
	s.normalizeRestoredRuntime(repo)
	return nil
}

func (s *Sandbox) normalizeRestoredRuntime(repo stateRepository) {
	if s.containers == nil {
		s.containers = make(map[string]*Container)
	}
	s.resManager.ensureMaps()
	if s.wg == nil {
		s.wg = &sync.WaitGroup{}
	}

	if s.config == nil {
		if s.network == nil {
			s.network = &dummyNetwork{}
		}
		return
	}
	if repo.store != nil {
		s.config.StateStore = repo.store
	}
	if s.config.Dependencies == nil {
		s.config.Dependencies = s.deps
	}
	if s.config.GuestControl == nil {
		s.config.GuestControl = s.guestControl
	}
	if s.config.HypervisorControl == nil {
		s.config.HypervisorControl = s.hypervisorControl
	}
	if s.config.ContainerConfigs == nil {
		s.config.ContainerConfigs = make(map[string]*ContainerConfig)
	}
	s.network = &s.config.NetworkConfig
}

func restoreSandboxWithDependencies(ctx context.Context, id string, deps *Dependencies) (*SandboxStorage, error) {
	repo, err := stateRepositoryFromDependenciesChecked(deps)
	if err != nil {
		return nil, err
	}
	return repo.LoadSandbox(ctx, id)
}
