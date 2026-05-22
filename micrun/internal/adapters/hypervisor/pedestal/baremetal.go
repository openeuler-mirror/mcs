package pedestal

import (
	"context"
	"runtime"

	"micrun/internal/support/cpuset"
)

// baremetal is the baremetal pedestal implementation.
// Linux exposes all physical CPUs; RTOS clients run on reserved cores.
// NOTE: Baremetal mode is not yet supported. This is a placeholder for future implementation.
type baremetal struct {
	DefaultPedestal
}

func (baremetal) Type() PedType { return Baremetal }

func (baremetal) String() string { return "baremetal" }

func (baremetal) GeneratePedConf() string {
	return "zephyr.elf"
}

func (baremetal) MaxCPUNum(context.Context) uint32 {
	return uint32(runtime.NumCPU())
}

func (baremetal) HostCPUSeta(context.Context) cpuset.CPUSet {
	return cpuset.NewCPUSet()
}

func (baremetal) MemLowThreshold() uint32 {
	return 2
}

func (baremetal) MemHighThreshold(ctx context.Context) uint32 {
	_, total := MemoryMB(ctx)
	if total < 2 {
		return 2
	}
	return total
}

func (baremetal) MemoryMB(ctx context.Context) (free, total uint32) {
	return MemoryMB(ctx)
}
