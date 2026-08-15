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
			if isEINTR(err) {
				continue
			}
			if isEAGAIN(err) {
				// EAGAIN on this FIFO is a live writer with no data. start -d
				// has no writer and returns EOF. After create-time EOF,
				// nerdctl -i / attach opens the write end and the next read
				// is EAGAIN — that client must block auto-close before the
				// first byte. Writer-close returns EOF again (not EAGAIN),
				// so this cannot busy-loop on HUP.
				if c.stdinEOFSeen {
					c.stdinEOFSeen = false
					c.attachClientConnected = true
				}
				c.noteLiveClient()
				if !c.waitForStdinOrCancel(c.stdinFIFOFD()) {
					return
				}
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

// stdinReattachPollInterval is how often the stdin copier polls for a
// reattach writer after the previous writer closed (epoll would busy-loop
// on the continuous HUP).
const stdinReattachPollInterval = 100 * time.Millisecond

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

		// Keep the session: start -d may open and close stdin once, and
		// `ctr task attach` reuses the same FIFOs with no second Start.
		// Stopping here leaves attach blocked on a stdout FIFO with no writer.
		// Clear the live-client CAS so the next writer can publish
		// ClientAttached again. CloseIO already cleared attached; without
		// this, auto-close would kill a second attach that never calls Start.
		log.Infof("[IO] stdin EOF for %s (non-TTY attach closed stdin, keeping stdout open for reattach)", c.config.ContainerID)
		c.attachClientConnected = false
		c.stdinEOFSeen = true
		c.noteLiveClientGone()
		if !c.waitForStdinReattach() {
			return stdinLoopStop
		}
		return stdinLoopContinue
	}

	if !c.stdinEOFSeen {
		log.Infof("[IO] stdin EOF for %s (no attach client yet, waiting)", c.config.ContainerID)
		c.stdinEOFSeen = true
		// start -d: StartInitialSession marked attached for nerdctl -i;
		// no writer means that was not a live client.
		c.publishEvent(ClientDetached, nil)
	}
	if !c.waitForStdinReattach() {
		return stdinLoopStop
	}
	return stdinLoopContinue
}

// waitForStdinReattach waits for a reattach writer on a timer instead of
// epoll: after a writer opened and closed the FIFO, the read end reports
// HUP continuously and an epoll wait returns immediately, which would busy-
// loop at 100% CPU until a new writer connects. Polling every
// stdinReattachPollInterval keeps the wait cheap; the caller re-reads and
// detects actual data (EAGAIN alone is not a writer signal).
func (c *Copier) waitForStdinReattach() bool {
	timer := time.NewTimer(stdinReattachPollInterval)
	defer timer.Stop()
	select {
	case <-c.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *Copier) handleStdinEAGAIN() {
	// No longer used for EOF-seen waiting (see waitForStdinReattach):
	// EAGAIN after EOF does NOT mean a writer appeared — only actual data
	// does. Kept as a no-op guard so callers cannot reintroduce the busy
	// loop.
}

func (c *Copier) markStdinDataReceived() {
	if c.stdinEOFSeen {
		log.Infof("[IO] stdin data received for %s (writer connected)", c.config.ContainerID)
		c.stdinEOFSeen = false
		if !c.attachClientConnected {
			log.Infof("[IO] Attach client connected for %s", c.config.ContainerID)
			c.attachClientConnected = true
			c.noteLiveClient()
		}
		c.reenableStdinEpoll()
		return
	}

	if !c.attachClientConnected {
		log.Infof("[IO] First data received for %s (attach client connected)", c.config.ContainerID)
		c.attachClientConnected = true
		c.noteLiveClient()
		c.reenableStdinEpoll()
	}
}

func (c *Copier) noteLiveClient() {
	if c == nil || !c.liveClientPublished.CompareAndSwap(false, true) {
		return
	}
	c.publishEvent(ClientAttached, nil)
}

func (c *Copier) noteLiveClientGone() {
	if c == nil || !c.liveClientPublished.CompareAndSwap(true, false) {
		return
	}
	c.publishEvent(ClientDetached, nil)
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
	for i := 0; i < len(data); {
		select {
		case <-c.ctx.Done():
			return written, c.ctx.Err()
		default:
		}

		// Keep CRLF atomic. A 20ms gap between CR and LF lets the guest
		// "skip next" window expire; the delayed LF then eats the first
		// byte of the next command (help → elp).
		chunk := data[i : i+1]
		if data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n' {
			chunk = data[i : i+2]
		}

		n, err := c.ttyIn.Write(chunk)
		written += n
		if err != nil {
			if isEAGAIN(err) {
				select {
				case <-c.ctx.Done():
					return written, c.ctx.Err()
				case <-time.After(outputWriteRetryDelay):
				}
				continue
			}
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
		if n < len(chunk) {
			i += n
			continue
		}
		i += n

		ch := chunk[len(chunk)-1]
		if lineDelay := c.lineDelayAfter(ch); lineDelay > 0 {
			log.Tracef("[IO] TTY write line-paced for %s: delayed by %v",
				c.config.ContainerID, lineDelay)
			if !sleepWithContext(c.ctx.Done(), lineDelay) {
				return written, c.ctx.Err()
			}
			continue
		}

		if delay > 0 {
			log.Tracef("[IO] TTY write paced for %s: delayed by %v",
				c.config.ContainerID, delay)
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
