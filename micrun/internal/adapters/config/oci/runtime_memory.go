package oci

import log "micrun/internal/support/logger"

func (r *RuntimeConfig) SetMaxContainerMemMB(memString string) {
	host := r.host()
	mem, ok := parseRuntimeMemoryMB(KeyMaxMemory, memString, host)
	if !ok {
		r.MaxContainerMemMB = host.MemHighThreshold
		return
	}

	r.MaxContainerMemMB = mem
}

func (r *RuntimeConfig) SetMinContainerMemMB(memString string) {
	host := r.host()
	mem, ok := parseRuntimeMemoryMB(KeyMinMemory, memString, host)
	if !ok {
		r.MinContainerMemMB = host.MemLowThreshold
		return
	}

	r.MinContainerMemMB = mem
}

func parseRuntimeMemoryMB(key string, value string, hostProfile HostProfile) (uint32, bool) {
	mem, ok := parseRuntimeUint32(key, value)
	if !ok {
		return 0, false
	}
	if !memoryWithinHostBounds(mem, hostProfile) {
		return 0, false
	}
	return mem, true
}

func memoryWithinHostBounds(memoryMB uint32, hostProfile HostProfile) bool {
	host := normalizeHostProfile(hostProfile)
	high := host.MemHighThreshold
	low := host.MemLowThreshold

	if memoryMB > high {
		log.Debugf("runtime memory %dMB exceeds host high threshold %dMB", memoryMB, high)
		return false
	}

	if memoryMB < low {
		log.Debugf("runtime memory %dMB is below host low threshold %dMB", memoryMB, low)
		return false
	}

	return true
}
