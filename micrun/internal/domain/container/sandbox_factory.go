package container

import (
	"context"
	"fmt"
	"sync"

	defs "micrun/internal/support/definitions"
	log "micrun/internal/support/logger"
)

const maxHostnameLength = 64

func CreateSandbox(ctx context.Context, cfg *SandboxConfig) (*Sandbox, error) {
	s, err := createSandboxFromConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func newSandbox(ctx context.Context, config SandboxConfig) (sb *Sandbox, retErr error) {
	if !config.valid() {
		return nil, fmt.Errorf("invalid sandbox configuration")
	}
	if config.Dependencies == nil {
		return nil, fmt.Errorf("sandbox configuration requires non-nil Dependencies")
	}
	var stateRepo stateRepository
	if config.StateStore == nil {
		repo, err := stateRepositoryFromDependenciesChecked(config.Dependencies)
		if err != nil {
			return nil, err
		}
		stateRepo = repo
		config.StateStore = repo.store
	} else {
		if err := config.Dependencies.Validate(); err != nil {
			return nil, err
		}
		stateRepo = stateRepositoryFromStore(config.StateStore)
	}
	config.StateStore = stateRepo.store

	s := &Sandbox{
		ctx:        context.Background(),
		stateRepo:  stateRepo,
		deps:       config.Dependencies,
		config:     &config,
		containers: make(map[string]*Container),
		id:         config.ID,
		state: SandboxState{
			State:   StateCreating,
			Ped:     config.PedConfig.PedType.String(),
			Version: defs.SandboxVersion,
		},
		resManager:        *newResMgmt(),
		wg:                &sync.WaitGroup{},
		guestControl:      config.GuestControl,
		hypervisorControl: config.HypervisorControl,
	}

	if s.config != nil {
		s.network = &s.config.NetworkConfig
	} else {
		s.network = &dummyNetwork{}
	}

	if err := s.restore(); err != nil {
		return nil, fmt.Errorf("failed to restore sandbox %s: %w", s.id, err)
	}
	return s, nil
}

func createSandbox(ctx context.Context, config *SandboxConfig) (*Sandbox, error) {
	s, err := newSandbox(ctx, *config)
	if err != nil {
		return nil, err
	}

	if s.state.State == StateReady || s.state.State == StateRunning {
		log.Debugf("sandbox already in ready/running state, creation finished.")
		return s, nil
	}

	hostname := s.config.Hostname
	if len(hostname) > maxHostnameLength {
		hostname = hostname[:maxHostnameLength]
	}
	s.config.Hostname = hostname

	if err := s.setSandboxState(StateReady); err != nil {
		return nil, err
	}

	return s, nil
}

func createSandboxFromConfig(ctx context.Context, config *SandboxConfig) (_ *Sandbox, err error) {
	s, err := createSandbox(ctx, config)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			log.Debugf("hooked delete sandbox!")
			if s != nil {
				if deleteErr := s.Delete(ctx); deleteErr != nil {
					log.Warnf("failed to delete sandbox during cleanup: %v", deleteErr)
				}
			}
		}
	}()

	defer func() {
		if err != nil {
			if removeErr := s.removeNetwork(); removeErr != nil {
				log.Warnf("failed to remove network during cleanup: %v", removeErr)
			}
		}
	}()

	if err = s.initContainers(ctx); err != nil {
		log.Debugf("failed to init containers: %v", err)
		return nil, err
	}

	return s, nil
}
