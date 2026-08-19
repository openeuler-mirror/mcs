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

	if mem := u.resources.Memory; mem != nil {
		// A non-positive limit (cgroup "unlimited"/-1 semantics) must not be
		// applied: bytesToMiB maps it to 0, which would push the guest
		// memory down to 0 MiB — the exact opposite of "unlimited". Skip it.
		if mem.Limit != nil && *mem.Limit > 0 {
			limitMiB := bytesToMiB(mem.Limit)
			res.MemoryMaxMB = copyUint32(limitMiB)
			hasUpdates = true
		}
		// A memory reservation has no live counterpart on the mica control
		// protocol: the only memory fields are Memory (current) and MaxMemory
		// (threshold), both driven by Limit via EnsureMemoryLimit. A changed
		// reservation is therefore config-only — applyTo persists it, and
		// registerClient uses it as the initial memory the next time the guest
		// domain is created (see the memoryReservationMB fallback there).
		// hasUpdates is still set so that persistence actually runs.
		//
		// A non-positive reservation (cgroup "unlimited"/-1 semantics) must
		// not be applied: bytesToMiB maps it to 0, which the guest would
		// interpret as an explicit 0 MiB floor — allowing the balloon driver
		// to shrink guest memory to 0 and OOM. Skip it, matching the Limit
		// path above.
		if mem.Reservation != nil && *mem.Reservation > 0 {
			hasUpdates = true
		}
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

	// Match the > 0 guard in changes(): a non-positive Limit (cgroup
	// "unlimited"/-1) must not be written to the config, or the guest never
	// gets it but the config/disk records -1 — bytesToMiB(-1)=0 makes the
	// restart path use the default memory instead of the real allocation.
	if mem := u.resources.Memory; mem != nil {
		if mem.Limit != nil && *mem.Limit > 0 {
			if res.Memory == nil {
				res.Memory = &specs.LinuxMemory{}
			}
			res.Memory.Limit = copyInt64(mem.Limit)
		}
		if mem.Reservation != nil && *mem.Reservation > 0 {
			if res.Memory == nil {
				res.Memory = &specs.LinuxMemory{}
			}
			res.Memory.Reservation = copyInt64(mem.Reservation)
		}
	}
}
