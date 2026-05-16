package io

import (
	"context"
	"testing"
	"time"
)

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
