package pedestal

import (
	"micrun/pkg/cpuset"
)

// acrn is the ACRN hypervisor pedestal implementation.
// Currently a stub implementation.
type acrn struct {
	DefaultPedestal
}

func (acrn) Type() PedType { return ACRN }

func (acrn) String() string { return "acrn" }

func (acrn) GeneratePedConf() string {
	return ""
}

func (acrn) MaxCPUNum() uint32 {
	return DefaultPedestal{}.MaxCPUNum()
}

func (acrn) HostCPUSeta() cpuset.CPUSet {
	return cpuset.NewCPUSet()
}

func (acrn) MemLowThreshold() uint32 {
	return DefaultPedestal{}.MemLowThreshold()
}

func (acrn) MemHighThreshold() uint32 {
	return DefaultPedestal{}.MemHighThreshold()
}

func (acrn) MemoryMB() (free, total uint32) {
	return DefaultPedestal{}.MemoryMB()
}
