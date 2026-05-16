//go:build test
// +build test

package oci

import (
	"os"
	"testing"

	configstack "micrun/internal/adapters/config/configstack"
)

func TestRuntimeStackReplace(t *testing.T) {
	stack := NewRuntimeStackWithHost(testHostProfile())
	if stack.Config() == nil {
		t.Fatal("expected default config")
	}

	cfg := NewRuntimeConfigWithHost(testHostProfile())
	cfg.ExclusiveDom0CPU = true
	stack.Replace(cfg)
	if stack.Config() != cfg {
		t.Fatalf("stack did not hold provided config")
	}
	if !stack.Config().ExclusiveDom0CPU {
		t.Fatal("expected ExclusiveDom0CPU to remain true")
	}

	stack.Replace(nil)
	if stack.Config() == cfg {
		t.Fatal("expected stack to reset when replacing with nil")
	}
	if stack.Config().ExclusiveDom0CPU {
		t.Fatal("expected reset config to disable ExclusiveDom0CPU")
	}
}

func TestApplyMicrunFiles(t *testing.T) {
	stack := NewRuntimeStackWithHost(testHostProfile())
	tmp := t.TempDir()
	conf := tmp + "/micrun.ini"
	if err := os.WriteFile(conf, []byte("[static_resource]\nmax_container_vcpu=2\n"), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	if err := stack.ApplyMicrunFiles([]configstack.MicrunConfigFile{{Path: conf, Format: configstack.FormatINI}}); err != nil {
		t.Fatalf("ApplyMicrunFiles returned error: %v", err)
	}
	if stack.Config().MaxContainerVCPUs != 2 {
		t.Fatalf("expected MaxContainerVCPUs=2, got %d", stack.Config().MaxContainerVCPUs)
	}
}

func TestApplyMicrunFilesReturnsApplyErrors(t *testing.T) {
	stack := NewRuntimeStackWithHost(testHostProfile())

	err := stack.ApplyMicrunFiles([]configstack.MicrunConfigFile{{
		Path:   "/tmp/unsupported.yaml",
		Format: configstack.FormatUnknown,
	}})

	if err == nil {
		t.Fatal("expected apply error for unsupported config format")
	}
}

func TestNewRuntimeStackWithHostResetKeepsInjectedHostProfile(t *testing.T) {
	stack := NewRuntimeStackWithHost(HostProfile{
		MemLowThreshold:  14,
		MemHighThreshold: 88,
	})

	stack.Replace(nil)
	cfg := stack.Config()
	cfg.SetMinContainerMemMB("invalid")
	cfg.SetMaxContainerMemMB("999")

	if cfg.MinContainerMemMB != 14 {
		t.Fatalf("MinContainerMemMB = %d, want 14", cfg.MinContainerMemMB)
	}
	if cfg.MaxContainerMemMB != 88 {
		t.Fatalf("MaxContainerMemMB = %d, want 88", cfg.MaxContainerMemMB)
	}
}
