package io

import (
	"context"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// init() must drop the registeredFds bookkeeping when it closes a failed
// epoll instance: stale entries would make a later init skip EPOLL_CTL_ADD on
// the fresh instance, leaving the fd permanently unwatched and the stream
// silently stalled.
func TestEpollWaiterInitFailureClearsRegisteredFds(t *testing.T) {
	pipe := make([]int, 2)
	if err := unix.Pipe(pipe); err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer unix.Close(pipe[0])
	defer unix.Close(pipe[1])
	w := newEpollWaiter(pipe[0], pipe[1], false)

	if err := w.init(pipe[0]); err != nil {
		t.Fatalf("initial init: %v", err)
	}
	if _, ok := w.registeredFds[pipe[0]]; !ok {
		t.Fatalf("pipe fd not registered after successful init: %v", w.registeredFds)
	}

	// A closed fd makes EPOLL_CTL_ADD fail with EBADF, driving init into its
	// error branch while registeredFds still holds the earlier binding.
	stale := pipe[1]
	unix.Close(stale)
	if err := w.init(stale); err == nil {
		t.Fatal("init on a closed fd unexpectedly succeeded")
	}
	if len(w.registeredFds) != 0 {
		t.Fatalf("registeredFds not cleared after init failure: %v", w.registeredFds)
	}

	// The next init must actually re-register the fd, not skip it because of
	// a stale bookkeeping entry.
	if err := w.init(pipe[0]); err != nil {
		t.Fatalf("re-init after failure: %v", err)
	}
	if _, ok := w.registeredFds[pipe[0]]; !ok {
		t.Fatalf("pipe fd not re-registered after recovery init: %v", w.registeredFds)
	}

	w.reset()
}

func TestWaitFallbackPollReturnsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if waitFallbackPoll(ctx) {
		t.Fatal("waitFallbackPoll returned true for canceled context")
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("waitFallbackPoll took %v after cancellation", elapsed)
	}
}

func TestWaitFallbackPollAcceptsNilContext(t *testing.T) {
	waiter := epollFallbackWaiter{interval: time.Nanosecond}
	if !waiter.wait(nil) {
		t.Fatal("waitFallbackPoll returned false for nil context")
	}
}
