package container

import (
	"context"

	"micrun/internal/ports"
	"micrun/internal/support/timex"
	"micrun/internal/support/validation"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

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
