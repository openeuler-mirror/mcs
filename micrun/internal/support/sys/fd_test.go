package sys

import "testing"

type testFDProvider struct {
	fd uintptr
}

func (p testFDProvider) Fd() uintptr {
	return p.fd
}

type nilableFDProvider struct {
	fd uintptr
}

func (p *nilableFDProvider) Fd() uintptr {
	return p.fd
}

func TestFDOfReturnsDescriptor(t *testing.T) {
	fd, ok := FDOf(testFDProvider{fd: 42})
	if !ok || fd != 42 {
		t.Fatalf("FDOf = (%d, %v), want (42, true)", fd, ok)
	}
}

func TestFDOfRejectsMissingDescriptor(t *testing.T) {
	if fd, ok := FDOf(struct{}{}); ok || fd != 0 {
		t.Fatalf("FDOf without descriptor = (%d, %v), want (0, false)", fd, ok)
	}
}

func TestFDOfRejectsNilValues(t *testing.T) {
	if fd, ok := FDOf(nil); ok || fd != 0 {
		t.Fatalf("FDOf(nil) = (%d, %v), want (0, false)", fd, ok)
	}

	var provider *nilableFDProvider
	if fd, ok := FDOf(provider); ok || fd != 0 {
		t.Fatalf("FDOf(typed nil) = (%d, %v), want (0, false)", fd, ok)
	}
}
