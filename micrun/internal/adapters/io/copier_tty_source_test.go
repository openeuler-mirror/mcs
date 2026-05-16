package io

import (
	"errors"
	stdio "io"
	"strings"
	"testing"
)

func TestTTYReadSourceFallsBackToReader(t *testing.T) {
	source := newTTYReadSource("test", "container1", strings.NewReader("abc"))
	buf := make([]byte, 2)

	n, err := source.read(buf)
	if err != nil {
		t.Fatalf("read returned error: %v", err)
	}
	if n != 2 || string(buf[:n]) != "ab" {
		t.Fatalf("read = (%d, %q), want (2, ab)", n, string(buf[:n]))
	}
}

func TestTTYReadSourceNilReaderReturnsClosedPipe(t *testing.T) {
	source := newTTYReadSource("test", "container1", nil)

	if _, err := source.read(make([]byte, 1)); !errors.Is(err, stdio.ErrClosedPipe) {
		t.Fatalf("read error = %v, want ErrClosedPipe", err)
	}
}

type fdTTYReader struct {
	fd uintptr
}

func (r fdTTYReader) Read(p []byte) (int, error) {
	return 0, stdio.EOF
}

func (r fdTTYReader) Fd() uintptr {
	return r.fd
}

func TestTTYReadSourceUsesFDProvider(t *testing.T) {
	source := newTTYReadSource("test", "container1", fdTTYReader{fd: 42})

	if source.fd != 42 {
		t.Fatalf("source fd = %d, want 42", source.fd)
	}
}

type nilableTTYReader struct{}

func (r *nilableTTYReader) Read(p []byte) (int, error) {
	return 0, stdio.EOF
}

func (r *nilableTTYReader) Fd() uintptr {
	return 42
}

func TestTTYReadSourceTypedNilReaderReturnsClosedPipe(t *testing.T) {
	var reader *nilableTTYReader
	source := newTTYReadSource("test", "container1", reader)

	if source.fd != -1 {
		t.Fatalf("source fd = %d, want -1", source.fd)
	}
	if source.reader != nil {
		t.Fatal("typed nil reader should not be retained")
	}
	if _, err := source.read(make([]byte, 1)); !errors.Is(err, stdio.ErrClosedPipe) {
		t.Fatalf("read error = %v, want ErrClosedPipe", err)
	}
}
