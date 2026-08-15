package libmica

import (
	"context"
	"testing"
)

// FuzzParseMicaStatus verifies the micad status parser never panics on
// arbitrary daemon responses — the protocol is text over a socket, so a
// corrupted or adversarial daemon must not crash the shim.
func FuzzParseMicaStatus(f *testing.F) {
	// Seed with known-good and known-bad patterns.
	f.Add("Running 0-3 1024")
	f.Add("Stopped")
	f.Add("")
	f.Add("\x00\xff\xfe invalid utf8 \x01")
	f.Add("Running\n\tCPU: 0-3\nMem: 1024\n")
	f.Add("Suspended")
	f.Add("state=Running cpu=0-3 mem=1024 extra=field")
	f.Add(string(make([]byte, 4096)))
	f.Fuzz(func(t *testing.T, raw string) {
		// Must not panic; error return is acceptable for garbage input.
		_, _ = parseMicaStatusWithCPUProvider(context.Background(), raw, func(context.Context) int { return 4 })
	})
}

// FuzzSplitMicaStatusFields exercises the field splitter with arbitrary
// whitespace-separated tokens — the parser indexes fields positionally, so
// a short or malformed response must not cause an out-of-bounds panic.
func FuzzSplitMicaStatusFields(f *testing.F) {
	f.Add("Running 0-3 1024")
	f.Add("a")
	f.Add("")
	f.Add(" ")
	f.Add("\t\n \r\n")
	f.Add("a b c d e f g h i j k l m n o p")
	f.Add(string([]byte{0xff, 0xfe, 0x00, 0x01}))
	f.Fuzz(func(t *testing.T, raw string) {
		_, _ = splitMicaStatusFields(raw)
	})
}
