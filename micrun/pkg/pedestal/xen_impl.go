package pedestal

import (
	"micrun/pkg/cpuset"
)

// xen is the Xen hypervisor pedestal implementation.
// Xen implements all optional interfaces.
type xen struct {
	DefaultPedestal
}

func (xen) Type() PedType { return Xen }

func (xen) String() string { return "xen" }

func (xen) GeneratePedConf() string {
	return XenDefaultPedConf()
}

func (xen) MaxCPUNum() uint32 {
	return MaxCPUNum()
}

func (xen) HostCPUSeta() cpuset.CPUSet {
	return ControlOSCpuset()
}

func (xen) MemLowThreshold() uint32 {
	return MemLowThreshold()
}

func (xen) MemHighThreshold() uint32 {
	return MemHighThreshold()
}

func (xen) MemoryMB() (free, total uint32) {
	return MemoryMB()
}

// CPUScheduler implementation
func (xen) SetCPUAffinity(clientID string, cpus cpuset.CPUSet) error {
	return PinVCPU(clientID, cpus.String())
}

func (xen) SetCPUWeight(clientID string, weight uint32) error {
	return XlSchedCredit2(clientID, int(weight), 0)
}

func (xen) SetCPUCapacity(clientID string, capacity uint32) error {
	return XlSchedCredit2(clientID, 0, int(capacity))
}

// LifecycleManager implementation
func (xen) Pause(clientID string) error {
	return Pause(clientID)
}

func (xen) Resume(clientID string) error {
	return Resume(clientID)
}

// StateQuerier implementation
func (xen) ClientState(clientID string) (string, error) {
	return xenStoreReadDomainState(clientID)
}

// MemoryManager implementation
func (xen) SetMemory(clientID string, memMB uint32) error {
	return XlMemSet(clientID, int(memMB))
}

func (xen) SetMaxMemory(clientID string, memMB uint32) error {
	return XlMemMax(clientID, int(memMB))
}

// Xen-specific operations (outside interface)
// DomainID returns the Xen domain ID for a client.
func (xen) DomainID(clientID string) (int, error) {
	return domainID(clientID)
}

// SetVCPUCount sets the number of VCPUs for a domain.
func (xen) SetVCPUCount(clientID string, count uint32) error {
	return xlVcpuSet(clientID, int(count))
}
