package pedestal

import (
	"math/bits"

	"micrun/internal/support/cpuset"
	log "micrun/internal/support/logger"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// quotaPeriodToCapacity converts a Linux CPU quota/period pair into the
// "capacity" unit (hundredths of a vCPU). It clamps at uint32 max and rejects
// negative quota to mirror domain/container.cpuCapacityFromQuotaPeriod so the
// value reported to the hypervisor never overflows or wraps.
func quotaPeriodToCapacity(quota int64, period uint64) uint32 {
	if quota <= 0 || period == 0 {
		return 0
	}
	const num2CapRatio = 100
	whole := uint64(quota) / period
	if whole > uint64(^uint32(0))/uint64(num2CapRatio) {
		return ^uint32(0)
	}
	capacity := whole * uint64(num2CapRatio)
	if remainder := uint64(quota) % period; remainder != 0 {
		hi, lo := bits.Mul64(remainder, uint64(num2CapRatio))
		fraction, _ := bits.Div64(hi, lo, period)
		capacity += fraction
	}
	if capacity > uint64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(capacity)
}

type resourcePlanner interface {
	FromSpec(spec *specs.Spec) *EssentialResource
}

// PlanEssentialResources returns the essential resource view for this pedestal facade.
func (f *PedestalFacade) PlanEssentialResources(spec *specs.Spec) *EssentialResource {
	if spec == nil || spec.Linux == nil || spec.Linux.Resources == nil {
		return InitResource()
	}
	if f == nil {
		return defaultPlanner{}.FromSpec(spec)
	}
	return plannerForType(f.Type()).FromSpec(spec)
}

func plannerForType(pedType PedType) resourcePlanner {
	switch pedType {
	case Xen:
		return xenPlanner{}
	default:
		return defaultPlanner{}
	}
}

type xenPlanner struct{}

func (xenPlanner) FromSpec(spec *specs.Spec) *EssentialResource {
	return linuxResourceToEssential(spec, true)
}

type defaultPlanner struct{}

func (defaultPlanner) FromSpec(spec *specs.Spec) *EssentialResource {
	return linuxResourceToEssential(spec, false)
}

func linuxResourceToEssential(spec *specs.Spec, convertShares bool) *EssentialResource {
	res := InitResource()
	if spec == nil || spec.Linux == nil || spec.Linux.Resources == nil {
		return res
	}

	mapCPUResources(res, spec.Linux.Resources.CPU, convertShares)
	mapMemoryResources(res, spec.Linux.Resources.Memory)

	return res
}

func mapCPUResources(res *EssentialResource, cpu *specs.LinuxCPU, convertShares bool) {
	if cpu == nil {
		res.CPUWeight = nil
		return
	}

	vcpuNum := mapCPUSet(res, cpu)
	mapCPUQuota(res, cpu, vcpuNum)
	mapCPUShares(res, cpu, convertShares)
}

func mapCPUSet(res *EssentialResource, cpu *specs.LinuxCPU) uint32 {
	cpus, cpuSetVCpuNum := validateCPUSet(cpu.Cpus)
	if cpus != "" && cpuSetVCpuNum > 0 {
		res.ClientCPUSet = cpus
		*res.VCPU = cpuSetVCpuNum
		return cpuSetVCpuNum
	}
	return 0
}

func mapCPUQuota(res *EssentialResource, cpu *specs.LinuxCPU, vcpuFromSet uint32) {
	if cpu.Quota == nil || *cpu.Quota <= 0 || cpu.Period == nil || *cpu.Period <= 0 {
		log.Debugf("cpu quota/period pair = < %v:%v > is incomplete", cpu.Quota, cpu.Period)
		if vcpuFromSet > 0 {
			*res.CPUCapacity = vcpuFromSet * 100
		}
		return
	}

	rawCapacity := quotaPeriodToCapacity(*cpu.Quota, uint64(*cpu.Period))
	if rawCapacity == 0 {
		return
	}

	vcpuNum := vcpuFromSet
	if vcpuNum == 0 {
		vcpuNum = vcpuCountFromCapacity(rawCapacity)
	}

	if vcpuNum > 0 {
		maxByCpuset := vcpuNum * 100
		if rawCapacity > maxByCpuset {
			rawCapacity = maxByCpuset
		}
		*res.VCPU = vcpuNum
	}

	*res.CPUCapacity = rawCapacity
}

func mapCPUShares(res *EssentialResource, cpu *specs.LinuxCPU, convertShares bool) {
	switch {
	case cpu.Shares != nil && *cpu.Shares > 0 && convertShares:
		weight := ShareToWeight(*cpu.Shares)
		res.CPUWeight = &weight
	case cpu.Shares != nil && *cpu.Shares > 0:
		// Clamp before narrowing: shares is uint64 and a bare uint32()
		// conversion silently wraps (e.g. 2^32 → weight 0).
		shares := *cpu.Shares
		if max := uint64(^uint32(0)); shares > max {
			shares = max
		}
		share := uint32(shares)
		res.CPUWeight = &share
	case convertShares:
		weight := uint32(DefaultXenWeight)
		res.CPUWeight = &weight
	default:
		res.CPUWeight = nil
	}
}

func mapMemoryResources(res *EssentialResource, mem *specs.LinuxMemory) {
	if mem != nil && mem.Limit != nil && *mem.Limit > 0 {
		// Clamp to uint32 to avoid silent truncation for extreme (>4.5 PiB)
		// limits. Mirrors bytesToMiB in container_resource_common.go.
		mib := *mem.Limit >> 20
		if mib > int64(^uint32(0)) {
			mib = int64(^uint32(0))
		}
		*res.MemoryMaxMB = uint32(mib)
	}
}

func validateCPUSet(s string) (validSet string, vcpus uint32) {
	set, err := cpuset.Parse(s)
	if err != nil {
		return "", 0
	}
	return set.String(), uint32(set.Size())
}

func vcpuCountFromCapacity(capacity uint32) uint32 {
	if capacity == 0 {
		return 1
	}
	// Guard against overflow: see requiredCPUCount in container_resource_common.go.
	if capacity > ^uint32(0)-99 {
		return ^uint32(0) / 100
	}
	return (capacity + 99) / 100
}
