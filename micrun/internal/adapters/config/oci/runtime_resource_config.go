package oci

import defs "micrun/internal/support/definitions"

type RuntimeResourceConfig struct {
	MaxContainerVCPUs uint32
	// MaxContainerMemMB is the initial memory threshold, not the init max available memory of RTOS.
	MaxContainerMemMB uint32
	MinContainerMemMB uint32

	HugePageSupport          bool
	StaticResourceManagement bool
	SharedCPUPool            bool
}

func defaultRuntimeResourceConfig(hostProfile HostProfile) RuntimeResourceConfig {
	return RuntimeResourceConfig{
		StaticResourceManagement: hostProfile.staticResourceDefault(),
		MinContainerMemMB:        defs.DefaultContainerMinMemMB,
		MaxContainerVCPUs:        defs.DefaultMaxVCPUs,
	}
}
