package sys

func CalculateVCpusFromMilliCpus(mCPU uint32) uint32 {
	return (mCPU + 999) / 1000
}

func CalculateMilliCPUs(quota int64, period uint64) uint32 {
	if quota >= 0 && period != 0 {
		return uint32((uint64(quota) * 1000) / period)
	}
	return 0
}
