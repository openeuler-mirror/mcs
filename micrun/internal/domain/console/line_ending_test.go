package console

import "testing"

func TestInputLineEndingTreatsCRLFAsSingleLineEnd(t *testing.T) {
	var ending inputLineEnding

	first := ending.consume('\r')
	second := ending.consume('\n')

	if !first.lineEnd || first.skip {
		t.Fatalf("CR result = %+v, want line end", first)
	}
	if !second.skip || second.lineEnd {
		t.Fatalf("LF after CR result = %+v, want skip", second)
	}
}

func TestInputLineEndingResetClearsPendingCR(t *testing.T) {
	var ending inputLineEnding
	_ = ending.consume('\r')
	ending.reset()

	got := ending.consume('\n')

	if !got.lineEnd || got.skip {
		t.Fatalf("LF after reset result = %+v, want standalone line end", got)
	}
}

func TestConvertLFToCRLFPreservesExistingCRLF(t *testing.T) {
	got := convertLFToCRLF([]byte("a\nb\r\nc"))
	want := "a\r\nb\r\nc"

	if string(got) != want {
		t.Fatalf("convertLFToCRLF = %q, want %q", got, want)
	}
}
