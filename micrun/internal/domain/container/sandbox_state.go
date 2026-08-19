package container

import (
	"context"
	"errors"
	"fmt"

	er "micrun/internal/support/errors"
	"micrun/internal/support/lockutil"
	log "micrun/internal/support/logger"
)

func (s *Sandbox) StoreSandbox(ctx context.Context) error {
	if s.storageRemoved.Load() {
		log.Debugf("StoreSandbox: sandbox %s storage already deleted, skipping persist", s.id)
		return nil
	}
	repo, err := s.stateRepositoryChecked()
	if err != nil {
		return err
	}
	// Re-check immediately before the write: cleanSandboxStorage sets the flag
	// and then deletes the state files between the check above and this point.
	// Narrowing the resurrection window to a single function call prevents a
	// late persist from recreating files a same-id Create would load as stale
	// state. Mirrors the container-level saveState guard.
	if s.storageRemoved.Load() {
		log.Debugf("StoreSandbox: sandbox %s storage deleted during persist, skipping write", s.id)
		return nil
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
	// Set the flag BEFORE deleting the files so a persist racing the deletion
	// is skipped instead of recreating the state files afterwards. A persist
	// that already passed the check may still land between the flag and the
	// delete; it is removed by the delete below.
	s.storageRemoved.Store(true)
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

	lockutil.WithLock(&s.stateMu, func() {
		s.state.Ped = ss.State.Ped
		s.state.Version = ss.State.Version
		s.state.State = ss.State.State
	})
	s.config = &ss.Config
	s.config.NetworkConfig = ss.Network
	// Stash the combined-format container records for one-shot consumption by
	// each container's restoreState during rebuild. nil for pre-combined
	// documents — containers then fall back to their legacy per-file state.
	if ss.Containers != nil {
		records := make(map[string]ContainerRuntimeRecord, len(ss.Containers))
		for id, record := range ss.Containers {
			records[id] = record
		}
		lockutil.WithLock(&s.containersLock, func() {
			s.restoredContainers = records
		})
	}
	s.normalizeRestoredRuntime(repo)
	return nil
}

// takeRestoredContainerRecord consumes (removes and returns) the restored
// runtime record for a container id. One-shot by design: after the rebuild
// consumed it, a later same-id Create must start fresh instead of
// resurrecting the pre-restart state.
func (s *Sandbox) takeRestoredContainerRecord(id string) (ContainerRuntimeRecord, bool) {
	var record ContainerRuntimeRecord
	var ok bool
	lockutil.WithLock(&s.containersLock, func() {
		record, ok = s.restoredContainers[id]
		if ok {
			delete(s.restoredContainers, id)
		}
	})
	return record, ok
}

func (s *Sandbox) normalizeRestoredRuntime(repo stateRepository) {
	if s.containers == nil {
		s.containers = make(map[string]*Container)
	}
	lockutil.WithLock(&s.resMu, func() { s.resManager.ensureMaps() })
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
