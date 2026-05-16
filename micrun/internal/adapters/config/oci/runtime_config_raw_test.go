package oci

import (
	"os"
	"path/filepath"
	"testing"

	"micrun/internal/adapters/hypervisor/pedestal"
	ann "micrun/internal/support/annotations"
	defs "micrun/internal/support/definitions"
)

func TestApplyRawConfigPreservesDefaultsForMissingKeys(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(HostProfile{
		Type:             pedestal.Xen,
		MemLowThreshold:  16,
		MemHighThreshold: 128,
	})

	cfg.applyRawConfig(map[string]string{
		KeyMinMemory: "64",
	})

	if cfg.MinContainerMemMB != 64 {
		t.Fatalf("MinContainerMemMB = %d, want 64", cfg.MinContainerMemMB)
	}
	if cfg.PauseImage != defs.DefaultPauseImage {
		t.Fatalf("PauseImage = %q, want %q", cfg.PauseImage, defs.DefaultPauseImage)
	}
	if cfg.MaxContainerVCPUs != defs.DefaultMaxVCPUs {
		t.Fatalf("MaxContainerVCPUs = %d, want %d", cfg.MaxContainerVCPUs, defs.DefaultMaxVCPUs)
	}
}

func TestApplyRawConfigCoversRuntimeKeys(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(HostProfile{})

	cfg.applyRawConfig(map[string]string{
		KeySharedCPUPool: "true",
		KeyStateDir:      "/run/custom-micrun",
	})

	if !cfg.SharedCPUPool {
		t.Fatal("expected SharedCPUPool to be enabled")
	}
	if cfg.StateDir != "/run/custom-micrun" {
		t.Fatalf("StateDir = %q, want /run/custom-micrun", cfg.StateDir)
	}
}

func TestNewRuntimeConfigDefaultsStateDir(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(HostProfile{})
	if cfg.StateDir != defs.MicrunStateDir {
		t.Fatalf("StateDir = %q, want %q", cfg.StateDir, defs.MicrunStateDir)
	}
}

func TestParseRuntimeConfigFromAnnoUsesRuntimeAnnotationKeys(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(HostProfile{
		Type:             pedestal.Xen,
		MemLowThreshold:  16,
		MemHighThreshold: 128,
	})

	cfg.ParseRuntimeConfigFromAnno(map[string]string{
		ann.RuntimeDebug:              " true ",
		ann.RuntimeMaxContainerCPUs:   " 3 ",
		ann.RuntimeMaxContainerMemory: "96",
		ann.RuntimePauseImage:         " pause:annotation ",
		ann.RuntimeExclusiveDom0CPU:   "true",
	})

	if !cfg.Debug {
		t.Fatal("Debug = false, want true")
	}
	if cfg.MaxContainerVCPUs != 3 {
		t.Fatalf("MaxContainerVCPUs = %d, want 3", cfg.MaxContainerVCPUs)
	}
	if cfg.MaxContainerMemMB != 96 {
		t.Fatalf("MaxContainerMemMB = %d, want 96", cfg.MaxContainerMemMB)
	}
	if cfg.PauseImage != "pause:annotation" {
		t.Fatalf("PauseImage = %q, want pause:annotation", cfg.PauseImage)
	}
	if !cfg.ExclusiveDom0CPU {
		t.Fatal("ExclusiveDom0CPU = false, want true")
	}
}

func TestSetPauseImageIgnoresBlankAndTrimsValue(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(HostProfile{})
	defaultPause := cfg.PauseImage

	cfg.SetPauseImage("  ")
	if cfg.PauseImage != defaultPause {
		t.Fatalf("PauseImage = %q, want default %q after blank value", cfg.PauseImage, defaultPause)
	}

	cfg.SetPauseImage("  pause:trimmed  ")
	if cfg.PauseImage != "pause:trimmed" {
		t.Fatalf("PauseImage = %q, want trimmed value", cfg.PauseImage)
	}
}

