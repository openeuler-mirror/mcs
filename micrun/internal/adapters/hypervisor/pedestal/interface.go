package pedestal

import (
	"context"

	"micrun/internal/support/cpuset"
)

// Pedestal is the core interface for hypervisor pedestal operations.
// All pedestal implementations must support these operations.
type Pedestal interface {
	// Type returns the pedestal type identifier.
	Type() PedType
	// String returns the string representation of the pedestal.
	String() string

	// Config operations
	GeneratePedConf() string

	// Host resource queries
	MaxCPUNum(ctx context.Context) uint32
	MemoryMB(ctx context.Context) (free, total uint32)
	MemLowThreshold() uint32
	MemHighThreshold(ctx context.Context) uint32
	HostCPUSeta(ctx context.Context) cpuset.CPUSet
}

// CPUScheduler describes CPU scheduling operations (optional).
// Not all pedestals support dynamic CPU scheduling.
type CPUScheduler interface {
	Pedestal
	// SetCPUAffinity sets CPU affinity for a client.
	SetCPUAffinity(ctx context.Context, clientID string, cpus cpuset.CPUSet) error
	// SetCPUWeight sets the CPU scheduling weight.
	SetCPUWeight(ctx context.Context, clientID string, weight uint32) error
	// SetCPUCapacity sets the CPU capacity cap.
	SetCPUCapacity(ctx context.Context, clientID string, capacity uint32) error
}

// LifecycleManager describes client lifecycle operations (optional).
type LifecycleManager interface {
	Pedestal
	Pause(ctx context.Context, clientID string) error
	Resume(ctx context.Context, clientID string) error
}

// StateQuerier describes client state query operations (optional).
type StateQuerier interface {
	Pedestal
	// ClientState returns the current state of a client.
	ClientState(ctx context.Context, clientID string) (string, error)
}

// MemoryManager describes dynamic memory operations (optional).
type MemoryManager interface {
	Pedestal
	SetMemory(ctx context.Context, clientID string, memMB uint32) error
	SetMaxMemory(ctx context.Context, clientID string, memMB uint32) error
}
