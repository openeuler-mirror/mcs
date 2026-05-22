package ports

import "context"

// ResourceSnapshot captures the current resource state of a guest client.
type ResourceSnapshot struct {
	CPUCapacity  *uint32
	CPUWeight    *uint32
	ClientCPUSet string
	VCPU         *uint32
	MemoryMaxMB  *uint32
}

// GuestResourceReader reads current guest resource state.
type GuestResourceReader interface {
	ReadResource() *ResourceSnapshot
	CurrentMaxMem() uint32
	MemoryThresholdMB() uint32
}

// GuestResourceUpdater applies resource changes to a guest.
type GuestResourceUpdater interface {
	UpdateCPUCapacity(ctx context.Context, capacity uint32) error
	UpdateCPUWeight(ctx context.Context, weight uint32) error
	UpdateVCPUNum(ctx context.Context, vcpu uint32) (oldCPUs, newCPUs uint32, err error)
	UpdatePCPUConstraints(ctx context.Context, cpuSet string) error
	EnsureMemoryLimit(ctx context.Context, mb uint32) error
	UpdateMemoryThreshold(ctx context.Context, memMiB uint32) error
	UpdateMemory(ctx context.Context, memMiB uint32) error
	RecordMemoryState(current, threshold uint32)
	VCPUPin(ctx context.Context, cpuList []int) error
}

// GuestResourceDiff checks pure local resource deltas.
type GuestResourceDiff interface {
	NeedUpdateMemLimit(target uint32) bool
	NeedUpdateCPUSet(oldSet, newSet string) bool
	NeedUpdateCPUWeight(target uint32) bool
}

// GuestResourceCapacityDiff checks resource deltas that may need host limits.
type GuestResourceCapacityDiff interface {
	NeedUpdateCPUCap(ctx context.Context, target uint32) bool
	NeedUpdateVCPUs(ctx context.Context, target uint32) bool
}

// GuestExecutor composes all guest-side resource management operations.
// Implemented by adapters/guest/libmica.MicaExecutor.
type GuestExecutor interface {
	GuestResourceReader
	GuestResourceUpdater
	GuestResourceDiff
	GuestResourceCapacityDiff
}
