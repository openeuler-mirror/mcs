package oci

import (
	"testing"
)

// FuzzGetBundleImageFile verifies the bundle path resolver handles
// arbitrary relative paths without panicking — the firmware_path
// annotation is user-controllable, so path traversal attempts must be
// safely rejected (returning empty string), never crash the shim.
func FuzzGetBundleImageFile(f *testing.F) {
	f.Add("/firmware.elf")
	f.Add("firmware.elf")
	f.Add("../etc/shadow")
	f.Add("../../../../etc/passwd")
	f.Add("sub/dir/file.bin")
	f.Add("")
	f.Add("/")
	f.Add("./nested/../../../escape")
	f.Add(string([]byte{0xff, 0x00, 'a', 'b'}))
	f.Add(string(make([]byte, 512)))
	f.Fuzz(func(t *testing.T, p string) {
		// Must not panic; empty return is the expected rejection for
		// traversal attempts and malformed paths.
		_ = getBundleImageFile("/test/bundle", p)
	})
}
