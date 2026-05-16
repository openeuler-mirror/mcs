package attach

import (
	"io"
	"testing"
)

type countingTTYHandle struct {
	closes int
}

func (h *countingTTYHandle) Read(p []byte) (int, error)  { return 0, io.EOF }
func (h *countingTTYHandle) Write(p []byte) (int, error) { return len(p), nil }
func (h *countingTTYHandle) Close() error {
	h.closes++
	return nil
}

type fdTTYHandle struct {
	countingTTYHandle
	fd uintptr
}

func (h *fdTTYHandle) Fd() uintptr {
	return h.fd
}

type uncomparableTTYHandle struct {
	data   []byte
	closes *int
}

func (h uncomparableTTYHandle) Read(p []byte) (int, error)  { return 0, io.EOF }
func (h uncomparableTTYHandle) Write(p []byte) (int, error) { return len(p), nil }
func (h uncomparableTTYHandle) Close() error {
	*h.closes = *h.closes + 1
	return nil
}

type nilableTTYHandle struct{}

func (h *nilableTTYHandle) Read(p []byte) (int, error) {
	panic("typed nil TTY output should not be read")
}

func (h *nilableTTYHandle) Write(p []byte) (int, error) {
	panic("typed nil TTY input should not be written")
}

func (h *nilableTTYHandle) Close() error {
	panic("typed nil TTY handle should not be closed")
}

func TestFreshTTYHandlesCloseSkipsSharedComparableOutput(t *testing.T) {
	handle := &countingTTYHandle{}

	freshTTYHandles{
		stdin:  handle,
		stdout: handle,
	}.close()

	if handle.closes != 1 {
		t.Fatalf("shared handle closes = %d, want 1", handle.closes)
	}
}

func TestFreshTTYHandlesCloseSkipsSharedFDOutput(t *testing.T) {
	stdin := &fdTTYHandle{fd: 10}
	stdout := &fdTTYHandle{fd: 10}

	freshTTYHandles{
		stdin:  stdin,
		stdout: stdout,
	}.close()

	if stdin.closes != 1 {
		t.Fatalf("stdin closes = %d, want 1", stdin.closes)
	}
	if stdout.closes != 0 {
		t.Fatalf("stdout closes = %d, want 0 for shared fd", stdout.closes)
	}
}

func TestFreshTTYHandlesCloseSkipsTypedNilHandles(t *testing.T) {
	var handle *nilableTTYHandle

	freshTTYHandles{
		stdin:  handle,
		stdout: handle,
	}.close()
}

func TestFreshTTYHandlesCloseToleratesUncomparableHandles(t *testing.T) {
	closes := 0
	handle := uncomparableTTYHandle{
		data:   []byte("tty"),
		closes: &closes,
	}

	freshTTYHandles{
		stdin:  handle,
		stdout: handle,
	}.close()

	if closes != 2 {
		t.Fatalf("uncomparable handle closes = %d, want 2", closes)
	}
}

func TestSameTTYHandle(t *testing.T) {
	shared := &countingTTYHandle{}
	if !sameTTYHandle(shared, shared) {
		t.Fatal("sameTTYHandle should detect shared comparable handles")
	}
	if sameTTYHandle(&countingTTYHandle{}, &countingTTYHandle{}) {
		t.Fatal("sameTTYHandle should not treat distinct handles as shared")
	}
	if !sameTTYHandle(&fdTTYHandle{fd: 12}, &fdTTYHandle{fd: 12}) {
		t.Fatal("sameTTYHandle should detect shared file descriptors")
	}
	if sameTTYHandle(&fdTTYHandle{fd: 12}, &fdTTYHandle{fd: 13}) {
		t.Fatal("sameTTYHandle should not match different file descriptors")
	}

	closes := 0
	uncomparable := uncomparableTTYHandle{data: []byte("tty"), closes: &closes}
	if sameTTYHandle(uncomparable, uncomparable) {
		t.Fatal("sameTTYHandle should not compare uncomparable handles")
	}
}
