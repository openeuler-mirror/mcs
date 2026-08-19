package io

import (
	"testing"
)

// FuzzIsValidFIFOPath verifies the FIFO path validator handles arbitrary
// strings without panicking — the paths originate from containerd client
// requests and can contain any bytes.
func FuzzIsValidFIFOPath(f *testing.F) {
	f.Add("/run/containerd/fifo/abc-stdin")
	f.Add("binary://stdin")
	f.Add("fd://1")
	f.Add("socket:///run/micrun/stderr.sock")
	f.Add("")
	f.Add("/")
	f.Add("../etc/passwd")
	f.Add("/tmp/\x00nul")
	f.Add(string(make([]byte, 1024)))
	f.Fuzz(func(t *testing.T, path string) {
		// Must not panic; result can be true or false for arbitrary input.
		_ = IsValidFIFOPath(path)
	})
}
