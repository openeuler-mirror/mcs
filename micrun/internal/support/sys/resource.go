package sys

const maxSafeQuotaForMilliCPU = int64(^uint64(0) / 1000)

func CalculateVCpusFromMilliCpus(mCPU uint32) uint32 {
	if mCPU == 0 {
		return 0
	}
	// Guard against overflow: mCPU+999 wraps when mCPU > maxUint32-999,
	// yielding 0 instead of the intended large count. Same class as
	// requiredCPUCount (container_resource_common.go) and
	// vcpuCountFromCapacity (planner.go).
	if mCPU > ^uint32(0)-999 {
		return ^uint32(0) / 1000
	}
	return (mCPU + 999) / 1000
}

func CalculateMilliCPUs(quota int64, period uint64) uint32 {
	if quota < 0 || period == 0 {
		return 0
	}
	if quota > maxSafeQuotaForMilliCPU {
		quota = maxSafeQuotaForMilliCPU
	}
	mCPU := (uint64(quota) * 1000) / period
	// Clamp again before the uint32 conversion: the quota clamp only bounds
	// the multiplication, (maxSafe*1000)/1 still exceeds uint32 and would
	// wrap to a garbage value.
	if mCPU > uint64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(mCPU)
}
