package lockutil

import (
	"errors"
	"sync"
	"testing"
)

func TestWithLockExecutesBody(t *testing.T) {
	var mu sync.Mutex
	invoked := false
	WithLock(&mu, func() {
		invoked = true
	})
	if !invoked {
		t.Fatal("expected body to be executed")
	}
}

func TestWithLockValueReturnsValue(t *testing.T) {
	var mu sync.Mutex
	value := WithLockValue(&mu, func() string {
		return "ok"
	})
	if value != "ok" {
		t.Fatalf("expected %q, got %q", "ok", value)
	}
}

func TestWithLockErrorReturnsError(t *testing.T) {
	var mu sync.Mutex
	expected := errors.New("sentinel")
	err := WithLockError(&mu, func() error {
		return expected
	})
	if err != expected {
		t.Fatalf("expected %v, got %v", expected, err)
	}
}

func TestWithLockErrorExecutesBody(t *testing.T) {
	var (
		mu      sync.Mutex
		invoked bool
	)
	err := WithLockError(&mu, func() error {
		invoked = true
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !invoked {
		t.Fatal("expected body to be executed")
	}
}

func TestWithReadLockExecutesBody(t *testing.T) {
	var rwMu sync.RWMutex
	invoked := false
	WithReadLock(&rwMu, func() {
		invoked = true
	})
	if !invoked {
		t.Fatal("expected body to be executed")
	}
}

func TestWithReadLockValueReturnsValue(t *testing.T) {
	var rwMu sync.RWMutex
	value := WithReadLockValue(&rwMu, func() int {
		return 1
	})
	if value != 1 {
		t.Fatalf("expected 1, got %d", value)
	}
}
