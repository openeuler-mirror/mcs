package pedestal

import (
	"context"

	"micrun/internal/support/cpuset"
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

func (xen) MaxCPUNum(ctx context.Context) uint32 {
	return MaxCPUNum(ctx)
}

func (xen) HostCPUSeta(ctx context.Context) cpuset.CPUSet {
	return ControlOSCpuset(ctx)
}

func (xen) MemLowThreshold() uint32 {
	return MemLowThreshold()
}

func (xen) MemHighThreshold(ctx context.Context) uint32 {
	return MemHighThreshold(ctx)
}

func (xen) MemoryMB(ctx context.Context) (free, total uint32) {
	return MemoryMB(ctx)
}

// CPUScheduler implementation
func (xen) SetCPUAffinity(ctx context.Context, clientID string, cpus cpuset.CPUSet) error {
	return PinVCPU(ctx, clientID, cpus.String())
}

func (xen) SetCPUWeight(ctx context.Context, clientID string, weight uint32) error {
	return XlSchedCredit2(ctx, clientID, int(weight), 0)
}

func (xen) SetCPUCapacity(ctx context.Context, clientID string, capacity uint32) error {
	return XlSchedCredit2(ctx, clientID, 0, int(capacity))
}

// LifecycleManager implementation
func (xen) Pause(ctx context.Context, clientID string) error {
	return Pause(ctx, clientID)
}

func (xen) Resume(ctx context.Context, clientID string) error {
	return Resume(ctx, clientID)
}

// StateQuerier implementation
func (xen) ClientState(ctx context.Context, clientID string) (string, error) {
	return xenStoreReadDomainState(ctx, clientID)
}

// MemoryManager implementation
func (xen) SetMemory(ctx context.Context, clientID string, memMB uint32) error {
	return XlMemSet(ctx, clientID, int(memMB))
}

func (xen) SetMaxMemory(ctx context.Context, clientID string, memMB uint32) error {
	return XlMemMax(ctx, clientID, int(memMB))
}

// Xen-specific operations (outside interface)
// DomainID returns the Xen domain ID for a client.
func (xen) DomainID(ctx context.Context, clientID string) (int, error) {
	return domainID(ctx, clientID)
}

// SetVCPUCount sets the number of VCPUs for a domain.
func (xen) SetVCPUCount(ctx context.Context, clientID string, count uint32) error {
	return xlVcpuSet(ctx, clientID, int(count))
}
