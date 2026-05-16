package container

import (
	"context"
	"errors"
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func testResourcePolicy() ResourcePolicy {
	return ResourcePolicy{
		PlanEssentialRes: func(spec *specs.Spec) *ResourceChanges {
			res := NewResourceChanges()
			if spec.Linux == nil || spec.Linux.Resources == nil || spec.Linux.Resources.CPU == nil {
				return res
			}
			cpu := spec.Linux.Resources.CPU
			if cpu.Quota != nil && *cpu.Quota > 0 && cpu.Period != nil && *cpu.Period > 0 {
				rawCapacity := uint32((*cpu.Quota * 100) / int64(*cpu.Period))
				if rawCapacity > 0 {
					vcpuNum := (rawCapacity + 99) / 100
					*res.VCPU = vcpuNum
					*res.CPUCapacity = rawCapacity
				}
			}
			return res
		},
		MaxClientCPUs:   func(_ context.Context, exclusiveDom0CPU bool) int { return 256 },
		HostMemoryMiB:   func(context.Context) (uint32, uint32) { return 4096, 8192 },
		HostMaxPhysCPUs: func(context.Context) uint32 { return 4 },
	}
}

func TestParseOCIResourcesDerivesVCPUCountFromQuota(t *testing.T) {
	quota := int64(150000)
	period := uint64(100000)
	cfg := &ContainerConfig{}
	spec := &specs.Spec{
		Linux: &specs.Linux{
			Resources: &specs.LinuxResources{
				CPU: &specs.LinuxCPU{
					Quota:  &quota,
					Period: &period,
				},
			},
		},
	}

	if err := cfg.ParseOCIResourcesWithPolicy(context.Background(), spec, testResourcePolicy()); err != nil {
		t.Fatalf("ParseOCIResourcesWithPolicy() error = %v", err)
	}

	if cfg.VCPUNum != 2 {
		t.Fatalf("VCPUNum = %d, want 2", cfg.VCPUNum)
	}
	if cfg.CPUCapacity() != 150 {
		t.Fatalf("CPUCapacity = %d, want 150", cfg.CPUCapacity())
	}
}

func TestCPUCapacityFromQuotaPeriodSaturatesOverflow(t *testing.T) {
	if got := cpuCapacityFromQuotaPeriod(150000, 100000); got != 150 {
		t.Fatalf("cpuCapacityFromQuotaPeriod normal = %d, want 150", got)
	}
	if got := cpuCapacityFromQuotaPeriod(1<<62, 1); got != maxUint32 {
		t.Fatalf("cpuCapacityFromQuotaPeriod huge = %d, want %d", got, maxUint32)
	}
	if got := cpuCapacityFromQuotaPeriod(-1, 100000); got != 0 {
		t.Fatalf("cpuCapacityFromQuotaPeriod negative = %d, want 0", got)
	}
}

func TestParseOCIResourcesRequiresValidPolicy(t *testing.T) {
	cfg := &ContainerConfig{}
	spec := &specs.Spec{}

	if err := cfg.ParseOCIResourcesWithPolicy(context.Background(), spec, ResourcePolicy{}); err == nil {
		t.Fatal("ParseOCIResourcesWithPolicy() expected policy validation error, got nil")
	}
}

func TestParseOCIResourcesRequiresConfig(t *testing.T) {
	var cfg *ContainerConfig
	err := cfg.ParseOCIResourcesWithPolicy(context.Background(), &specs.Spec{}, testResourcePolicy())
	if err == nil {
		t.Fatal("ParseOCIResourcesWithPolicy() expected config error, got nil")
	}
}

func TestParseOCIResourcesSkipsPolicyForInfraContainer(t *testing.T) {
	cfg := &ContainerConfig{IsInfra: true}
	if err := cfg.ParseOCIResourcesWithPolicy(context.Background(), &specs.Spec{}, ResourcePolicy{}); err != nil {
		t.Fatalf("ParseOCIResourcesWithPolicy() infra error = %v, want nil", err)
	}
}

func TestParseOCIResourcesNormalizesNilSpec(t *testing.T) {
	cfg := &ContainerConfig{}
	policy := testResourcePolicy()
	policy.PlanEssentialRes = func(spec *specs.Spec) *ResourceChanges {
		if spec == nil {
			t.Fatal("PlanEssentialRes received nil spec")
		}
		return NewResourceChanges()
	}

	if err := cfg.ParseOCIResourcesWithPolicy(context.Background(), nil, policy); err != nil {
		t.Fatalf("ParseOCIResourcesWithPolicy(nil) error = %v", err)
	}
}

func TestParseOCIResourcesPassesContextToHostCPUQuery(t *testing.T) {
	cfg := &ContainerConfig{}
	spec := &specs.Spec{
		Linux: &specs.Linux{
			Resources: &specs.LinuxResources{
				CPU: &specs.LinuxCPU{Cpus: "0,1"},
			},
		},
	}
	policy := testResourcePolicy()
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	var gotCtx context.Context
	policy.HostMaxPhysCPUs = func(ctx context.Context) uint32 {
		gotCtx = ctx
		return 4
	}

	if err := cfg.ParseOCIResourcesWithPolicy(ctx, spec, policy); err != nil {
		t.Fatalf("ParseOCIResourcesWithPolicy() error = %v", err)
	}
	if gotCtx != ctx {
		t.Fatal("HostMaxPhysCPUs did not receive caller context")
	}
}

func TestParseOCIResourcesHonorsCanceledContextBeforeHostCPUQuery(t *testing.T) {
	cfg := &ContainerConfig{}
	spec := &specs.Spec{
		Linux: &specs.Linux{
			Resources: &specs.LinuxResources{
				CPU: &specs.LinuxCPU{Cpus: "0"},
			},
		},
	}
	policy := testResourcePolicy()
	policy.HostMaxPhysCPUs = func(context.Context) uint32 {
		t.Fatal("HostMaxPhysCPUs should not run after context cancellation")
		return 0
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := cfg.ParseOCIResourcesWithPolicy(ctx, spec, policy)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ParseOCIResourcesWithPolicy canceled error = %v, want context.Canceled", err)
	}
}

func TestApplyCPUResourcesInitializesResources(t *testing.T) {
	shares := uint64(2048)
	cfg := &ContainerConfig{}
	spec := &specs.Spec{
		Linux: &specs.Linux{
			Resources: &specs.LinuxResources{
				CPU: &specs.LinuxCPU{Shares: &shares},
			},
		},
	}

	if err := cfg.applyCPUResources(context.Background(), spec, NewResourceChanges(), testResourcePolicy()); err != nil {
		t.Fatalf("applyCPUResources() error = %v", err)
	}
	if cfg.Resources == nil || cfg.Resources.CPU == nil {
		t.Fatal("applyCPUResources() did not initialize CPU resources")
	}
}

func TestParseOCIResourcesAppliesEssentialCPUWithoutOCICPU(t *testing.T) {
	vcpu := uint32(3)
	policy := testResourcePolicy()
	policy.PlanEssentialRes = func(spec *specs.Spec) *ResourceChanges {
		return &ResourceChanges{
			VCPU:         &vcpu,
			ClientCPUSet: "0,1,5",
		}
	}

	cfg := &ContainerConfig{}
	spec := &specs.Spec{Linux: &specs.Linux{Resources: &specs.LinuxResources{}}}

	if err := cfg.ParseOCIResourcesWithPolicy(context.Background(), spec, policy); err != nil {
		t.Fatalf("ParseOCIResourcesWithPolicy() error = %v", err)
	}

	if cfg.Resources == nil || cfg.Resources.CPU == nil {
		t.Fatal("ParseOCIResourcesWithPolicy() did not create CPU resources")
	}
	if got := cfg.CPUSet(); got != "0-1" {
		t.Fatalf("CPUSet() = %q, want %q", got, "0-1")
	}
	if cfg.VCPUNum != 2 {
		t.Fatalf("VCPUNum = %d, want filtered cpuset size 2", cfg.VCPUNum)
	}
}

func TestApplyCPUResourcesRequiresConfig(t *testing.T) {
	var cfg *ContainerConfig
	if err := cfg.applyCPUResources(context.Background(), &specs.Spec{}, NewResourceChanges(), testResourcePolicy()); err == nil {
		t.Fatal("applyCPUResources() expected config error, got nil")
	}
}

func TestApplyMemoryResourcesInitializesResources(t *testing.T) {
	limit := int64(256 << 20)
	cfg := &ContainerConfig{}
	spec := &specs.Spec{
		Linux: &specs.Linux{
			Resources: &specs.LinuxResources{
				Memory: &specs.LinuxMemory{Limit: &limit},
			},
		},
	}

	cfg.applyMemoryResources(spec)

	if cfg.Resources == nil || cfg.Resources.Memory == nil || cfg.Resources.Memory.Limit == nil {
		t.Fatal("applyMemoryResources() did not initialize memory resources")
	}
}

func TestApplyMemoryResourcesAllowsNilConfig(t *testing.T) {
	var cfg *ContainerConfig
	cfg.applyMemoryResources(&specs.Spec{})
}

func TestBytesToMiBSaturatesAtUint32Max(t *testing.T) {
	huge := (int64(maxUint32) + 1) * miB

	if got := bytesToMiB(&huge); got != maxUint32 {
		t.Fatalf("bytesToMiB(huge) = %d, want %d", got, maxUint32)
	}
}

func TestLogCPUResourceSummaryAllowsNilConfig(t *testing.T) {
	logCPUResourceSummary(nil)
}

func TestValidateResourceLimitsAcceptsCapacityWithinHostCPUCount(t *testing.T) {
	quota := int64(200000)
	period := uint64(100000)
	cfg := &ContainerConfig{
		Resources: &specs.LinuxResources{
			CPU: &specs.LinuxCPU{
				Quota:  &quota,
				Period: &period,
			},
		},
	}

	if err := ValidateResourceLimitsWithPolicy(context.Background(), cfg, testResourcePolicy()); err != nil {
		t.Fatalf("ValidateResourceLimitsWithPolicy() error = %v, want nil", err)
	}
}

func TestValidateResourceLimitsRequiresValidPolicy(t *testing.T) {
	cfg := &ContainerConfig{}
	if err := ValidateResourceLimitsWithPolicy(context.Background(), cfg, ResourcePolicy{}); err == nil {
		t.Fatal("ValidateResourceLimitsWithPolicy() expected policy validation error, got nil")
	}
}

func TestValidateResourceLimitsRequiresConfig(t *testing.T) {
	if err := ValidateResourceLimitsWithPolicy(context.Background(), nil, testResourcePolicy()); err == nil {
		t.Fatal("ValidateResourceLimitsWithPolicy() expected config error, got nil")
	}
}

func TestValidateResourceLimitsSkipsPolicyForInfraContainer(t *testing.T) {
	cfg := &ContainerConfig{IsInfra: true}
	if err := ValidateResourceLimitsWithPolicy(context.Background(), cfg, ResourcePolicy{}); err != nil {
		t.Fatalf("ValidateResourceLimitsWithPolicy() infra error = %v, want nil", err)
	}
}

func TestValidateResourceLimitsWithPolicyUsesExplicitPolicy(t *testing.T) {
	cfg := &ContainerConfig{
		Resources: &specs.LinuxResources{
			Memory: &specs.LinuxMemory{},
		},
	}
	cfg.Resources.Memory.Limit = miBToBytes(512)

	policy := ResourcePolicy{
		PlanEssentialRes: func(spec *specs.Spec) *ResourceChanges { return NewResourceChanges() },
		MaxClientCPUs:    func(_ context.Context, exclusiveDom0CPU bool) int { return 1 },
		HostMemoryMiB:    func(context.Context) (uint32, uint32) { return 0, 256 },
		HostMaxPhysCPUs:  func(context.Context) uint32 { return 1 },
	}

	if err := ValidateResourceLimitsWithPolicy(context.Background(), cfg, policy); err == nil {
		t.Fatal("ValidateResourceLimitsWithPolicy() expected explicit policy rejection, got nil")
	}
}

func TestValidateResourceLimitsRejectsHugeMemoryAfterSaturation(t *testing.T) {
	huge := (int64(maxUint32) + 1) * miB
	cfg := &ContainerConfig{
		Resources: &specs.LinuxResources{
			Memory: &specs.LinuxMemory{Limit: &huge},
		},
	}

	if err := ValidateResourceLimitsWithPolicy(context.Background(), cfg, testResourcePolicy()); err == nil {
		t.Fatal("ValidateResourceLimitsWithPolicy() expected huge memory rejection, got nil")
	}
}

func TestValidateResourceLimitsPassesContextToHostMemoryQuery(t *testing.T) {
	limit := int64(128 << 20)
	cfg := &ContainerConfig{
		Resources: &specs.LinuxResources{
			Memory: &specs.LinuxMemory{Limit: &limit},
		},
	}
	policy := testResourcePolicy()
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	var gotCtx context.Context
	policy.HostMemoryMiB = func(ctx context.Context) (uint32, uint32) {
		gotCtx = ctx
		return 4096, 8192
	}

	if err := ValidateResourceLimitsWithPolicy(ctx, cfg, policy); err != nil {
		t.Fatalf("ValidateResourceLimitsWithPolicy() error = %v", err)
	}
	if gotCtx != ctx {
		t.Fatal("HostMemoryMiB did not receive caller context")
	}
}

func TestValidateResourceLimitsPassesContextToMaxClientCPUQuery(t *testing.T) {
	quota := int64(100000)
	period := uint64(100000)
	cfg := &ContainerConfig{
		Resources: &specs.LinuxResources{
			CPU: &specs.LinuxCPU{
				Quota:  &quota,
				Period: &period,
			},
		},
	}
	policy := testResourcePolicy()
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	var gotCtx context.Context
	policy.MaxClientCPUs = func(ctx context.Context, exclusiveDom0CPU bool) int {
		gotCtx = ctx
		return 4
	}

	if err := ValidateResourceLimitsWithPolicy(ctx, cfg, policy); err != nil {
		t.Fatalf("ValidateResourceLimitsWithPolicy() error = %v", err)
	}
	if gotCtx != ctx {
		t.Fatal("MaxClientCPUs did not receive caller context")
	}
}

func TestValidateResourceLimitsHonorsCanceledContextBeforeHostQueries(t *testing.T) {
	quota := int64(100000)
	period := uint64(100000)
	cfg := &ContainerConfig{
		Resources: &specs.LinuxResources{
			CPU: &specs.LinuxCPU{
				Quota:  &quota,
				Period: &period,
			},
		},
	}
	policy := testResourcePolicy()
	policy.MaxClientCPUs = func(context.Context, bool) int {
		t.Fatal("MaxClientCPUs should not run after context cancellation")
		return 0
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ValidateResourceLimitsWithPolicy(ctx, cfg, policy)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateResourceLimitsWithPolicy canceled error = %v, want context.Canceled", err)
	}
}

func TestParseOCIResourcesFiltersOutOfRangeCPUSet(t *testing.T) {
	policy := ResourcePolicy{
		PlanEssentialRes: func(spec *specs.Spec) *ResourceChanges {
			return &ResourceChanges{ClientCPUSet: "0,2,9"}
		},
		MaxClientCPUs:   func(_ context.Context, exclusiveDom0CPU bool) int { return 256 },
		HostMemoryMiB:   func(context.Context) (uint32, uint32) { return 4096, 8192 },
		HostMaxPhysCPUs: func(context.Context) uint32 { return 4 },
	}

	cfg := &ContainerConfig{}
	spec := &specs.Spec{
		Linux: &specs.Linux{
			Resources: &specs.LinuxResources{
				CPU: &specs.LinuxCPU{Cpus: "0,2,9"},
			},
		},
	}

	if err := cfg.ParseOCIResourcesWithPolicy(context.Background(), spec, policy); err != nil {
		t.Fatalf("ParseOCIResourcesWithPolicy() error = %v", err)
	}

	if got := cfg.CPUSet(); got != "0,2" {
		t.Fatalf("CPUSet() = %q, want %q", got, "0,2")
	}
	if cfg.VCPUNum != 2 {
		t.Fatalf("VCPUNum = %d, want 2", cfg.VCPUNum)
	}
}

func TestParseOCIResourcesCanonicalizesValidCPUSet(t *testing.T) {
	cfg := &ContainerConfig{}
	spec := &specs.Spec{
		Linux: &specs.Linux{
			Resources: &specs.LinuxResources{
				CPU: &specs.LinuxCPU{Cpus: "2,1,1"},
			},
		},
	}

	if err := cfg.ParseOCIResourcesWithPolicy(context.Background(), spec, testResourcePolicy()); err != nil {
		t.Fatalf("ParseOCIResourcesWithPolicy() error = %v", err)
	}

	if got := cfg.CPUSet(); got != "1-2" {
		t.Fatalf("CPUSet() = %q, want %q", got, "1-2")
	}
	if cfg.VCPUNum != 2 {
		t.Fatalf("VCPUNum = %d, want 2", cfg.VCPUNum)
	}
}

func TestParseOCIResourcesDerivesVCPUCountFromCanonicalCPUSet(t *testing.T) {
	cfg := &ContainerConfig{}
	spec := &specs.Spec{
		Linux: &specs.Linux{
			Resources: &specs.LinuxResources{
				CPU: &specs.LinuxCPU{Cpus: "0-1"},
			},
		},
	}

	if err := cfg.ParseOCIResourcesWithPolicy(context.Background(), spec, testResourcePolicy()); err != nil {
		t.Fatalf("ParseOCIResourcesWithPolicy() error = %v", err)
	}

	if got := cfg.CPUSet(); got != "0-1" {
		t.Fatalf("CPUSet() = %q, want %q", got, "0-1")
	}
	if cfg.VCPUNum != 2 {
		t.Fatalf("VCPUNum = %d, want 2", cfg.VCPUNum)
	}
}

func TestParseOCIResourcesClearsFullyOutOfRangeCPUSet(t *testing.T) {
	policy := ResourcePolicy{
		PlanEssentialRes: func(spec *specs.Spec) *ResourceChanges {
			return &ResourceChanges{ClientCPUSet: "8,9"}
		},
		MaxClientCPUs:   func(_ context.Context, exclusiveDom0CPU bool) int { return 256 },
		HostMemoryMiB:   func(context.Context) (uint32, uint32) { return 4096, 8192 },
		HostMaxPhysCPUs: func(context.Context) uint32 { return 4 },
	}

	quota := int64(250000)
	period := uint64(100000)
	cfg := &ContainerConfig{}
	spec := &specs.Spec{
		Linux: &specs.Linux{
			Resources: &specs.LinuxResources{
				CPU: &specs.LinuxCPU{
					Cpus:   "8,9",
					Quota:  &quota,
					Period: &period,
				},
			},
		},
	}

	if err := cfg.ParseOCIResourcesWithPolicy(context.Background(), spec, policy); err != nil {
		t.Fatalf("ParseOCIResourcesWithPolicy() error = %v", err)
	}

	if got := cfg.CPUSet(); got != "" {
		t.Fatalf("CPUSet() = %q, want empty", got)
	}
	if cfg.VCPUNum != 3 {
		t.Fatalf("VCPUNum = %d, want 3", cfg.VCPUNum)
	}
}

func TestParseOCIResourcesKeepsExplicitVCPUWhenCPUSetFullyOutOfRange(t *testing.T) {
	policy := ResourcePolicy{
		PlanEssentialRes: func(spec *specs.Spec) *ResourceChanges {
			vcpu := uint32(4)
			return &ResourceChanges{
				VCPU:         &vcpu,
				ClientCPUSet: "8,9",
			}
		},
		MaxClientCPUs:   func(_ context.Context, exclusiveDom0CPU bool) int { return 256 },
		HostMemoryMiB:   func(context.Context) (uint32, uint32) { return 4096, 8192 },
		HostMaxPhysCPUs: func(context.Context) uint32 { return 4 },
	}
	cfg := &ContainerConfig{}
	spec := &specs.Spec{
		Linux: &specs.Linux{
			Resources: &specs.LinuxResources{
				CPU: &specs.LinuxCPU{Cpus: "8,9"},
			},
		},
	}

	if err := cfg.ParseOCIResourcesWithPolicy(context.Background(), spec, policy); err != nil {
		t.Fatalf("ParseOCIResourcesWithPolicy() error = %v", err)
	}

	if got := cfg.CPUSet(); got != "" {
		t.Fatalf("CPUSet() = %q, want empty", got)
	}
	if cfg.VCPUNum != 4 {
		t.Fatalf("VCPUNum = %d, want explicit 4", cfg.VCPUNum)
	}
}

func TestParseCPUSetMaskRejectsEmptyAndNegativeEntries(t *testing.T) {
	tests := []string{"0,,2", "-1", "0,-2"}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := parseCPUSetMask(input); err == nil {
				t.Fatalf("parseCPUSetMask(%q) expected error, got nil", input)
			}
		})
	}
}
