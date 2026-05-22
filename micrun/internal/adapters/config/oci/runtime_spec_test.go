package oci

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSpecReadsBundleConfigJSON(t *testing.T) {
	bundle := t.TempDir()
	configPath := filepath.Join(bundle, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"ociVersion":"1.0.2","process":{"terminal":true}}`), 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	spec, err := LoadSpec(bundle)
	if err != nil {
		t.Fatalf("LoadSpec returned error: %v", err)
	}
	if spec.Version != "1.0.2" {
		t.Fatalf("spec version = %q, want 1.0.2", spec.Version)
	}
	if spec.Process == nil || !spec.Process.Terminal {
		t.Fatalf("spec process terminal = %#v, want true", spec.Process)
	}
}
