package container

import (
	"micrun/internal/support/cpuset"
)

type sandboxResource struct {
	// Total VCPU number of sandbox.
	VCPUCount uint32

	// VCPU number of each container.
	ContainerVCPUs map[string][]int
	// CPUSet of each container.
	ContainerCPUSet map[string]cpuset.CPUSet
	// Total requested memory of sandbox workloads in bytes.
	MemoryPoolBytes uint64
}

func newResMgmt() *sandboxResource {
	return &sandboxResource{
		ContainerVCPUs:  make(map[string][]int),
		ContainerCPUSet: make(map[string]cpuset.CPUSet),
	}
}

func (n *sandboxResource) ensureMaps() {
	if n.ContainerVCPUs == nil {
		n.ContainerVCPUs = make(map[string][]int)
	}
	if n.ContainerCPUSet == nil {
		n.ContainerCPUSet = make(map[string]cpuset.CPUSet)
	}
}

func (n *sandboxResource) resizeVCPUs(newNum uint32) (uint32, uint32) {
	old := n.VCPUCount
	n.VCPUCount = newNum
	return old, n.VCPUCount
}

func (n *sandboxResource) resizeMemory(newMemMB uint64) (uint64, uint64) {
	// Convert MiB to bytes.
	newMem := newMemMB << 20
	old := n.MemoryPoolBytes
	if old == newMem {
		return old, old
	}
	n.MemoryPoolBytes = newMem
	return old, newMem
}
