package io

import (
	"io"
	"sync/atomic"
	"time"

	"micrun/internal/support/logger"
)

func (c *Copier) noteStdinActivity() {
	atomic.AddUint64(&c.stdinActivityGeneration, 1)
}

func (c *Copier) logPostInputOutput(outputLen int) {
	currentGeneration := atomic.LoadUint64(&c.stdinActivityGeneration)
	observedGeneration := atomic.LoadUint64(&c.observedOutputGeneration)

	if !shouldMarkPostInputOutput(currentGeneration, observedGeneration, outputLen) {
		return
	}

	if atomic.CompareAndSwapUint64(&c.observedOutputGeneration, observedGeneration, currentGeneration) {
		log.Infof("[IO] Observed output after stdin for %s (generation=%d, bytes=%d)",
			c.config.ContainerID, currentGeneration, outputLen)
	}
}

func (c *Copier) copyStdin() {
	defer c.wg.Done()

	buf := make([]byte, c.config.StdinBufSize)

	log.Infof("[IO] stdin→TTY copier started for %s (terminal=%v)", c.config.ContainerID, c.config.Terminal)

	for {
		n, err := c.stdinFifo.Read(buf)
		// Only log errors and significant events, not every read (high-frequency)
		if err != nil && !isClosed(err) && err != io.EOF && !isEAGAIN(err) {
			log.Tracef("[IO] stdin FIFO read: n=%d, err=%v", n, err)
		}
		if err != nil {
			if isClosed(err) || err == io.EOF {
				if c.handleStdinEOF() == stdinLoopStop {
					return
				}
				continue
			}
			if isEAGAIN(err) {
				c.handleStdinEAGAIN()
				continue
			}
			log.Errorf("[IO] stdin read error for %s: %v", c.config.ContainerID, err)
			c.publishEvent(IOError, err)
			return
		}

		if n == 0 {
			continue
		}

		c.noteStdinActivity()
		c.markStdinDataReceived()

		// Use different processing logic for TTY vs non-TTY mode
		if c.config.Terminal {
			c.copyStdinTTY(buf[:n])
		} else {
			c.copyStdinNonTTY(buf[:n])
		}
	}
}

type stdinLoopDecision int

const (
	stdinLoopContinue stdinLoopDecision = iota
	stdinLoopStop
)

func (c *Copier) handleStdinEOF() stdinLoopDecision {
	select {
	case <-c.ctx.Done():
		log.Infof("[IO] stdin→TTY copier canceled for %s (exiting on EOF)", c.config.ContainerID)
		return stdinLoopStop
	default:
	}

	if c.attachClientConnected {
		if c.config.Terminal {
			log.Infof("[IO] stdin EOF for %s (attach client closed stdin, exiting)", c.config.ContainerID)
			c.publishEvent(StdinClosed, nil)
			return stdinLoopStop
		}

		log.Infof("[IO] stdin EOF for %s (non-TTY attach closed stdin, keeping stdout open for output/reattach)", c.config.ContainerID)
		c.attachClientConnected = false
		c.stdinEOFSeen = true
		if !c.waitForStdinOrCancel(c.stdinFIFOFD()) {
			return stdinLoopStop
		}
		return stdinLoopContinue
	}

	if !c.stdinEOFSeen {
		log.Infof("[IO] stdin EOF for %s (no attach client yet, waiting)", c.config.ContainerID)
		c.stdinEOFSeen = true
	}
	if !c.waitForStdinOrCancel(c.stdinFIFOFD()) {
		return stdinLoopStop
	}
	return stdinLoopContinue
}

func (c *Copier) handleStdinEAGAIN() {
	if !c.stdinEOFSeen {
		return
	}
	log.Infof("[IO] stdin writer detected for %s (reattach detected)", c.config.ContainerID)
	c.stdinEOFSeen = false
	c.reenableStdinEpoll()
}

func (c *Copier) markStdinDataReceived() {
	if c.stdinEOFSeen {
		log.Infof("[IO] stdin data received for %s (writer connected)", c.config.ContainerID)
		c.stdinEOFSeen = false
		if !c.attachClientConnected {
			log.Infof("[IO] Attach client connected for %s", c.config.ContainerID)
			c.attachClientConnected = true
		}
		c.reenableStdinEpoll()
		return
	}

	if !c.attachClientConnected {
		log.Infof("[IO] First data received for %s (attach client connected)", c.config.ContainerID)
		c.attachClientConnected = true
		c.reenableStdinEpoll()
	}
}

func (c *Copier) stdinFIFOFD() int {
	fd, ok := fdOf(c.stdinFifo)
	if !ok {
		return -1
	}
	return fd
}

// copyStdinTTY delegates user-facing terminal semantics to the domain
// interpreter and executes the resulting device actions.
func (c *Copier) copyStdinTTY(data []byte) {
	c.executeInputActions(c.input.Interpret(data))
}

func (c *Copier) trackSentCharForEcho(ch byte) {
	if !c.suppressEcho || ch == '\r' || ch == '\n' {
		return
	}

	c.echoSuppressor.Track(ch)
	log.Tracef("[IO] Tracking sent char: %d (%q), total tracked: %d", ch, ch, c.echoSuppressor.Len())
}

func (c *Copier) trackSentCharsForEcho(data []byte) {
	for _, ch := range data {
		c.trackSentCharForEcho(ch)
	}
}

func (c *Copier) copyStdinNonTTY(data []byte) {
	logNonTTYInputActivity(c.config.ContainerID, data)
	c.executeInputActions(c.input.Interpret(data))
}

func (c *Copier) writeTTY(data []byte) (int, error) {
	if c.ttyIn == nil {
		return 0, io.ErrClosedPipe
	}

	delay := c.config.TTYWriteDelay
	if delay < 0 {
		delay = 0
	}

	written := 0
	var singleByte [1]byte
	for i, ch := range data {
		select {
		case <-c.ctx.Done():
			return written, c.ctx.Err()
		default:
		}

		singleByte[0] = ch
		n, err := c.ttyIn.Write(singleByte[:])
		written += n
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}

		if lineDelay := c.lineDelayAfter(ch); lineDelay > 0 {
			log.Tracef("[IO] TTY write line-paced for %s: delayed by %v",
				c.config.ContainerID, lineDelay)
			if !sleepWithContext(c.ctx.Done(), lineDelay) {
				return written, c.ctx.Err()
			}
			continue
		}

		if delay > 0 {
			log.Tracef("[IO] TTY write paced for %s: byte %d/%d delayed by %v",
				c.config.ContainerID, i+1, len(data), delay)
			if !sleepWithContext(c.ctx.Done(), delay) {
				return written, c.ctx.Err()
			}
		}
	}

	return written, nil
}

func (c *Copier) lineDelayAfter(ch byte) time.Duration {
	if ch != '\n' {
		return 0
	}
	if c.config.TTYWriteLineDelay < 0 {
		return 0
	}
	return c.config.TTYWriteLineDelay
}

func sleepWithContext(ctxDone <-chan struct{}, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctxDone:
		return false
	case <-timer.C:
		return true
	}
}
