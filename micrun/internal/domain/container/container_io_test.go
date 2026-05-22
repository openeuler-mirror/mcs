package container

import (
	"errors"
	"reflect"
	"testing"
)

type failingCloser struct {
	err error
}

func (f failingCloser) Close() error {
	return f.err
}

func TestAppendCloseErrorKeepsPrimaryError(t *testing.T) {
	primaryErr := errors.New("primary")
	closeErr := errors.New("close failed")
	err := primaryErr

	appendCloseError(&err, "tty", failingCloser{err: closeErr})

	if !errors.Is(err, primaryErr) {
		t.Fatalf("joined error should contain primary error: %v", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("joined error should contain close error: %v", err)
	}
}

func TestAppendCloseErrorReturnsCloseErrorWithoutPrimary(t *testing.T) {
	closeErr := errors.New("close failed")
	var err error

	appendCloseError(&err, "tty", failingCloser{err: closeErr})

	if !errors.Is(err, closeErr) {
		t.Fatalf("close error should be returned: %v", err)
	}
}

func TestContainerTTYDiscoveryRootsUseSandboxDependency(t *testing.T) {
	want := []string{"/dev", "/custom/micrun", "/tmp/mica"}
	container := &Container{
		sandbox: &Sandbox{
			deps: &Dependencies{
				TTYDiscoveryRoots: func() []string {
					return want
				},
			},
		},
	}

	if got := container.ttyDiscoveryRoots(); !reflect.DeepEqual(got, want) {
		t.Fatalf("tty discovery roots = %v, want %v", got, want)
	}
}
