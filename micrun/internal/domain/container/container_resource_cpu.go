package container

import (
	"math/bits"

	"micrun/internal/support/cpuset"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func (cfg *ContainerConfig) ensureCPU() *specs.LinuxCPU {
	res := cfg.ensureResources()
	if res == nil {
		return nil
	}
	if res.CPU == nil {
		res.CPU = &specs.LinuxCPU{}
	}
	return res.CPU
}

func (cfg *ContainerConfig) cpuCapacity() uint32 {
	cpu := cfg.cpuSpec()
	if cpu == nil || cpu.Quota == nil || cpu.Period == nil || *cpu.Period == 0 {
		return 0
	}
	if *cpu.Quota <= 0 {
		return 0
	}

	return cpuCapacityFromQuotaPeriod(*cpu.Quota, *cpu.Period)
}

func (cfg *ContainerConfig) cpuShares() uint64 {
	cpu := cfg.cpuSpec()
	if cpu == nil || cpu.Shares == nil {
		return 0
	}
	return *cpu.Shares
}

func (cfg *ContainerConfig) cpuMask() string {
	cpu := cfg.cpuSpec()
	if cpu == nil {
		return ""
	}
	return cpu.Cpus
}

func cpuCapacityFromQuotaPeriod(quota int64, period uint64) uint32 {
	if quota <= 0 || period == 0 {
		return 0
	}

	whole := uint64(quota) / period
	if whole > uint64(maxUint32)/uint64(num2CapRatio) {
		return maxUint32
	}

	capacity := whole * uint64(num2CapRatio)
	remainder := uint64(quota) % period
	if remainder != 0 {
		hi, lo := bits.Mul64(remainder, uint64(num2CapRatio))
		fraction, _ := bits.Div64(hi, lo, period)
		capacity += fraction
	}
	if capacity > uint64(maxUint32) {
		return maxUint32
	}
	return uint32(capacity)
}

// CPUCapacity reports the configured CPU capacity in units of 0.01 CPUs.
// 映射关系：Container Quota/Period (1:100) -> RTOS Client CPU Capacity (百分比)
// 100% 表示占满一个 vCPU
func (cfg *ContainerConfig) CPUCapacity() uint32 {
	return cfg.cpuCapacity()
}

// CPUShares reports the configured CPU shares weight.
// 映射关系：Container CPU Share (1024:256) -> RTOS Client CPU Weight
// 转换比例：1024 (cgroup默认) : 256 (Xen默认) = 4:1
func (cfg *ContainerConfig) CPUShares() uint64 {
	return cfg.cpuShares()
}

// CPUSet returns the configured CPU affinity mask.
// 映射关系：Container cpuset -> RTOS Client CPUS
// 格式："0-3" 或 "0,1,3"，表示硬亲和性设置
func (cfg *ContainerConfig) CPUSet() string {
	return cfg.cpuMask()
}

type normalizedCPUSet struct {
	Mask       string
	VCPUs      uint32
	ApplyVCPUs bool
}

func normalizeCPUSetWithLimit(mask string, fallbackVCPUs uint32, maxCPUs uint32) normalizedCPUSet {
	if mask == "" {
		return normalizedCPUSet{VCPUs: fallbackVCPUs}
	}

	cpus, err := parseCPUSetMask(mask)
	if err != nil {
		return normalizedCPUSet{Mask: mask, VCPUs: fallbackVCPUs}
	}

	ok, outOfRange := cpusetRangeValidWithLimit(cpus, maxCPUs)
	if ok {
		return normalizedCPUSet{
			Mask:       cpuset.NewCPUSet(cpus...).String(),
			VCPUs:      uint32(len(cpus)),
			ApplyVCPUs: len(cpus) > 0,
		}
	}

	valid := filterValidCPUs(cpus, outOfRange)
	if len(valid) == 0 {
		return normalizedCPUSet{VCPUs: fallbackVCPUs, ApplyVCPUs: true}
	}
	return normalizedCPUSet{
		Mask:       cpuset.NewCPUSet(valid...).String(),
		VCPUs:      uint32(len(valid)),
		ApplyVCPUs: true,
	}
}

func filterValidCPUs(cpus []int, outOfRange []int) []int {
	if len(cpus) == 0 {
		return nil
	}

	bad := make(map[int]struct{}, len(outOfRange))
	for _, cpu := range outOfRange {
		bad[cpu] = struct{}{}
	}

	valid := make([]int, 0, len(cpus))
	for _, cpu := range cpus {
		if _, outOfRange := bad[cpu]; outOfRange {
			continue
		}
		valid = append(valid, cpu)
	}

	return valid
}

func parseCPUSetMask(mask string) ([]int, error) {
	set, err := cpuset.Parse(mask)
	if err != nil {
		return nil, err
	}
	return set.ToSlice(), nil
}
