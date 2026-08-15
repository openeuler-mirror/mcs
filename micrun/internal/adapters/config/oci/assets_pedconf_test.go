package oci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pedestal "micrun/internal/adapters/hypervisor/pedestal"
	ann "micrun/internal/support/annotations"
)

func staticAnnotations(annotations map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := annotations[key]
		return v, ok
	}
}

// The ped.conf annotation is pod-author-controlled input documented as
// rootfs-relative. On non-Xen pedestals it was passed through verbatim,
// letting a pod author make the root shim/micad read arbitrary host files as
// the pedestal image.
func TestExtPedConfigNonXenRejectsHostPath(t *testing.T) {
	rootfs := t.TempDir()
	for _, hostile := range []string{"/etc/shadow", "../../etc/shadow", "/proc/version"} {
		_, _, err := extPedConfig(staticAnnotations(map[string]string{
			ann.PedestalConf: hostile,
		}), rootfs, HostProfile{Type: pedestal.Baremetal})
		if err == nil {
			t.Fatalf("extPedConfig(%q) accepted a host path on non-Xen pedestal", hostile)
		}
	}
}

func TestExtPedConfigNonXenResolvesRootfsRelativePath(t *testing.T) {
	rootfs := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootfs, "boot.bin"), []byte("img"), 0o644); err != nil {
		t.Fatalf("write rootfs file: %v", err)
	}

	pedtype, pedconf, err := extPedConfig(staticAnnotations(map[string]string{
		ann.PedestalConf: "boot.bin",
	}), rootfs, HostProfile{Type: pedestal.Baremetal})
	if err != nil {
		t.Fatalf("extPedConfig error = %v", err)
	}
	if pedtype != pedestal.Baremetal {
		t.Fatalf("pedtype = %v, want Baremetal", pedtype)
	}
	if !strings.HasSuffix(pedconf, "boot.bin") || !filepath.IsAbs(pedconf) {
		t.Fatalf("pedconf = %q, want absolute path inside rootfs", pedconf)
	}

	// No annotation on non-Xen: pedconf stays empty (it is optional there).
	_, pedconf, err = extPedConfig(staticAnnotations(nil), rootfs, HostProfile{Type: pedestal.Baremetal})
	if err != nil {
		t.Fatalf("extPedConfig without annotation error = %v", err)
	}
	if pedconf != "" {
		t.Fatalf("pedconf = %q, want empty without annotation", pedconf)
	}
}
