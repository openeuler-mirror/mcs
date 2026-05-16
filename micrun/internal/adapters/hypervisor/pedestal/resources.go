package pedestal

import (
	"context"
	defs "micrun/internal/support/definitions"
	"runtime"
)

// EssentialResource contains essential resource specifications for a client.
type EssentialResource struct {
	CPUCapacity  *uint32
	CPUWeight    *uint32
	ClientCPUSet string
	VCPU         *uint32
	MemoryMaxMB  *uint32
	MemoryMinMB  uint32
	VIF          []string
}

// default value of essential resource struct, not about runtime config.
const defaultVcpus = 1

func InitResource() *EssentialResource {
	res := EssentialResource{}
	vcpu := uint32(defaultVcpus)
	capacity := uint32(0)
	maxmem := uint32(defs.DefaultMinMemMB)
	cpuWeight := uint32(0)
	res.VCPU = &vcpu
	res.CPUCapacity = &capacity
	res.CPUWeight = &cpuWeight
	res.MemoryMaxMB = &maxmem
	return &res
}

// HostCPUInventory captures the static CPU counts visible to micrun.
type HostCPUInventory struct {
	// Physical represents the physical CPUs the pedestal can schedule.
	Physical uint32
	// LinuxVisible is the CPU count reported to the Linux side (Dom0 or baremetal kernel).
	LinuxVisible uint32
}

// CPUCounts returns CPU counts for this bound pedestal facade.
func (f *PedestalFacade) CPUCounts(ctx context.Context) HostCPUInventory {
	if f == nil {
		return HostCPUInventory{}
	}
	physical := f.MaxCPUNum(ctx)
	linux := min(uint32(runtime.NumCPU()), physical)
	return HostCPUInventory{
		Physical:     physical,
		LinuxVisible: linux,
	}
}

// ClientCPUCapacity returns the number of CPUs micrun can hand to RTOS clients.
// Baremetal pedestals currently expose the full physical CPU count.
// The exclusiveDom0CPU parameter indicates whether Dom0 CPUs should be reserved exclusively (Xen only).
func (f *PedestalFacade) ClientCPUCapacity(ctx context.Context, exclusiveDom0CPU bool) uint32 {
	if f == nil {
		return 0
	}
	inv := f.CPUCounts(ctx)
	switch f.Type() {
	case Xen:
		if inv.Physical > inv.LinuxVisible && exclusiveDom0CPU {
			return inv.Physical - inv.LinuxVisible
		}
		return inv.Physical
	default:
		return inv.Physical
	}
}
