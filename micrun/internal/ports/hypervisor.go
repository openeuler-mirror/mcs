package ports

import "context"

type HypervisorType string

const (
	HypervisorXen         HypervisorType = "xen"
	HypervisorBaremetal   HypervisorType = "baremetal"
	HypervisorUnsupported HypervisorType = "unsupported"
)

// HypervisorControl abstracts host/hypervisor operations that may be needed by
// runtime orchestration or backend adapters.
type HypervisorControl interface {
	// Type returns the hypervisor type.
	Type() HypervisorType

	// MaxCPUNum returns the maximum number of CPUs available.
	MaxCPUNum(ctx context.Context) uint32

	// MemoryMB returns the amount of free and total memory in MB.
	MemoryMB(ctx context.Context) (free uint32, total uint32)

	// DomainState returns the current state of a domain.
	DomainState(ctx context.Context, id string) (string, error)

	// Pause pauses a domain.
	Pause(ctx context.Context, id string) error

	// Resume resumes a paused domain.
	Resume(ctx context.Context, id string) error

	// SetVCPUCount sets the VCPU count for a domain.
	SetVCPUCount(ctx context.Context, id string, count uint32) error

	// SetMemory sets the current memory allocation for a domain.
	SetMemory(ctx context.Context, id string, memMB uint32) error

	// SetMaxMemory sets the maximum memory limit for a domain.
	SetMaxMemory(ctx context.Context, id string, memMB uint32) error

	// SetCPUWeight sets the CPU scheduling weight for a domain.
	SetCPUWeight(ctx context.Context, id string, weight uint32) error

	// SetCPUCapacity sets the CPU capacity cap for a domain.
	SetCPUCapacity(ctx context.Context, id string, capacity uint32) error
}