func TestSetStateDirIgnoresBlankAndTrimsValue(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(HostProfile{})
	defaultStateDir := cfg.StateDir

	cfg.SetStateDir("  ")
	if cfg.StateDir != defaultStateDir {
		t.Fatalf("StateDir = %q, want default %q after blank value", cfg.StateDir, defaultStateDir)
	}

	cfg.SetStateDir("  /run/custom-micrun  ")
	if cfg.StateDir != "/run/custom-micrun" {
		t.Fatalf("StateDir = %q, want trimmed value", cfg.StateDir)
	}
}

func TestSetStateDirIgnoresRelativeValue(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(HostProfile{})
	defaultStateDir := cfg.StateDir

	cfg.SetStateDir("relative-state")

	if cfg.StateDir != defaultStateDir {
		t.Fatalf("StateDir = %q, want default %q after relative value", cfg.StateDir, defaultStateDir)
	}
}

func TestSetStateDirCleansAbsoluteValue(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(HostProfile{})
	dir := filepath.Join(t.TempDir(), "state", "..", "runtime")

	cfg.SetStateDir(dir)

	if cfg.StateDir != filepath.Clean(dir) {
		t.Fatalf("StateDir = %q, want clean path %q", cfg.StateDir, filepath.Clean(dir))
	}
}

func TestRuntimeBoolSettersTrimValues(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(HostProfile{})

	cfg.SetDebug(" true ")
	if !cfg.Debug {
		t.Fatal("Debug = false, want true from trimmed value")
	}

	cfg.SetHugePageSupport(" true ")
	if !cfg.HugePageSupport {
		t.Fatal("HugePageSupport = false, want true from trimmed value")
	}

	cfg.SetStaticResourceManagement(" true ")
	if !cfg.StaticResourceManagement {
		t.Fatal("StaticResourceManagement = false, want true from trimmed value")
	}

	cfg.SetExclusiveDom0CPU(" true ")
	if !cfg.ExclusiveDom0CPU {
		t.Fatal("ExclusiveDom0CPU = false, want true from trimmed value")
	}

	cfg.SetSharedCPUPool(" true ")
	if !cfg.SharedCPUPool {
		t.Fatal("SharedCPUPool = false, want true from trimmed value")
	}
}

func TestRuntimeBoolSettersIgnoreBlankAndInvalidValues(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(HostProfile{})
	cfg.Debug = true
	cfg.HugePageSupport = true
	cfg.StaticResourceManagement = true
	cfg.ExclusiveDom0CPU = true
	cfg.SharedCPUPool = true

	cfg.SetDebug(" ")
	cfg.SetHugePageSupport("invalid")
	cfg.SetStaticResourceManagement(" ")
	cfg.SetExclusiveDom0CPU("invalid")
	cfg.SetSharedCPUPool(" ")

	if !cfg.Debug {
		t.Fatal("Debug should remain true after blank value")
	}
	if !cfg.HugePageSupport {
		t.Fatal("HugePageSupport should remain true after invalid value")
	}
	if !cfg.StaticResourceManagement {
		t.Fatal("StaticResourceManagement should remain true after blank value")
	}
	if !cfg.ExclusiveDom0CPU {
		t.Fatal("ExclusiveDom0CPU should remain true after invalid value")
	}
	if !cfg.SharedCPUPool {
		t.Fatal("SharedCPUPool should remain true after blank value")
	}
}

func TestRuntimeUintSettersTrimValues(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(HostProfile{
		MemLowThreshold:  16,
		MemHighThreshold: 128,
	})

	cfg.SetMaxContainerVCPUs(" 5 ")
	cfg.SetMaxContainerMemMB(" 96 ")
	cfg.SetMinContainerMemMB(" 32 ")
	cfg.SetMiniVCPUNum(" 2 ")

	if cfg.MaxContainerVCPUs != 5 {
		t.Fatalf("MaxContainerVCPUs = %d, want 5", cfg.MaxContainerVCPUs)
	}
	if cfg.MaxContainerMemMB != 96 {
		t.Fatalf("MaxContainerMemMB = %d, want 96", cfg.MaxContainerMemMB)
	}
	if cfg.MinContainerMemMB != 32 {
		t.Fatalf("MinContainerMemMB = %d, want 32", cfg.MinContainerMemMB)
	}
	if cfg.MiniVCPUNum != 2 {
		t.Fatalf("MiniVCPUNum = %d, want 2", cfg.MiniVCPUNum)
	}
}

