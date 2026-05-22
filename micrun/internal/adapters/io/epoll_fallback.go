package io

import (
	"context"
	"time"

	"micrun/internal/support/contextx"
)

const epollFallbackInterval = 100 * time.Millisecond

var defaultEpollFallbackWaiter = epollFallbackWaiter{interval: epollFallbackInterval}

type epollFallbackWaiter struct {
	interval time.Duration
}

func waitFallbackPoll(ctx context.Context) bool {
	return defaultEpollFallbackWaiter.wait(ctx)
}

func (w epollFallbackWaiter) wait(ctx context.Context) bool {
	ctx = contextx.OrBackground(ctx)
	interval := w.interval
	if interval <= 0 {
		interval = epollFallbackInterval
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}
