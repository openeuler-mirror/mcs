package shim

import (
	"context"
	"errors"
	"testing"
	"time"

	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

type bootstrapFailingCloser struct {
	err    error
	closed bool
}

func (c *bootstrapFailingCloser) Close() error {
	c.closed = true
	return c.err
}

func TestCloseShimExtraFileAfterStartFailureJoinsCloseError(t *testing.T) {
	startErr := errors.New("start failed")
	closeErr := errors.New("close failed")
	closer := &bootstrapFailingCloser{err: closeErr}

	err := closeShimExtraFileAfterStartFailure(startErr, closer)

	if !closer.closed {
		t.Fatal("extra file was not closed")
	}
	if !errors.Is(err, startErr) {
		t.Fatalf("joined error should contain start error: %v", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("joined error should contain close error: %v", err)
	}
}

func TestCloseShimExtraFileHandlesNilCloser(t *testing.T) {
	if err := closeShimExtraFile(nil); err != nil {
		t.Fatalf("closeShimExtraFile(nil) error = %v", err)
	}
}

func TestGetContainerSocketAddrHonorsCanceledContextBeforeLoadingBundle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := getContainerSocketAddr(ctx, "/path/that/does/not/exist", shimv2.StartOpts{})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("getContainerSocketAddr error = %v, want context.Canceled", err)
	}
}

func TestStartShimHonorsCanceledContextBeforeFilesystemWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (&shimService{}).StartShim(ctx, shimv2.StartOpts{ID: "container1"})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("StartShim error = %v, want context.Canceled", err)
	}
}

func TestNewCommandHonorsCanceledContextBeforeExecutableLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := newCommand(ctx, shimv2.StartOpts{ID: "container1"}, ".")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("newCommand error = %v, want context.Canceled", err)
	}
}

func TestEnsureShimSocketHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, _, err := ensureShimSocket(ctx, shimv2.StartOpts{ID: "container1"})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ensureShimSocket error = %v, want context.Canceled", err)
	}
}

func TestKillWithBackoffFuncRetriesWithExponentialDelays(t *testing.T) {
	attempts := 0
	var waits []time.Duration
	err := killWithBackoffFunc(func() error {
		attempts++
		if attempts < 3 {
			return errors.New("kill failed")
		}
		return nil
	}, func(wait time.Duration) {
		waits = append(waits, wait)
	})
	if err != nil {
		t.Fatalf("killWithBackoffFunc returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}
	if len(waits) != len(want) {
		t.Fatalf("wait count = %d, want %d (%v)", len(waits), len(want), waits)
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("wait[%d] = %v, want %v", i, waits[i], want[i])
		}
	}
}

func TestKillWithBackoffFuncDoesNotSleepAfterFinalFailure(t *testing.T) {
	expectedErr := errors.New("kill failed")
	attempts := 0
	var waits []time.Duration
	err := killWithBackoffFunc(func() error {
		attempts++
		return expectedErr
	}, func(wait time.Duration) {
		waits = append(waits, wait)
	})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("killWithBackoffFunc error = %v, want wrapped final error", err)
	}
	if attempts != 5 {
		t.Fatalf("attempts = %d, want 5", attempts)
	}
	if len(waits) != 4 {
		t.Fatalf("wait count = %d, want 4 (%v)", len(waits), waits)
	}
}

func TestKillWithBackoffRejectsNilProcess(t *testing.T) {
	if err := killWithBackoff(nil); err == nil {
		t.Fatal("expected nil process error")
	}
}
