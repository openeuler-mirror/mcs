//go:build test
// +build test

package oci

import (
	"os"
	"testing"

	configstack "micrun/pkg/configstack"
)

func TestRuntimeStackReplace(t *testing.T) {
	stack := NewRuntimeStack()
	if stack.Config() == nil {
		t.Fatal("expected default config")
	}

	cfg := NewRuntimeConfig()
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
	stack := NewRuntimeStack()
	tmp := t.TempDir()
	conf := tmp + "/micrun.ini"
	if err := os.WriteFile(conf, []byte("[static_resource]\nmax_container_vcpu=2\n"), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	stack.ApplyMicrunFiles([]configstack.MicrunConfigFile{{Path: conf, Format: configstack.FormatINI}})
	if stack.Config().MaxContainerCPUs != 2 {
		t.Fatalf("expected MaxContainerCPUs=2, got %d", stack.Config().MaxContainerCPUs)
	}
}
