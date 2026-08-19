package container

import (
	"github.com/opencontainers/runtime-spec/specs-go"
	"micrun/internal/support/lockutil"
)

func sandboxStorageFromSandbox(sandbox *Sandbox, createdAt int64, shimPID int) SandboxStorage {
	// ContainerConfigs and Annotations are maps mutated under containersLock
	// (e.g. by concurrent CreateContainer/DeleteContainer). A bare struct copy
	// shares the map headers, so json.Marshal iterating them later would race
	// with a concurrent writer and crash the shim. Snapshot the maps under the
	// read lock so the returned SandboxStorage owns an independent copy.
	var records map[string]ContainerRuntimeRecord
	cfg := lockutil.WithReadLockValue(&sandbox.containersLock, func() SandboxConfig {
		c := *sandbox.config
		if c.ContainerConfigs != nil {
			copied := make(map[string]*ContainerConfig, len(c.ContainerConfigs))
			for k, v := range c.ContainerConfigs {
				copied[k] = cloneContainerConfigForStorage(v)
			}
			c.ContainerConfigs = copied
		}
		if c.Annotations != nil {
			copied := make(map[string]string, len(c.Annotations))
			for k, v := range c.Annotations {
				copied[k] = v
			}
			c.Annotations = copied
		}
		// Snapshot container runtime state under the same lock so the
		// combined document is internally consistent. Nesting container
		// stateMu reads under containersLock matches allContainersTerminal.
		records = make(map[string]ContainerRuntimeRecord, len(sandbox.containers))
		for id, cont := range sandbox.containers {
			if cont == nil {
				continue
			}
			records[id] = ContainerRuntimeRecord{
				State:         cont.snapshotState(),
				Mounts:        cont.mounts,
				ContainerPath: cont.containerPath,
			}
		}
		return c
	})

	return SandboxStorage{
		ID:         sandbox.id,
		State:      sandbox.snapshotState(),
		Config:     cfg,
		Network:    networkStorageFromSandbox(sandbox),
		Containers: records,
		CreatedAt:  createdAt,
		ShimPID:    shimPID,
	}
}

func networkStorageFromSandbox(sandbox *Sandbox) NetworkConfig {
	switch netCfg := sandbox.network.(type) {
	case *NetworkConfig:
		// removeNetwork mutates these fields under containersLock; copy
		// them under the same lock so the snapshot is never torn.
		return lockutil.WithReadLockValue(&sandbox.containersLock, func() NetworkConfig {
			return *netCfg
		})
	case *dummyNetwork:
		return NetworkConfig{
			NetworkID:      netCfg.NetID(),
			NetworkCreated: netCfg.NetworkIsCreated(),
		}
	default:
		if sandbox.config == nil {
			return NetworkConfig{}
		}
		return sandbox.config.NetworkConfig
	}
}

// cloneContainerConfigForStorage returns a deep copy of cfg with independent
// Resources (CPU/Memory/HugepageLimits) so json.Marshal iterating the copy
// after containersLock is released does not race with a concurrent
// UpdateContainer/setVcpuAffinity writing the same fields.
func cloneContainerConfigForStorage(cfg *ContainerConfig) *ContainerConfig {
	if cfg == nil {
		return nil
	}
	copied := *cfg
	if cfg.Resources != nil {
		res := &specs.LinuxResources{}
		res.CPU = cloneLinuxCPU(cfg.Resources.CPU)
		res.Memory = cloneLinuxMemory(cfg.Resources.Memory)
		if len(cfg.Resources.HugepageLimits) > 0 {
			res.HugepageLimits = append([]specs.LinuxHugepageLimit(nil), cfg.Resources.HugepageLimits...)
		}
		copied.Resources = res
	}
	return &copied
}
