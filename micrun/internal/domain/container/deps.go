package container

import (
	"context"
	"sync"

	"micrun/internal/ports"
	"micrun/internal/support/timex"
	"micrun/internal/support/validation"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// Dependencies carries the adapter hooks the container domain calls.
// StateStoreFactory and TTYDiscoveryRoots are the only fields mutated after
// construction (every Create RPC re-runs configureRuntimePaths in the shim
// layer) while attach/IO paths read them outside the Create lock, so their
// accesses must go through SetRuntimePaths/StateStore/RPMSGTTYRoots, which
// pathsMu synchronizes. Dependencies must stay pointer-shared, never copied
// (the mutex makes it non-copyable; `go vet` copylocks enforces this).
type Dependencies struct {
	Now                      timex.Clock
	StateStoreFactory        func() ports.StateStore
	PlanEssentialRes         func(spec *specs.Spec) *ResourceChanges
	MaxClientCPUs            func(ctx context.Context, exclusiveDom0CPU bool) int
	HostMemoryMiB            func(ctx context.Context) (freeMB, totalMB uint32)
	HostMaxPhysCPUs          func(ctx context.Context) uint32
	VCPUStats                func(ctx context.Context) (*VCPUUsageInfo, error)
	GuestExecutorFactory     func(id string) ports.GuestExecutor
	TTYDiscoveryRoots        func() []string
	DefaultHypervisorControl func() ports.HypervisorControl
	CreateGuest              func(ctx context.Context, conf GuestClientConfig) error

	pathsMu sync.RWMutex
}

// SetRuntimePaths atomically swaps the two runtime-reconfigurable hooks.
// Callers outside package container must use this instead of assigning the
// fields directly once the Dependencies instance is shared.
func (d *Dependencies) SetRuntimePaths(stateStoreFactory func() ports.StateStore, ttyRoots func() []string) {
	d.pathsMu.Lock()
	defer d.pathsMu.Unlock()
	d.StateStoreFactory = stateStoreFactory
	d.TTYDiscoveryRoots = ttyRoots
}

// StateStore builds a store via the (possibly swapped) factory under the
// swap lock; nil when no factory is configured.
func (d *Dependencies) StateStore() ports.StateStore {
	d.pathsMu.RLock()
	factory := d.StateStoreFactory
	d.pathsMu.RUnlock()
	if factory == nil {
		return nil
	}
	return factory()
}

// RPMSGTTYRoots reads the (possibly swapped) TTY discovery roots under the
// swap lock; nil when no hook is configured.
func (d *Dependencies) RPMSGTTYRoots() []string {
	d.pathsMu.RLock()
	hook := d.TTYDiscoveryRoots
	d.pathsMu.RUnlock()
	if hook == nil {
		return nil
	}
	return hook()
}

func (d *Dependencies) Validate() error {
	if d == nil {
		return validation.RequireAll("container: missing required dependencies", validation.Required("Dependencies", d))
	}
	return validation.RequireAll("container: missing required dependencies",
		validation.Required("StateStoreFactory", d.StateStoreFactory),
		validation.Required("PlanEssentialRes", d.PlanEssentialRes),
		validation.Required("MaxClientCPUs", d.MaxClientCPUs),
		validation.Required("HostMemoryMiB", d.HostMemoryMiB),
		validation.Required("HostMaxPhysCPUs", d.HostMaxPhysCPUs),
		validation.Required("VCPUStats", d.VCPUStats),
		validation.Required("GuestExecutorFactory", d.GuestExecutorFactory),
		validation.Required("TTYDiscoveryRoots", d.TTYDiscoveryRoots),
		validation.Required("DefaultHypervisorControl", d.DefaultHypervisorControl),
		validation.Required("CreateGuest", d.CreateGuest),
	)
}

type ResourcePolicy struct {
	PlanEssentialRes func(spec *specs.Spec) *ResourceChanges
	MaxClientCPUs    func(ctx context.Context, exclusiveDom0CPU bool) int
	HostMemoryMiB    func(ctx context.Context) (freeMB, totalMB uint32)
	HostMaxPhysCPUs  func(ctx context.Context) uint32
}

// NoopResourcePolicy returns a valid policy that preserves OCI resources
// without adding host-derived resource planning results.
func NoopResourcePolicy() ResourcePolicy {
	return ResourcePolicy{
		PlanEssentialRes: func(spec *specs.Spec) *ResourceChanges {
			return &ResourceChanges{}
		},
		MaxClientCPUs: func(context.Context, bool) int {
			return 0
		},
		HostMemoryMiB: func(context.Context) (uint32, uint32) {
			return 0, 0
		},
		HostMaxPhysCPUs: func(context.Context) uint32 {
			return 0
		},
	}
}

// ResourcePolicyOrDefault returns the explicit policy when supplied and a
// valid no-op policy otherwise. It keeps defaulting policy decisions in the
// container domain instead of scattering them through adapter code.
func ResourcePolicyOrDefault(policy *ResourcePolicy) ResourcePolicy {
	if policy == nil {
		return NoopResourcePolicy()
	}
	return *policy
}

// Validate verifies that all hooks required by resource parsing and validation
// are available before the policy is used.
func (p ResourcePolicy) Validate() error {
	return validation.RequireAll("container: missing required resource policy hooks",
		validation.Required("PlanEssentialRes", p.PlanEssentialRes),
		validation.Required("MaxClientCPUs", p.MaxClientCPUs),
		validation.Required("HostMemoryMiB", p.HostMemoryMiB),
		validation.Required("HostMaxPhysCPUs", p.HostMaxPhysCPUs),
	)
}

// ResourcePolicyFromDependencies projects the resource-planning subset of
// Dependencies into the narrower policy consumed by container configuration.
func ResourcePolicyFromDependencies(deps *Dependencies) ResourcePolicy {
	if deps == nil {
		return ResourcePolicy{}
	}
	return ResourcePolicy{
		PlanEssentialRes: deps.PlanEssentialRes,
		MaxClientCPUs:    deps.MaxClientCPUs,
		HostMemoryMiB:    deps.HostMemoryMiB,
		HostMaxPhysCPUs:  deps.HostMaxPhysCPUs,
	}
}
