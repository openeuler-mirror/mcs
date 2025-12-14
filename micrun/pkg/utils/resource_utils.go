package utils

// CalculateVCpusFromMilliCpus converts from mCPU to CPU, taking the ceiling
// value when necessary
func CalculateVCpusFromMilliCpus(mCPU uint32) uint32 {
	return (mCPU + 999) / 1000
}

// CalculateMilliCPUs converts CPU quota and period to milli-CPUs
func CalculateMilliCPUs(quota int64, period uint64) uint32 {

	// If quota is -1, it means the CPU resource request is
	// unconstrained.  In that case, we don't currently assign
	// additional CPUs.
	if quota >= 0 && period != 0 {
		return uint32((uint64(quota) * 1000) / period)
	}

	return 0
}
