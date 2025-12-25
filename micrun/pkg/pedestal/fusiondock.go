package pedestal

import (
	"micrun/pkg/cpuset"
)

// fusiondock is the FusionDock hypervisor pedestal implementation.
// Currently a stub implementation.
type fusiondock struct {
	DefaultPedestal
}

func (fusiondock) Type() PedType { return FusionDock }

func (fusiondock) String() string { return "fusiondock" }

func (fusiondock) GeneratePedConf() string {
	return ""
}

func (fusiondock) MaxCPUNum() uint32 {
	return DefaultPedestal{}.MaxCPUNum()
}

func (fusiondock) HostCPUSeta() cpuset.CPUSet {
	return cpuset.NewCPUSet()
}

func (fusiondock) MemLowThreshold() uint32 {
	return DefaultPedestal{}.MemLowThreshold()
}

func (fusiondock) MemHighThreshold() uint32 {
	return DefaultPedestal{}.MemHighThreshold()
}

func (fusiondock) MemoryMB() (free, total uint32) {
	return DefaultPedestal{}.MemoryMB()
}
