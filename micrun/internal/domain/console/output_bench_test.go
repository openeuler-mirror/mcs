package console

import (
	"strings"
	"testing"
)

// BenchmarkCompressLineEndings measures the output newline compressor on
// pathological RTOS output — this runs on every stdout chunk from the
// copier, so embedded \r\n pairs and long buffers must not degrade latency.
func BenchmarkCompressLineEndings(b *testing.B) {
	inputs := []struct {
		name string
		data []byte
	}{
		{"clean-unix", []byte(strings.Repeat("hello world\n", 100))},
		{"crlv-heavy", []byte(strings.Repeat("line\r\n", 200))},
		{"mixed", []byte(strings.Repeat("a\r\nb\nc\r\r\n", 100))},
		{"no-newline", []byte(strings.Repeat("x", 8192))},
		{"cr-only", []byte(strings.Repeat("\r", 1024))},
	}
	for _, tc := range inputs {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = CompressLineEndings(tc.data)
			}
		})
	}
}

// BenchmarkFilterNUL measures the NUL byte filter — RTOS firmware often
// emits NUL-padded fixed-size records, so this is on the hot stdout path.
func BenchmarkFilterNUL(b *testing.B) {
	nulHeavy := make([]byte, 4096)
	for i := 0; i < len(nulHeavy); i += 2 {
		nulHeavy[i] = 'a'
		nulHeavy[i+1] = 0
	}
	clean := []byte(strings.Repeat("abcdefgh", 512))

	b.Run("nul-heavy", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = FilterNUL(nil, nulHeavy)
		}
	})
	b.Run("clean", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = FilterNUL(nil, clean)
		}
	})
}
