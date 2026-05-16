package container

import (
	"context"
	"strings"
	"testing"

	"micrun/internal/ports"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func stubDeps() *Dependencies {
	return &Dependencies{
		StateStoreFactory:        func() ports.StateStore { return nil },
		PlanEssentialRes:         func(spec *specs.Spec) *ResourceChanges { return NewResourceChanges() },
		MaxClientCPUs:            func(_ context.Context, exclusiveDom0CPU bool) int { return 256 },
		HostMemoryMiB:            func(context.Context) (uint32, uint32) { return 4096, 8192 },
		HostMaxPhysCPUs:          func(context.Context) uint32 { return 4 },
		VCPUStats:                func(context.Context) (*VCPUUsageInfo, error) { return nil, nil },
		GuestExecutorFactory:     func(id string) ports.GuestExecutor { return nil },
		TTYDiscoveryRoots:        func() []string { return defaultTTYDiscoveryRoots() },
		DefaultHypervisorControl: func() ports.HypervisorControl { return nil },
		CreateGuest:              func(context.Context, GuestClientConfig) error { return nil },
	}
}

func clearField(d *Dependencies, name string) {
	switch name {
	case "StateStoreFactory":
		d.StateStoreFactory = nil
	case "PlanEssentialRes":
		d.PlanEssentialRes = nil
	case "MaxClientCPUs":
		d.MaxClientCPUs = nil
	case "HostMemoryMiB":
		d.HostMemoryMiB = nil
	case "HostMaxPhysCPUs":
		d.HostMaxPhysCPUs = nil
	case "VCPUStats":
		d.VCPUStats = nil
	case "GuestExecutorFactory":
		d.GuestExecutorFactory = nil
	case "TTYDiscoveryRoots":
		d.TTYDiscoveryRoots = nil
	case "DefaultHypervisorControl":
		d.DefaultHypervisorControl = nil
	case "CreateGuest":
		d.CreateGuest = nil
	}
}

func TestValidateReturnsErrorOnMissingDependency(t *testing.T) {
	fieldNames := []string{
		"StateStoreFactory",
		"PlanEssentialRes",
		"MaxClientCPUs",
		"HostMemoryMiB",
		"HostMaxPhysCPUs",
		"VCPUStats",
		"GuestExecutorFactory",
		"TTYDiscoveryRoots",
		"DefaultHypervisorControl",
		"CreateGuest",
	}

	for _, field := range fieldNames {
		t.Run(field, func(t *testing.T) {
			deps := stubDeps()
			clearField(deps, field)
			err := deps.Validate()
			if err == nil {
				t.Fatalf("expected error when %s is nil", field)
			}
			if !strings.Contains(err.Error(), field) {
				t.Errorf("error %q should mention %q", err.Error(), field)
			}
		})
	}
}

func TestValidateRejectsNilDependencies(t *testing.T) {
	var deps *Dependencies
	err := deps.Validate()
	if err == nil || !strings.Contains(err.Error(), "Dependencies") {
		t.Fatalf("Validate nil dependencies error = %v, want Dependencies", err)
	}
}

func TestValidateSucceedsWithAllFields(t *testing.T) {
	deps := stubDeps()
	if err := deps.Validate(); err != nil {
		t.Fatalf("Validate should not return error with all fields set, got: %v", err)
	}
}

func TestDependenciesAndResourcePolicyDelegateToHooks(t *testing.T) {
	deps := stubDeps()
	t.Run("HostMaxPhysCPUs delegates", func(t *testing.T) {
		if got := deps.HostMaxPhysCPUs(context.Background()); got != 4 {
			t.Errorf("deps.HostMaxPhysCPUs(context.Background()) = %d, want 4", got)
		}
	})
	t.Run("MaxClientCPUs delegates", func(t *testing.T) {
		if got := deps.MaxClientCPUs(context.Background(), false); got != 256 {
			t.Errorf("deps.MaxClientCPUs(context.Background(), false) = %d, want 256", got)
		}
	})
	t.Run("HostMemoryMiB delegates", func(t *testing.T) {
		free, total := deps.HostMemoryMiB(context.Background())
		if free != 4096 || total != 8192 {
			t.Errorf("deps.HostMemoryMiB(context.Background()) = (%d, %d), want (4096, 8192)", free, total)
		}
	})
	t.Run("ResourcePolicyFromDependencies extracts policy", func(t *testing.T) {
		policy := ResourcePolicyFromDependencies(deps)
		if err := policy.Validate(); err != nil {
			t.Fatalf("policy.Validate() error = %v", err)
		}
		if got := policy.MaxClientCPUs(context.Background(), false); got != 256 {
			t.Errorf("policy.MaxClientCPUs(context.Background(), false) = %d, want 256", got)
		}
		if got := policy.HostMaxPhysCPUs(context.Background()); got != 4 {
			t.Errorf("policy.HostMaxPhysCPUs(context.Background()) = %d, want 4", got)
		}
	})
	t.Run("ResourcePolicyFromDependencies handles nil dependencies", func(t *testing.T) {
		policy := ResourcePolicyFromDependencies(nil)
		if err := policy.Validate(); err == nil {
			t.Fatal("policy.Validate() expected error for nil dependencies, got nil")
		}
	})
}

func TestResourcePolicyValidateReturnsErrorOnMissingHook(t *testing.T) {
	fieldNames := []string{
		"PlanEssentialRes",
		"MaxClientCPUs",
		"HostMemoryMiB",
		"HostMaxPhysCPUs",
	}

	for _, field := range fieldNames {
		t.Run(field, func(t *testing.T) {
			deps := stubDeps()
			clearField(deps, field)
			policy := ResourcePolicyFromDependencies(deps)
			err := policy.Validate()
			if err == nil {
				t.Fatalf("expected error when %s is nil", field)
			}
			if !strings.Contains(err.Error(), field) {
				t.Errorf("error %q should mention %q", err.Error(), field)
			}
		})
	}
}

func TestNoopResourcePolicyDoesNotInjectResourceChanges(t *testing.T) {
	policy := NoopResourcePolicy()
	if err := policy.Validate(); err != nil {
		t.Fatalf("NoopResourcePolicy().Validate() error = %v", err)
	}

	changes := policy.PlanEssentialRes(&specs.Spec{})
	if changes == nil {
		t.Fatal("NoopResourcePolicy().PlanEssentialRes returned nil")
	}
	if changes.VCPU != nil || changes.CPUCapacity != nil || changes.CPUWeight != nil || changes.MemoryMaxMB != nil || changes.ClientCPUSet != "" {
		t.Fatalf("NoopResourcePolicy injected resource changes: %+v", changes)
	}
}

func TestResourcePolicyOrDefault(t *testing.T) {
	defaulted := ResourcePolicyOrDefault(nil)
	if err := defaulted.Validate(); err != nil {
		t.Fatalf("ResourcePolicyOrDefault(nil).Validate() error = %v", err)
	}

	explicit := stubDeps()
	policy := ResourcePolicyFromDependencies(explicit)
	resolved := ResourcePolicyOrDefault(&policy)
	if resolved.PlanEssentialRes == nil || resolved.MaxClientCPUs == nil || resolved.HostMemoryMiB == nil || resolved.HostMaxPhysCPUs == nil {
		t.Fatal("ResourcePolicyOrDefault(explicit) dropped hooks")
	}
}
