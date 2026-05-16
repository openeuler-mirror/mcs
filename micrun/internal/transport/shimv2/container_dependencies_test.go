package shim

import (
	"testing"

	pedestal "micrun/internal/adapters/hypervisor/pedestal"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func TestBuildContainerDependenciesReturnsCompleteSet(t *testing.T) {
	bindings := testRuntimeEnvironment()
	deps := buildContainerDependencies(bindings)
	if deps == nil {
		t.Fatal("buildContainerDependencies returned nil")
	}
	if err := deps.Validate(); err != nil {
		t.Fatalf("buildContainerDependencies returned invalid dependencies: %v", err)
	}
}

func TestMapVCPUUsageInfoPreservesPerDomainEntries(t *testing.T) {
	info := mapVCPUUsageInfo(&pedestal.XlVcpuInfo{
		DomainVCPUMap: map[string][]pedestal.VCPUEntry{
			"demo": {
				{TimeSeconds: 1.5},
				{TimeSeconds: 2.5},
			},
		},
	})

	if info == nil {
		t.Fatal("mapVCPUUsageInfo returned nil")
	}
	entries := info.DomainVCPUMap["demo"]
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].TimeSeconds != 1.5 || entries[1].TimeSeconds != 2.5 {
		t.Fatalf("unexpected mapped entries: %+v", entries)
	}
}

func TestMapEssentialResourcesCarriesMemoryFloor(t *testing.T) {
	resource := mapEssentialResources(&specs.Spec{}, func(*specs.Spec) *pedestal.EssentialResource {
		return pedestal.InitResource()
	})
	if resource == nil {
		t.Fatal("mapEssentialResources returned nil")
	}
	if resource.MemoryMinMB != 0 {
		t.Fatalf("expected zero memory floor for empty spec, got %d", resource.MemoryMinMB)
	}
}

func TestMapEssentialResourcesFallsBackWhenPlannerReturnsNil(t *testing.T) {
	resource := mapEssentialResources(&specs.Spec{}, func(*specs.Spec) *pedestal.EssentialResource {
		return nil
	})
	if resource == nil {
		t.Fatal("mapEssentialResources returned nil")
	}
	if resource.VCPU == nil || *resource.VCPU != 1 {
		t.Fatalf("expected default VCPU of 1, got %v", resource.VCPU)
	}
}
