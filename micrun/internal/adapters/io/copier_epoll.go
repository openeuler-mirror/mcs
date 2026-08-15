package io

import (
	"micrun/internal/support/logger"
)

func (c *Copier) waitForData(waiter *epollWaiter, ttyFd int) bool {
	if ttyFd < 0 {
		return waitFallbackPoll(c.ctx)
	}

	return waiter.wait(c.ctx, ttyFd)
}

func (c *Copier) waitForStdinOrCancel(stdinFd int) bool {
	if stdinFd < 0 {
		return waitFallbackPoll(c.ctx)
	}

	return c.stdinWaiter.wait(c.ctx, stdinFd)
}

func (c *Copier) reenableStdinEpoll() {
	// Read disabled under the waiter lock: disable()/reenable() write it
	// concurrently from other goroutines (init failure, stop paths).
	if c.stdinWaiter.isDisabled() {
		c.stdinWaiter.reenable()
		log.Infof("[IO] Re-enabled stdin epoll for %s", c.config.ContainerID)
	}
}
