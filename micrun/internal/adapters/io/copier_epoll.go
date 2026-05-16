package io

import (
	"micrun/internal/support/logger"
)

func (c *Copier) waitForData(ttyFd int) bool {
	if ttyFd < 0 {
		return waitFallbackPoll(c.ctx)
	}

	return c.ttyWaiter.wait(c.ctx, ttyFd)
}

func (c *Copier) waitForStdinOrCancel(stdinFd int) bool {
	if stdinFd < 0 {
		return waitFallbackPoll(c.ctx)
	}

	return c.stdinWaiter.wait(c.ctx, stdinFd)
}

func (c *Copier) reenableStdinEpoll() {
	if c.stdinWaiter.disabled {
		c.stdinWaiter.reenable()
		log.Infof("[IO] Re-enabled stdin epoll for %s", c.config.ContainerID)
	}
}
