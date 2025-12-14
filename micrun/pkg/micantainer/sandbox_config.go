package micantainer

import (
	log "micrun/logger"
	ped "micrun/pkg/pedestal"
)

// NOTICE:
// in current minimal branch, micrun remove sandbox resource fields, and workload statsitics,
type SandboxConfig struct {
	ID                 string
	Hostname           string
	NetworkConfig      NetworkConfig
	PedConfig          ped.PedestalConfig
	ContainerConfigs   map[string]*ContainerConfig
	Annotations        map[string]string
	SharedMemorySize   uint64
	EnableVCPUsPinning bool
	SharedCPUPool      bool
	StaticResourceMgmt bool
	HugePageSupport    bool
	InfraOnly          bool
}

func (sc *SandboxConfig) valid() bool {
	if sc.ID == "" {
		log.Warn("sandbox ID is empty")
		return false
	}

	if sc.PedConfig.PedType == ped.Unsupported && !sc.InfraOnly {
		log.Warn("pedestal type is unsupported")
		return false
	}

	return true
}
