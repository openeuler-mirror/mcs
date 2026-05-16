package container

import (
	"context"
	"fmt"
	"micrun/internal/ports"
	"sync"
)

const (
	okCode = 0
)

var (
	_ SandboxTraits   = (*Sandbox)(nil)
	_ ContainerTraits = (*Container)(nil)
)

type SandboxStorage struct {
	ID      string        `json:"id"`
	State   SandboxState  `json:"state"`
	Config  SandboxConfig `json:"config"`
	Network NetworkConfig `json:"network"`

	CreatedAt int64 `json:"created_at,omitempty"`
	ShimPID   int   `json:"shim_pid,omitempty"`
}

type Sandbox struct {
	ctx        context.Context
	resManager sandboxResource
	stateRepo  stateRepository
	deps       *Dependencies
	config     *SandboxConfig
	containers map[string]*Container
	id         string
	network    Network
	state      SandboxState

	guestControl      ports.GuestControl
	hypervisorControl ports.HypervisorControl

	vcpuAlreadyPinned bool
	networkCleaned    bool

	lifecycleLock sync.Mutex
	annotLock     sync.RWMutex
	wg            *sync.WaitGroup
}

func (s *Sandbox) stateRepositoryChecked() (stateRepository, error) {
	if s != nil && s.stateRepo.store != nil {
		return s.stateRepo, nil
	}
	if s != nil && s.config != nil && s.config.StateStore != nil {
		return stateRepositoryFromStore(s.config.StateStore), nil
	}
	if s != nil && s.deps != nil {
		return stateRepositoryFromDependenciesChecked(s.deps)
	}
	return stateRepository{}, fmt.Errorf("container: no state repository available; dependencies not configured")
}

func (s *Sandbox) dependenciesChecked() (*Dependencies, error) {
	if s == nil || s.deps == nil {
		return nil, fmt.Errorf("container: dependencies not configured; pass Dependencies via SandboxConfig")
	}
	return s.deps, nil
}