func TestParseRuntimeMemoryMBChecksHostBounds(t *testing.T) {
	host := HostProfile{
		MemLowThreshold:  16,
		MemHighThreshold: 128,
	}

	if got, ok := parseRuntimeMemoryMB(KeyMaxMemory, " 64 ", host); !ok || got != 64 {
		t.Fatalf("parseRuntimeMemoryMB = (%d, %v), want (64, true)", got, ok)
	}
	if _, ok := parseRuntimeMemoryMB(KeyMaxMemory, "8", host); ok {
		t.Fatal("parseRuntimeMemoryMB accepted memory below host low threshold")
	}
	if _, ok := parseRuntimeMemoryMB(KeyMaxMemory, "256", host); ok {
		t.Fatal("parseRuntimeMemoryMB accepted memory above host high threshold")
	}
}

func TestSetMiniVCPUNumIgnoresBlankAndInvalidValues(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(HostProfile{})
	cfg.MiniVCPUNum = 4

	cfg.SetMiniVCPUNum(" ")
	if cfg.MiniVCPUNum != 4 {
		t.Fatalf("MiniVCPUNum = %d, want preserved value after blank input", cfg.MiniVCPUNum)
	}

	cfg.SetMiniVCPUNum("invalid")
	if cfg.MiniVCPUNum != 4 {
		t.Fatalf("MiniVCPUNum = %d, want preserved value after invalid input", cfg.MiniVCPUNum)
	}
}

func TestSetMaxContainerVCPUsDefaultsOnBlankInvalidAndZero(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(HostProfile{})
	cfg.MaxContainerVCPUs = 7

	cfg.SetMaxContainerVCPUs(" ")
	if cfg.MaxContainerVCPUs != defs.DefaultMaxVCPUs {
		t.Fatalf("MaxContainerVCPUs = %d, want default after blank input", cfg.MaxContainerVCPUs)
	}

	cfg.MaxContainerVCPUs = 7
	cfg.SetMaxContainerVCPUs("invalid")
	if cfg.MaxContainerVCPUs != defs.DefaultMaxVCPUs {
		t.Fatalf("MaxContainerVCPUs = %d, want default after invalid input", cfg.MaxContainerVCPUs)
	}

	cfg.MaxContainerVCPUs = 7
	cfg.SetMaxContainerVCPUs("0")
	if cfg.MaxContainerVCPUs != defs.DefaultMaxVCPUs {
		t.Fatalf("MaxContainerVCPUs = %d, want default after zero input", cfg.MaxContainerVCPUs)
	}
}

func TestParseRuntimeFromTomlAppliesScalarRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "micrun.toml")
	content := []byte(`
[container_minmem]
container_minmem = 64

[pause_image]
pause_image = "pause:test"
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write toml config: %v", err)
	}

	cfg := NewRuntimeConfigWithHost(HostProfile{
		Type:             pedestal.Xen,
		MemLowThreshold:  16,
		MemHighThreshold: 128,
	})
	if err := cfg.ParseRuntimeFromToml(path); err != nil {
		t.Fatalf("ParseRuntimeFromToml returned error: %v", err)
	}

	if cfg.MinContainerMemMB != 64 {
		t.Fatalf("MinContainerMemMB = %d, want 64", cfg.MinContainerMemMB)
	}
	if cfg.PauseImage != "pause:test" {
		t.Fatalf("PauseImage = %q, want pause:test", cfg.PauseImage)
	}
}
