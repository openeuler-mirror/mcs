package container

func sandboxStorageFromSandbox(sandbox *Sandbox, createdAt int64, shimPID int) SandboxStorage {
	return SandboxStorage{
		ID:        sandbox.id,
		State:     sandbox.state,
		Config:    *sandbox.config,
		Network:   networkStorageFromSandbox(sandbox),
		CreatedAt: createdAt,
		ShimPID:   shimPID,
	}
}

func networkStorageFromSandbox(sandbox *Sandbox) NetworkConfig {
	switch netCfg := sandbox.network.(type) {
	case *NetworkConfig:
		return *netCfg
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

func containerStorageFromContainer(container *Container) ContainerStorage {
	return ContainerStorage{
		ID:            container.id,
		SandboxID:     container.sandbox.SandboxID(),
		State:         container.state,
		Config:        *container.config,
		Mounts:        container.mounts,
		ContainerPath: container.containerPath,
	}
}
