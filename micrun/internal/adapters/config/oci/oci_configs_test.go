package oci

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	cntr "micrun/internal/domain/container"
	ann "micrun/internal/support/annotations"
	defs "micrun/internal/support/definitions"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func TestApplyContainerRuntimeDefaultsMaxVcpu(t *testing.T) {
	limit := int64(128 * 1024 * 1024)
	reservation := int64(16 * 1024 * 1024)
	cfg := &cntr.ContainerConfig{
		Resources: &specs.LinuxResources{
			Memory: &specs.LinuxMemory{
				Limit:       &limit,
				Reservation: &reservation,
			},
		},
	}
	rc := &RuntimeConfig{
		RuntimeResourceConfig: RuntimeResourceConfig{
			MaxContainerVCPUs: 6,
			MinContainerMemMB: defs.DefaultContainerMinMemMB,
			MaxContainerMemMB: 512,
		},
	}
	annotations := map[string]string{
		ann.ContainerMaxVcpuNum: "4",
	}

	if err := applyContainerRuntimeDefaults(cfg, annotations, rc); err != nil {
		t.Fatalf("applyContainerRuntimeDefaults returned error: %v", err)
	}

	if cfg.MaxVcpuNum != 4 {
		t.Fatalf("MaxVcpuNum = %d, want 4", cfg.MaxVcpuNum)
	}
	if cfg.MemoryThresholdMB != 256 {
		t.Fatalf("memory threshold = %d, want 256", cfg.MemoryThresholdMB)
	}
}

func TestResolveMaxVcpuTrimsAnnotation(t *testing.T) {
	rc := &RuntimeConfig{
		RuntimeResourceConfig: RuntimeResourceConfig{MaxContainerVCPUs: 6},
	}
	annotations := map[string]string{
		ann.ContainerMaxVcpuNum: " 4 ",
	}

	if got := resolveMaxVcpu(annotations, rc); got != 4 {
		t.Fatalf("resolveMaxVcpu = %d, want trimmed annotation value 4", got)
	}
}

func TestResolveMaxVcpuBlankAnnotationFallsBackToRuntime(t *testing.T) {
	rc := &RuntimeConfig{
		RuntimeResourceConfig: RuntimeResourceConfig{MaxContainerVCPUs: 6},
	}
	annotations := map[string]string{
		ann.ContainerMaxVcpuNum: "  ",
	}

	if got := resolveMaxVcpu(annotations, rc); got != 6 {
		t.Fatalf("resolveMaxVcpu = %d, want runtime fallback 6", got)
	}
}

func TestApplyContainerRuntimeDefaultsMemoryFallback(t *testing.T) {
	cfg := &cntr.ContainerConfig{}
	rc := &RuntimeConfig{
		RuntimeResourceConfig: RuntimeResourceConfig{MinContainerMemMB: 24},
	}

	if err := applyContainerRuntimeDefaults(cfg, nil, rc); err != nil {
		t.Fatalf("applyContainerRuntimeDefaults returned error: %v", err)
	}

	if cfg.MemoryReservationMiB() != 24 {
		t.Fatalf("MemoryReservationMiB = %d, want 24", cfg.MemoryReservationMiB())
	}
	if cfg.MemoryThresholdMB != 48 {
		t.Fatalf("memory threshold = %d, want 48", cfg.MemoryThresholdMB)
	}
	if cfg.MaxVcpuNum != defs.DefaultMaxVCPUs {
		t.Fatalf("MaxVcpuNum = %d, want default %d", cfg.MaxVcpuNum, defs.DefaultMaxVCPUs)
	}
}

func TestSandboxConfigRejectsNilSpec(t *testing.T) {
	rc := NewRuntimeConfigWithHost(HostProfile{})

	_, err := SandboxConfig(context.Background(), nil, *rc, t.TempDir(), "sandbox", cntr.PodSandbox, nil)
	if err == nil || err.Error() != "oci spec is required" {
		t.Fatalf("SandboxConfig() error = %v, want oci spec is required", err)
	}
}

