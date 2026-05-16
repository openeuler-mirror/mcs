package oci

import (
	defs "micrun/internal/support/definitions"
	log "micrun/internal/support/logger"
)

func (r *RuntimeConfig) SetMaxContainerVCPUs(cpuString string) {
	vcpu, ok := parseRuntimeUint32(KeyMaxContainerVCPU, cpuString)
	if !ok {
		r.MaxContainerVCPUs = defs.DefaultMaxVCPUs
		return
	}
	if vcpu == 0 {
		log.Debugf("max container cpus parsed as 0, defaulting to %d", defs.DefaultMaxVCPUs)
		r.MaxContainerVCPUs = defs.DefaultMaxVCPUs
		return
	}
	r.MaxContainerVCPUs = vcpu
}

func (r *RuntimeConfig) SetHugePageSupport(hugePageStr string) {
	hugePage, ok := parseRuntimeBool(KeyHugePage, hugePageStr)
	if !ok {
		return
	}
	r.HugePageSupport = hugePage
}

func (r *RuntimeConfig) SetStaticResourceManagement(staticResourceStr string) {
	staticResource, ok := parseRuntimeBool(KeyStaticResource, staticResourceStr)
	if !ok {
		return
	}
	r.StaticResourceManagement = staticResource
}

func (r *RuntimeConfig) SetSharedCPUPool(sharedCPUPoolStr string) {
	sharedCPUPool, ok := parseRuntimeBool(KeySharedCPUPool, sharedCPUPoolStr)
	if !ok {
		return
	}
	r.SharedCPUPool = sharedCPUPool
}
