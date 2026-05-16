package pedestal

import (
	"context"
	"errors"
	"micrun/internal/support/cpuset"
)

// ErrNotSupported is returned when a pedestal doesn't support an optional operation.
var ErrNotSupported = errors.New("operation not supported by this pedestal type")

// PedestalFacade encapsulates all pedestal operations and provides a unified interface.
// It wraps the underlying Pedestal implementation and caches optional interface implementations.
type PedestalFacade struct {
	impl Pedestal // underlying implementation

	// cached optional interface implementations (lazy loaded)
	cpuScheduler CPUScheduler
	lifecycleMgr LifecycleManager
	stateQuerier StateQuerier
	memoryMgr    MemoryManager
}

// NewPedestalFacade creates a new PedestalFacade wrapping the given Pedestal implementation.
func NewPedestalFacade(impl Pedestal) *PedestalFacade {
	return &PedestalFacade{impl: impl}
}

// Type returns the pedestal type identifier.
func (f *PedestalFacade) Type() PedType {
	return f.impl.Type()
}

// String returns the string representation of the pedestal.
func (f *PedestalFacade) String() string {
	return f.impl.String()
}

// GeneratePedConf generates the pedestal configuration.
func (f *PedestalFacade) GeneratePedConf() string {
	return f.impl.GeneratePedConf()
}

// MaxCPUNum returns the maximum number of CPUs available.
func (f *PedestalFacade) MaxCPUNum(ctx context.Context) uint32 {
	return f.impl.MaxCPUNum(ctx)
}

// MemoryMB returns the amount of free and total memory in MB.
func (f *PedestalFacade) MemoryMB(ctx context.Context) (free, total uint32) {
	return f.impl.MemoryMB(ctx)
}

// MemLowThreshold returns the low memory threshold.
func (f *PedestalFacade) MemLowThreshold() uint32 {
	return f.impl.MemLowThreshold()
}

// MemHighThreshold returns the high memory threshold.
func (f *PedestalFacade) MemHighThreshold(ctx context.Context) uint32 {
	return f.impl.MemHighThreshold(ctx)
}

// HostCPUSeta returns the host CPU set.
func (f *PedestalFacade) HostCPUSeta(ctx context.Context) cpuset.CPUSet {
	return f.impl.HostCPUSeta(ctx)
}

// Optional interface getters (lazy loaded)

func (f *PedestalFacade) getCPUScheduler() CPUScheduler {
	if f.cpuScheduler == nil {
		if cs, ok := f.impl.(CPUScheduler); ok {
			f.cpuScheduler = cs
		}
	}
	return f.cpuScheduler
}

func (f *PedestalFacade) getLifecycleManager() LifecycleManager {
	if f.lifecycleMgr == nil {
		if lm, ok := f.impl.(LifecycleManager); ok {
			f.lifecycleMgr = lm
		}
	}
	return f.lifecycleMgr
}

func (f *PedestalFacade) getStateQuerier() StateQuerier {
	if f.stateQuerier == nil {
		if sq, ok := f.impl.(StateQuerier); ok {
			f.stateQuerier = sq
		}
	}
	return f.stateQuerier
}

func (f *PedestalFacade) getMemoryManager() MemoryManager {
	if f.memoryMgr == nil {
		if mm, ok := f.impl.(MemoryManager); ok {
			f.memoryMgr = mm
		}
	}
	return f.memoryMgr
}

// CPUScheduler methods

// SetCPUAffinity sets CPU affinity for a client.
// Returns ErrNotSupported if the pedestal doesn't support CPU scheduling.
func (f *PedestalFacade) SetCPUAffinity(ctx context.Context, clientID string, cpus cpuset.CPUSet) error {
	if cs := f.getCPUScheduler(); cs != nil {
		return cs.SetCPUAffinity(ctx, clientID, cpus)
	}
	return ErrNotSupported
}

// SetCPUWeight sets the CPU scheduling weight.
// Returns ErrNotSupported if the pedestal doesn't support CPU scheduling.
func (f *PedestalFacade) SetCPUWeight(ctx context.Context, clientID string, weight uint32) error {
	if cs := f.getCPUScheduler(); cs != nil {
		return cs.SetCPUWeight(ctx, clientID, weight)
	}
	return ErrNotSupported
}

// SetCPUCapacity sets the CPU capacity cap.
// Returns ErrNotSupported if the pedestal doesn't support CPU scheduling.
func (f *PedestalFacade) SetCPUCapacity(ctx context.Context, clientID string, capacity uint32) error {
	if cs := f.getCPUScheduler(); cs != nil {
		return cs.SetCPUCapacity(ctx, clientID, capacity)
	}
	return ErrNotSupported
}

// LifecycleManager methods

// Pause pauses a client.
// Returns ErrNotSupported if the pedestal doesn't support lifecycle management.
func (f *PedestalFacade) Pause(ctx context.Context, clientID string) error {
	if lm := f.getLifecycleManager(); lm != nil {
		return lm.Pause(ctx, clientID)
	}
	return ErrNotSupported
}

// Resume resumes a paused client.
// Returns ErrNotSupported if the pedestal doesn't support lifecycle management.
func (f *PedestalFacade) Resume(ctx context.Context, clientID string) error {
	if lm := f.getLifecycleManager(); lm != nil {
		return lm.Resume(ctx, clientID)
	}
	return ErrNotSupported
}

// StateQuerier methods

// ClientState returns the current state of a client.
// Returns ErrNotSupported if the pedestal doesn't support state querying.
func (f *PedestalFacade) ClientState(ctx context.Context, clientID string) (string, error) {
	if sq := f.getStateQuerier(); sq != nil {
		return sq.ClientState(ctx, clientID)
	}
	return "", ErrNotSupported
}

// MemoryManager methods

// SetMemory sets the current memory allocation for a client.
// Returns ErrNotSupported if the pedestal doesn't support dynamic memory management.
func (f *PedestalFacade) SetMemory(ctx context.Context, clientID string, memMB uint32) error {
	if mm := f.getMemoryManager(); mm != nil {
		return mm.SetMemory(ctx, clientID, memMB)
	}
	return ErrNotSupported
}

// SetMaxMemory sets the maximum memory limit for a client.
// Returns ErrNotSupported if the pedestal doesn't support dynamic memory management.
func (f *PedestalFacade) SetMaxMemory(ctx context.Context, clientID string, memMB uint32) error {
	if mm := f.getMemoryManager(); mm != nil {
		return mm.SetMaxMemory(ctx, clientID, memMB)
	}
	return ErrNotSupported
}

// Capabilities returns information about which optional interfaces are supported.
func (f *PedestalFacade) Capabilities() Capabilities {
	return Capabilities{
		CPUScheduling:    f.getCPUScheduler() != nil,
		DynamicMemory:    f.getMemoryManager() != nil,
		LifecycleControl: f.getLifecycleManager() != nil,
		StateQuery:       f.getStateQuerier() != nil,
	}
}

// Capabilities describes which optional interfaces are supported by the pedestal.
type Capabilities struct {
	CPUScheduling    bool
	DynamicMemory    bool
	LifecycleControl bool
	StateQuery       bool
}
