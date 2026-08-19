package panicsafe

import (
	"runtime"
	"testing"
	"time"
)

func TestGoRunsFunction(t *testing.T) {
	done := make(chan struct{})
	Go("test-runner", func() { close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Go did not run the function")
	}
}

func TestGoRecoversPanicWithoutKillingProcess(t *testing.T) {
	Go("test-panicker", func() { panic("boom") })
	// Give the panicking goroutine time to blow up (or not) before the test
	// process continues; a missing recover would crash the whole test binary.
	runtime.Gosched()
	time.Sleep(50 * time.Millisecond)

	// The guard must keep the process healthy for later work.
	ran := make(chan struct{})
	Go("test-after-panic", func() { close(ran) })
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("process unhealthy after a recovered panic: later goroutine never ran")
	}
}

func TestRecoverDeferrable(t *testing.T) {
	done := make(chan struct{})
	// Production pattern: Recover must be deferred directly (recover() only
	// works when called by a directly-deferred function). close(done) is
	// deferred after it, so LIFO runs it before Recover handles the panic.
	go func() {
		defer Recover("test-deferred")
		defer close(done)
		panic("deferred boom")
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deferred Recover did not complete after panic")
	}
}

func TestRecoverNoPanicIsNoop(t *testing.T) {
	// recover() returns nil when no panic is in flight; calling Recover
	// directly (not deferred) must be a harmless no-op.
	Recover("test-noop")
}
