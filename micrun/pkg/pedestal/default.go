package pedestal

import (
	"runtime"

	"github.com/shirou/gopsutil/v3/mem"
	"micrun/pkg/cpuset"
)

// DefaultPedestal provides default implementations for core Pedestal interface.
// Pedestal implementations can embed this type to inherit shared behavior.
type DefaultPedestal struct{}

func (DefaultPedestal) Type() PedType { return Unsupported }

func (DefaultPedestal) String() string { return "unsupported" }

func (DefaultPedestal) GeneratePedConf() string { return "" }

func (DefaultPedestal) MaxCPUNum() uint32 {
	return uint32(runtime.NumCPU())
}

func (DefaultPedestal) HostCPUSeta() cpuset.CPUSet {
	return cpuset.NewCPUSet(0)
}

func (DefaultPedestal) MemLowThreshold() uint32 {
	return 2
}

func (DefaultPedestal) MemHighThreshold() uint32 {
	v, _ := mem.VirtualMemory()
	total := uint32(v.Total >> 20)
	if total < 2 {
		return 2
	}
	return total
}

func (DefaultPedestal) MemoryMB() (free, total uint32) {
	v, _ := mem.VirtualMemory()
	free = uint32(v.Free >> 20)
	total = uint32(v.Total >> 20)
	return
}
