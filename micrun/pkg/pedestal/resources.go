package pedestal

import (
	defs "micrun/definitions"
	"runtime"
	"sync"
	"sync/atomic"
)

// EssentialResource contains essential resource specifications for a client.
type EssentialResource struct {
	// mica conf: CPUCapacity.
	CpuCpacity *uint32
	// mica conf: CPUWeight.
	CPUWeight *uint32
	// mica conf: CPU, representing CpuAffinity []i32.
	ClientCpuSet string
	// mica conf: vcpu.
	Vcpu *uint32
	// mica conf: Memory. the max memory client does can use, not memory threshold
	MemoryMaxMB *uint32
	// The reserved memory size for rtos
	MemoryMinMB uint32
	// Virtual network interface.
	VIF []string
}

// default value of essential resource struct, not about runtime config.
const defaultVcpus = 1

func InitResource() *EssentialResource {
	res := EssentialResource{}
	vcpu := uint32(defaultVcpus)
	capacity := uint32(0)
	maxmem := uint32(defs.DefaultMinMemMB)
	cpuWeight := uint32(0)
	res.Vcpu = &vcpu
	res.CpuCpacity = &capacity
	res.CPUWeight = &cpuWeight
	res.MemoryMaxMB = &maxmem
	return &res
}

// HostCPUInventory captures the static CPU counts visible to micran.
type HostCPUInventory struct {
	// Physical represents the physical CPUs the pedestal can schedule.
	Physical uint32
	// LinuxVisible is the CPU count reported to the Linux side (Dom0 or baremetal kernel).
	LinuxVisible uint32
}

// HostMemoryInventory contains the host memory capacity in MiB.
type HostMemoryInventory struct {
	FreeMB  uint32
	TotalMB uint32
}

var (
	hostCPUOnce        sync.Once
	hostCPUInventory   HostCPUInventory
	baremetalReservedC atomic.Uint32
	exclusiveDom0Flag  atomic.Bool
)

// HostCPUCounts returns cached host CPU counts (physical vs. Linux visible).
func HostCPUCounts() HostCPUInventory {
	hostCPUOnce.Do(func() {
		physical := MaxCPUNum()
		linux := min(uint32(runtime.NumCPU()), physical)
		hostCPUInventory = HostCPUInventory{
			Physical:     physical,
			LinuxVisible: linux,
		}
	})
	return hostCPUInventory
}

// ClientCPUCapacity returns the number of CPUs micran can hand to RTOS clients.
// Baremetal pedestals keep Linux visibility of all CPUs
// (docs/resource-management-comparison.md:7-12), so we subtract the reserved set
// recorded via SetBaremetalReservedCPUs instead of re-reading /proc/cpuinfo.
func ClientCPUCapacity() uint32 {
	inv := HostCPUCounts()
	switch GetHostPed() {
	case Xen:
		// TODO: linux cpu can be shared with clients by default, for xen case.
		if inv.Physical > inv.LinuxVisible && ExclusiveDom0CPUEnabled() {
			return inv.Physical - inv.LinuxVisible
		}
		return inv.Physical
	default:
		reserved := baremetalReservedC.Load()
		if reserved >= inv.Physical {
			return 0
		}
		return inv.Physical - reserved
	}
}

// HostMemoryMiB returns the current host memory capacity snapshot in MiB.
func HostMemoryMiB() HostMemoryInventory {
	free, total := MemoryMB()
	return HostMemoryInventory{
		FreeMB:  free,
		TotalMB: total,
	}
}

// SetBaremetalReservedCPUs records how many CPUs must remain with Linux when
// running on baremetal pedestals. Linux always exposes the full CPU set, so
// we persist the reservation in shared state instead of recalculating it from
// /proc/cpuinfo on every request.
func SetBaremetalReservedCPUs(count uint32) {
	inv := HostCPUCounts()
	if count > inv.Physical {
		count = inv.Physical
	}
	baremetalReservedC.Store(count)
}

// BaremetalReservedCPUs returns the currently recorded baremetal reservation.
func BaremetalReservedCPUs() uint32 {
	return baremetalReservedC.Load()
}

// EnableDom0CPUExclusive toggles whether Dom0 CPUs stay exclusive (Xen only).
func EnableDom0CPUExclusive(enabled bool) {
	exclusiveDom0Flag.Store(enabled)
}

// ExclusiveDom0CPUEnabled reports whether Dom0 CPUs are reserved exclusively.
func ExclusiveDom0CPUEnabled() bool {
	return exclusiveDom0Flag.Load()
}