func TestSandboxConfigUsesRequestedContainerTypeForInfraClassification(t *testing.T) {
	bundle := t.TempDir()
	rootfs := filepath.Join(bundle, "rootfs")
	if err := os.MkdirAll(rootfs, 0o755); err != nil {
		t.Fatalf("mkdir rootfs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootfs, defs.DefaultFirmwareName), []byte("elf"), 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}

	rc := NewRuntimeConfigWithHost(HostProfile{})
	rc.SetStateDir(t.TempDir())

	single, err := SandboxConfig(context.Background(), &specs.Spec{}, *rc, bundle, "single", cntr.SingleContainer, nil)
	if err != nil {
		t.Fatalf("SandboxConfig(single) returned error: %v", err)
	}
	if single.InfraOnly {
		t.Fatal("single-container sandbox was classified as infra-only")
	}
	if got := single.ContainerConfigs["single"]; got == nil || got.IsInfra {
		t.Fatalf("single container config = %#v, want non-infra", got)
	}

	pod, err := SandboxConfig(context.Background(), &specs.Spec{}, *rc, bundle, "pod", cntr.PodSandbox, nil)
	if err != nil {
		t.Fatalf("SandboxConfig(pod) returned error: %v", err)
	}
	if !pod.InfraOnly {
		t.Fatal("pod sandbox was not classified as infra-only")
	}
	if got := pod.ContainerConfigs["pod"]; got == nil || !got.IsInfra {
		t.Fatalf("pod container config = %#v, want infra", got)
	}
}

func TestBuildContainerConfigCopiesAnnotations(t *testing.T) {
	bundle := t.TempDir()
	rootfs := filepath.Join(bundle, "rootfs")
	if err := os.MkdirAll(rootfs, 0o755); err != nil {
		t.Fatalf("mkdir rootfs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootfs, defs.DefaultFirmwareName), []byte("elf"), 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}

	rc := NewRuntimeConfigWithHost(HostProfile{})
	rc.SetStateDir(t.TempDir())
	spec := specs.Spec{Annotations: map[string]string{
		ann.AutoCloseTimeout: "0",
	}}

	cfg, err := BuildContainerConfig(context.Background(), ContainerConfigRequest{
		ID:            "container1",
		Bundle:        bundle,
		Spec:          spec,
		ContainerType: cntr.SingleContainer,
		RuntimeConfig: rc,
	})
	if err != nil {
		t.Fatalf("BuildContainerConfig returned error: %v", err)
	}

	if got := cfg.Annotations[ann.AutoCloseTimeout]; got != "0" {
		t.Fatalf("copied annotation = %q, want 0", got)
	}
	spec.Annotations[ann.AutoCloseTimeout] = "60s"
	if got := cfg.Annotations[ann.AutoCloseTimeout]; got != "0" {
		t.Fatalf("annotations should be cloned, got %q", got)
	}
}

// Spec acceptance 5 end-to-end at the config layer: an os=zephyr annotation
// must select zephyr on the built config. The E2E case cannot distinguish
// this from the default (it pins os=uniproton == DefaultOS).
func TestBuildContainerConfigOSAnnotationSelectsZephyr(t *testing.T) {
	bundle := t.TempDir()
	rootfs := filepath.Join(bundle, "rootfs")
	if err := os.MkdirAll(rootfs, 0o755); err != nil {
		t.Fatalf("mkdir rootfs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootfs, defs.DefaultFirmwareName), []byte("elf"), 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}

	rc := NewRuntimeConfigWithHost(HostProfile{})
	rc.SetStateDir(t.TempDir())
	spec := specs.Spec{Annotations: map[string]string{
		ann.OSAnnotation: "zephyr",
	}}

	cfg, err := BuildContainerConfig(context.Background(), ContainerConfigRequest{
		ID:            "zephyr1",
		Bundle:        bundle,
		Spec:          spec,
		ContainerType: cntr.SingleContainer,
		RuntimeConfig: rc,
	})
	if err != nil {
		t.Fatalf("BuildContainerConfig returned error: %v", err)
	}
	if cfg.OS != "zephyr" {
		t.Fatalf("cfg.OS = %q, want zephyr (annotation must not be dropped)", cfg.OS)
	}
}

// Spec acceptance 6: pin the min_memory_mb annotation parsing contract — a
// valid value applies, an invalid/blank one leaves the config untouched
// (ignore-with-default is the documented behavior E2E case 9 relies on).
func TestApplyMemoryReservationFromAnnotation(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  uint32 // 0 = config untouched
	}{
		{name: "valid value applies", value: "48", want: 48},
		{name: "invalid value ignored", value: "abc", want: 0},
		{name: "negative value ignored", value: "-5", want: 0},
		{name: "uint32 overflow ignored", value: "4294967296", want: 0},
		{name: "blank value ignored", value: "  ", want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &cntr.ContainerConfig{}
			applyMemoryReservationFromAnnotation(cfg, map[string]string{ann.ContainerMinMemMB: tc.value})
			if got := cfg.MemoryReservationMiB(); got != tc.want {
				t.Fatalf("MemoryReservationMiB = %d, want %d", got, tc.want)
			}
		})
	}
}

// Annotations hold highest priority for values (runtime_annotation_config.go):
// a min_memory_mb annotation overrides an OCI spec memory.reservation. Pins
// the direction so docs and code cannot drift apart again.
func TestApplyResourcesMinMemoryAnnotationOverridesSpecReservation(t *testing.T) {
	reservation := int64(64 * 1024 * 1024)
	spec := specs.Spec{
		Linux: &specs.Linux{
			Resources: &specs.LinuxResources{
				Memory: &specs.LinuxMemory{Reservation: &reservation},
			},
		},
		Annotations: map[string]string{ann.ContainerMinMemMB: "32"},
	}

	builder := &containerConfigBuilder{
		request: ContainerConfigRequest{
			Spec:          spec,
			RuntimeConfig: NewRuntimeConfigWithHost(HostProfile{}),
		},
		annotations: spec.Annotations,
		policy:      cntr.ResourcePolicyOrDefault(nil),
	}
	cfg := &cntr.ContainerConfig{ID: "prio", Resources: &specs.LinuxResources{}}
	if err := builder.applyResources(context.Background(), cfg); err != nil {
		t.Fatalf("applyResources returned error: %v", err)
	}
	if got := cfg.MemoryReservationMiB(); got != 32 {
		t.Fatalf("MemoryReservationMiB = %d, want 32 (annotation overrides spec reservation)", got)
	}
}

func TestResolveFirmwarePathUsesFallbackFirmware(t *testing.T) {
	tmpDir := t.TempDir()
	fwPath := filepath.Join(tmpDir, "default.elf")
	if err := os.WriteFile(fwPath, []byte("elf"), 0o644); err != nil {
		t.Fatalf("write fallback firmware: %v", err)
	}

	got, err := resolveFirmwarePath(tmpDir, "", fwPath)
	if err != nil {
		t.Fatalf("resolveFirmwarePath returned error: %v", err)
	}
	if got != fwPath {
		t.Fatalf("resolveFirmwarePath = %q, want %q", got, fwPath)
	}
}

func TestResolveFirmwarePathAnnotationOverridesFallback(t *testing.T) {
	tmpDir := t.TempDir()
	rootfsDir := filepath.Join(tmpDir, "rootfs")
	if err := os.MkdirAll(filepath.Join(rootfsDir, "images"), 0o755); err != nil {
		t.Fatalf("mkdir rootfs: %v", err)
	}
	annotationPath := filepath.Join(rootfsDir, "images", "app.elf")
	if err := os.WriteFile(annotationPath, []byte("anno"), 0o644); err != nil {
		t.Fatalf("write annotation firmware: %v", err)
	}
	fallbackPath := filepath.Join(tmpDir, "fallback.elf")
	if err := os.WriteFile(fallbackPath, []byte("fallback"), 0o644); err != nil {
		t.Fatalf("write fallback firmware: %v", err)
	}

	got, err := resolveFirmwarePath(rootfsDir, "images/app.elf", fallbackPath)
	if err != nil {
		t.Fatalf("resolveFirmwarePath returned error: %v", err)
	}
	if got != annotationPath {
		t.Fatalf("resolveFirmwarePath = %q, want annotation %q", got, annotationPath)
	}
}

func TestBuildContainerConfigRequiresRuntimeConfig(t *testing.T) {
	_, err := BuildContainerConfig(context.Background(), ContainerConfigRequest{
		ID:            "demo",
		Bundle:        t.TempDir(),
		Spec:          specs.Spec{},
		ContainerType: cntr.SingleContainer,
	})
	if err == nil {
		t.Fatal("expected error when runtime config is nil")
	}
	if err.Error() != "runtime config is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyContainerRuntimeDefaultsRequiresRuntimeConfig(t *testing.T) {
	err := applyContainerRuntimeDefaults(&cntr.ContainerConfig{}, nil, nil)
	if err == nil {
		t.Fatal("expected error when runtime config is nil")
	}
	if err.Error() != "runtime config is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}
