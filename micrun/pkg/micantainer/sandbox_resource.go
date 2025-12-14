package micantainer

import (
	er "micrun/errors"
	"micrun/pkg/cpuset"
)

const (
	maxHostnameLength = 64
)

type SandboxResource struct {
	// total vcpu number of sanbox
	VcpuNum uint32

	// TODO: Pool is not enabled currently
	// Physical cpu pool for container,
	PcpuPool []int

	// Vcpu number of each container
	ContainerVcpus map[string][]int
	// Cpuset of each container
	ContainerCpuSets map[string]cpuset.CPUSet
	// Total requested memory of sandbox workloads
	MemoryPoolBytes uint64
}

// nolint:golint
func NewResMgmt() *SandboxResource {
	return &SandboxResource{
		ContainerVcpus:   make(map[string][]int),
		ContainerCpuSets: make(map[string]cpuset.CPUSet),
	}
}

// closeContainerStdin is the Noop agent process stdin closer. It does nothing.
// nolint
func (n *SandboxResource) closeContainerStdin(c *Container) error {
	if c == nil || c.config == nil {
		return er.EmptyContainerID
	}
	if c.config.IsInfra {
		return nil
	}

	if c.id == "" {
		return er.EmptyContainerID
	}

	return nil
}

func (n *SandboxResource) resizeVCPUs(newNum uint32) (uint32, uint32) {
	old := n.VcpuNum
	n.VcpuNum = newNum
	return old, n.VcpuNum
}
func (n *SandboxResource) resizeMemory(newMemMB uint64) (uint64, uint64) {
	// Convert MiB to bytes
	newMem := newMemMB << 20
	old := n.MemoryPoolBytes
	if old == newMem {
		// No change; avoid unnecessary churn
		return old, old
	}
	n.MemoryPoolBytes = newMem
	return old, newMem
}

// TALK: may be not here
func (n *SandboxResource) getDNS(s *Sandbox) ([]string, error) {
	ret := make([]string, 0)
	return ret, nil
}

func (n *SandboxResource) getTotalMemoryMB() uint64 {
	return n.MemoryPoolBytes >> 20
}

func (n *SandboxResource) ContainerVcpuSet(cid string) ([]int, error) {
	list, ok := n.ContainerVcpus[cid]
	if !ok {
		return []int{}, er.ContainerNotFound
	}

	return list, nil
}

func (n *SandboxResource) setNewPCpuList(cpulist []int) {
	n.PcpuPool = cpulist
}
