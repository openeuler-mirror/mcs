package container

import "github.com/opencontainers/runtime-spec/specs-go"

type linuxResourceUpdate struct {
	resources specs.LinuxResources
}

func newLinuxResourceUpdate(resources specs.LinuxResources) linuxResourceUpdate {
	return linuxResourceUpdate{resources: resources}
}

func (u linuxResourceUpdate) changes() (*ResourceChanges, bool) {
	res := &ResourceChanges{}
	hasUpdates := false

	if cpu := u.resources.CPU; cpu != nil {
		if cpu.Period != nil && *cpu.Period != 0 {
			hasUpdates = true
		}
		if cpu.Quota != nil && *cpu.Quota != 0 {
			if cpu.Period != nil && *cpu.Period != 0 {
				capacity := cpuCapacityFromQuotaPeriod(*cpu.Quota, *cpu.Period)
				res.CPUCapacity = copyUint32(capacity)
				if capacity > 0 {
					res.VCPU = copyUint32(requiredCPUCount(capacity))
				}
			}
			hasUpdates = true
		}
		if cpu.Cpus != "" {
			res.ClientCPUSet = cpu.Cpus
			hasUpdates = true
		}
		if cpu.Shares != nil {
			weight := ShareToWeight(*cpu.Shares)
			weightCopy := weight
			res.CPUWeight = &weightCopy
			hasUpdates = true
		}
	}

	if mem := u.resources.Memory; mem != nil && mem.Limit != nil {
		limitMiB := uint32(*mem.Limit >> 20)
		res.MemoryMinMB = limitMiB
		res.MemoryMaxMB = copyUint32(limitMiB)
		hasUpdates = true
	}

	return res, hasUpdates
}

func (u linuxResourceUpdate) applyTo(res *specs.LinuxResources) {
	if res == nil {
		return
	}
	if cpu := u.resources.CPU; cpu != nil {
		if res.CPU == nil {
			res.CPU = &specs.LinuxCPU{}
		}
		if cpu.Period != nil && *cpu.Period != 0 {
			res.CPU.Period = copyUint64(cpu.Period)
		}
		if cpu.Quota != nil && *cpu.Quota != 0 {
			res.CPU.Quota = copyInt64(cpu.Quota)
		}
		if cpu.Cpus != "" {
			res.CPU.Cpus = cpu.Cpus
		}
		if cpu.Shares != nil {
			sharesCopy := *cpu.Shares
			res.CPU.Shares = &sharesCopy
		}
	}

	if mem := u.resources.Memory; mem != nil && mem.Limit != nil {
		if res.Memory == nil {
			res.Memory = &specs.LinuxMemory{}
		}
		res.Memory.Limit = copyInt64(mem.Limit)
	}
}
