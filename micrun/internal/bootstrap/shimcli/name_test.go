package shimcli

import "testing"

func TestShimBinaryNameUsesContainerdConvention(t *testing.T) {
	got := ShimBinaryName("io.containerd.mica.v2")
	if got != "containerd-shim-mica-v2" {
		t.Fatalf("ShimBinaryName() = %q, want %q", got, "containerd-shim-mica-v2")
	}
}

func TestBinaryNameFallsBackToExecutableForInvalidShimName(t *testing.T) {
	got := BinaryName("mica", "/usr/local/bin/custom-shim")
	if got != "custom-shim" {
		t.Fatalf("BinaryName() = %q, want %q", got, "custom-shim")
	}
}
