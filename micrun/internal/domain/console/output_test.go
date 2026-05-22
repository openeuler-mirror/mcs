package console

import (
	"bytes"
	"testing"
)

func TestOutputNormalizerCompressesLineEndings(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want []byte
	}{
		{name: "single CRLF", in: []byte("Hello\r\nWorld"), want: []byte("Hello\r\nWorld")},
		{name: "multiple CRLF", in: []byte("Hello\r\n\r\n\r\nWorld"), want: []byte("Hello\r\nWorld")},
		{name: "single LF", in: []byte("Hello\nWorld"), want: []byte("Hello\r\nWorld")},
		{name: "multiple LF", in: []byte("Hello\n\n\nWorld"), want: []byte("Hello\r\nWorld")},
		{name: "standalone CR", in: []byte("Hello\rWorld"), want: []byte("Hello\rWorld")},
		{name: "only line endings", in: []byte("\r\n\r\n\r\n"), want: []byte("\r\n")},
		{name: "RTOS boot message", in: []byte("Hello, UniProton!\r\n\r\n\r\nopenEuler UniProton #"), want: []byte("Hello, UniProton!\r\nopenEuler UniProton #")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalizer := NewOutputNormalizer(OutputConfig{CompressLineEndings: true})
			got := normalizer.Normalize(tt.in)
			got = append(got, normalizer.Flush()...)
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestOutputNormalizerCompressesAcrossChunks(t *testing.T) {
	normalizer := NewOutputNormalizer(OutputConfig{CompressLineEndings: true})
	fragments := [][]byte{
		[]byte("Hello\r"),
		[]byte("\n"),
		[]byte("\r\n"),
		[]byte("World"),
	}

	var got []byte
	for _, fragment := range fragments {
		got = append(got, normalizer.Normalize(fragment)...)
	}
	got = append(got, normalizer.Flush()...)

	want := []byte("Hello\r\nWorld")
	if !bytes.Equal(got, want) {
		t.Fatalf("fragmented normalize = %q, want %q", got, want)
	}
}

func TestOutputNormalizerFiltersNUL(t *testing.T) {
	normalizer := NewOutputNormalizer(OutputConfig{FilterNUL: true})
	got := normalizer.Normalize([]byte("H\x00e\x00l\x00l\x00o"))
	want := []byte("Hello")
	if !bytes.Equal(got, want) {
		t.Fatalf("NUL filtered output = %q, want %q", got, want)
	}
}

func TestOutputNormalizerFilterOnlyCanReturnInputDirectly(t *testing.T) {
	normalizer := NewOutputNormalizer(OutputConfig{FilterNUL: true})
	input := []byte("hello")
	got := normalizer.Normalize(input)

	if len(got) == 0 {
		t.Fatalf("expected non-empty output")
	}
	if &got[0] != &input[0] {
		t.Fatalf("expected filter-only fast path to return original slice when no NUL present")
	}
}

func TestOutputNormalizerFiltersAndCompresses(t *testing.T) {
	normalizer := NewOutputNormalizer(OutputConfig{FilterNUL: true, CompressLineEndings: true})
	got := normalizer.Normalize([]byte("Hel\x00lo\r\n\r\nWor\x00ld"))
	want := []byte("Hello\r\nWorld")
	if !bytes.Equal(got, want) {
		t.Fatalf("normalized output = %q, want %q", got, want)
	}
}

func TestFilterNULCanReturnInputDirectly(t *testing.T) {
	input := []byte("hello")
	got := FilterNUL(nil, input)

	if len(got) == 0 {
		t.Fatal("expected non-empty output")
	}
	if &got[0] != &input[0] {
		t.Fatal("expected FilterNUL to return original slice when no NUL and destination is nil")
	}
}

func TestOutputNormalizerFiltersAndCompressesCanReturnInputDirectly(t *testing.T) {
	normalizer := NewOutputNormalizer(OutputConfig{FilterNUL: true, CompressLineEndings: true})
	input := []byte("hello")
	got := normalizer.Normalize(input)

	if len(got) == 0 {
		t.Fatalf("expected non-empty output")
	}
	if &got[0] != &input[0] {
		t.Fatalf("expected combined fast path to return original slice when no filter/line-ending work is needed")
	}
}

func TestOutputNormalizerFlushesPendingStandaloneCR(t *testing.T) {
	normalizer := NewOutputNormalizer(OutputConfig{CompressLineEndings: true})
	got := normalizer.Normalize([]byte("Hello\r"))
	got = append(got, normalizer.Flush()...)
	want := []byte("Hello\r")
	if !bytes.Equal(got, want) {
		t.Fatalf("flushed output = %q, want %q", got, want)
	}
}

func TestOutputNormalizerCompressOnlyCanReturnInputDirectly(t *testing.T) {
	normalizer := NewOutputNormalizer(OutputConfig{CompressLineEndings: true})
	input := []byte("hello")
	got := normalizer.Normalize(input)

	if len(got) == 0 {
		t.Fatalf("expected non-empty output")
	}
	if &got[0] != &input[0] {
		t.Fatalf("expected compress-only fast path to return original slice when no line endings are pending")
	}
}

func TestOutputNormalizerCompressWithPendingCRMutatesOutput(t *testing.T) {
	normalizer := NewOutputNormalizer(OutputConfig{CompressLineEndings: true})
	normalizer.Normalize([]byte("\r"))

	input := []byte("h")
	got := normalizer.Normalize(input)
	want := []byte("\rh")
	if !bytes.Equal(got, want) {
		t.Fatalf("normalized output with pending CR = %q, want %q", got, want)
	}
}

func TestEchoSuppressorSuppressesTrackedEcho(t *testing.T) {
	suppressor := NewEchoSuppressor(256)
	suppressor.Track('h')
	suppressor.Track('i')

	got := suppressor.Suppress([]byte("hi there"))
	if got.Suppressed != 2 {
		t.Fatalf("Suppressed = %d, want 2", got.Suppressed)
	}
	if !bytes.Equal(got.Data, []byte(" there")) {
		t.Fatalf("Data = %q, want %q", got.Data, " there")
	}
	if suppressor.Len() != 0 {
		t.Fatalf("tracked length = %d, want 0", suppressor.Len())
	}
}

func TestEchoSuppressorResetsOnMismatch(t *testing.T) {
	suppressor := NewEchoSuppressor(256)
	suppressor.Track('h')
	suppressor.Track('i')

	got := suppressor.Suppress([]byte("ho"))
	if got.Suppressed != 1 {
		t.Fatalf("Suppressed = %d, want 1", got.Suppressed)
	}
	if !got.LostSync {
		t.Fatal("LostSync = false, want true")
	}
	if !bytes.Equal(got.Data, []byte("o")) {
		t.Fatalf("Data = %q, want %q", got.Data, "o")
	}
	if suppressor.Len() != 0 {
		t.Fatalf("tracked length = %d, want 0", suppressor.Len())
	}
}

func TestEchoSuppressorMismatchFastPathReturnsInputDirectly(t *testing.T) {
	suppressor := NewEchoSuppressor(256)
	suppressor.Track('h')

	input := []byte("output")
	got := suppressor.Suppress(input)

	if len(got.Data) == 0 {
		t.Fatal("expected non-empty output")
	}
	if &got.Data[0] != &input[0] {
		t.Fatal("expected mismatch fast path to return original slice")
	}
	if got.Suppressed != 0 || got.LostSync {
		t.Fatalf("Suppress result = %+v, want no suppression or lost sync", got)
	}
	if suppressor.Len() != 0 {
		t.Fatalf("tracked length = %d, want 0 after mismatch", suppressor.Len())
	}
}

func TestEchoSuppressorBoundsWindow(t *testing.T) {
	suppressor := NewEchoSuppressor(4)
	for _, ch := range []byte("abcdef") {
		suppressor.Track(ch)
	}

	if suppressor.Len() != 4 {
		t.Fatalf("tracked length = %d, want 4", suppressor.Len())
	}
	got := suppressor.Suppress([]byte("cdef"))
	if got.Suppressed != 4 || len(got.Data) != 0 {
		t.Fatalf("Suppress result = %+v, want all tracked bytes suppressed", got)
	}
}
