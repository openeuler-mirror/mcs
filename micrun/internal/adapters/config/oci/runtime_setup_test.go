//go:build test
// +build test

package oci

import (
	"testing"

	"micrun/internal/adapters/hypervisor/pedestal"
)

func testHostProfile() HostProfile {
	return HostProfile{
		Type:             pedestal.Xen,
		MemLowThreshold:  16,
		MemHighThreshold: 128,
	}
}

func TestNewRuntimeConfigWithHostUsesInjectedDefaults(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(HostProfile{
		Type:             pedestal.Baremetal,
		MemLowThreshold:  12,
		MemHighThreshold: 96,
	})

	if !cfg.StaticResourceManagement {
		t.Fatalf("expected StaticResourceManagement to default to true for baremetal host profile")
	}

	cfg.SetMinContainerMemMB("bad")
	if cfg.MinContainerMemMB != 12 {
		t.Fatalf("MinContainerMemMB = %d, want 12", cfg.MinContainerMemMB)
	}

	cfg.SetMaxContainerMemMB("9999")
	if cfg.MaxContainerMemMB != 96 {
		t.Fatalf("MaxContainerMemMB = %d, want 96", cfg.MaxContainerMemMB)
	}
}

func TestRuntimeConfigSetExclusiveDom0CPU(t *testing.T) {
	cfg := NewRuntimeConfigWithHost(testHostProfile())

	cfg.SetExclusiveDom0CPU("true")
	if !cfg.ExclusiveDom0CPU {
		t.Fatalf("expected ExclusiveDom0CPU field to be true")
	}

	cfg.SetExclusiveDom0CPU("false")
	if cfg.ExclusiveDom0CPU {
		t.Fatalf("expected ExclusiveDom0CPU field to be false")
	}
}
