package container

import (
	"micrun/internal/ports"
	log "micrun/internal/support/logger"
)

type SandboxConfig struct {
	ID                 string
	Hostname           string
	NetworkConfig      NetworkConfig
	PedConfig          PedestalConfig
	ContainerConfigs   map[string]*ContainerConfig
	Annotations        map[string]string
	SharedMemorySize   uint64
	EnableVCPUsPinning bool
	SharedCPUPool      bool
	StaticResourceMgmt bool
	HugePageSupport    bool
	InfraOnly          bool

	GuestControl      ports.GuestControl      `json:"-"`
	HypervisorControl ports.HypervisorControl `json:"-"`
	StateStore        ports.StateStore        `json:"-"`
	Dependencies      *Dependencies           `json:"-"`
}

func (sc *SandboxConfig) valid() bool {
	if sc.ID == "" {
		log.Warn("sandbox ID is empty")
		return false
	}

	if sc.PedConfig.PedType == PedestalUnsupported && !sc.InfraOnly {
		log.Warn("pedestal type is unsupported")
		return false
	}

	return true
}
