package pedestal

import (
	"micrun/pkg/cpuset"
)

// baremetal is the baremetal pedestal implementation using openAMP/rpmsg.
// Linux exposes all physical CPUs; RTOS clients run on reserved cores.
type baremetal struct {
	DefaultPedestal
}

func (baremetal) Type() PedType { return Baremetal }

func (baremetal) String() string { return "baremetal" }

func (baremetal) GeneratePedConf() string {
	return "zephyr.elf"
}

func (baremetal) MaxCPUNum() uint32 {
	inv := HostCPUCounts()
	return inv.Physical
}

func (baremetal) HostCPUSeta() cpuset.CPUSet {
	return cpuset.NewCPUSet()
}

func (baremetal) MemLowThreshold() uint32 {
	return 2
}

func (baremetal) MemHighThreshold() uint32 {
	_, total := MemoryMB()
	if total < 2 {
		return 2
	}
	return total
}

func (baremetal) MemoryMB() (free, total uint32) {
	return MemoryMB()
}
