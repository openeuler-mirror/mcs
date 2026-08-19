package libmica

import (
	"strings"
	"testing"

	defs "micrun/internal/support/definitions"
)

// BenchmarkLastMicaMarker measures the terminal-marker scan over the socket
// response buffer — this runs on every rx chunk, so pathological inputs
// (many embedded markers, very long buffers) must not degrade latency.
func BenchmarkLastMicaMarker(b *testing.B) {
	inputs := []struct {
		name string
		data string
	}{
		{"short-ok", "some output\n" + defs.MicaSuccess},
		{"short-fail", "error detail\n" + defs.MicaFailed},
		{"long-no-marker", strings.Repeat("x", 4096)},
		{"long-embedded-markers", strings.Repeat("prefix"+defs.MicaSuccess+"\n", 100) + defs.MicaFailed},
		{"max-buffer", strings.Repeat("a", defs.MicaSocketBufSize)},
	}
	for _, tc := range inputs {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _, _ = lastMicaMarker(tc.data)
			}
		})
	}
}
