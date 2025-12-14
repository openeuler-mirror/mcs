//go:build test
// +build test

package oci

import (
	"testing"

	"micrun/pkg/pedestal"
)

func TestRuntimeConfigSetExclusiveDom0CPU(t *testing.T) {
	defer pedestal.SetExclusiveDom0CPU(false)

	cfg := NewRuntimeConfig()

	cfg.SetExclusiveDom0CPU("true")
	if !cfg.ExclusiveDom0CPU {
		t.Fatalf("expected ExclusiveDom0CPU field to be true")
	}
	if !pedestal.ExclusiveDom0CPUEnabled() {
		t.Fatalf("expected pedestal ExclusiveDom0CPU flag to be true")
	}

	cfg.SetExclusiveDom0CPU("false")
	if cfg.ExclusiveDom0CPU {
		t.Fatalf("expected ExclusiveDom0CPU field to be false")
	}
	if pedestal.ExclusiveDom0CPUEnabled() {
		t.Fatalf("expected pedestal ExclusiveDom0CPU flag to be false")
	}
}
