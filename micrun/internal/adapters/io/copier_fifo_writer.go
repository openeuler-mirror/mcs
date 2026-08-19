package io

import (
	"io"
	"time"

	"micrun/internal/support/logger"
	"sync/atomic"
)

type outputWriteDecision int

const (
	outputWriteStop outputWriteDecision = iota
	outputWriteContinue
	// outputWriteRetry: the FIFO is under transient backpressure (EAGAIN).
	// The caller must wait briefly and retry the same data — dropping it
	// would lose guest output, and killing the output worker would leave the
	// session half-dead with no recovery path.
	outputWriteRetry
)

type outputWriteFailure int

const (
	outputWriteOK outputWriteFailure = iota
	outputWriteNilFIFO
	outputWriteClosed
	outputWriteAgain
	outputWriteShort
	outputWriteOther
)

type outputWriteAttempt struct {
	stream  string
	dataLen int
	written int
	err     error
	nilFIFO bool
}

func (d outputWriteDecision) String() string {
	switch d {
	case outputWriteStop:
		return "stop"
	case outputWriteContinue:
		return "continue"
	case outputWriteRetry:
		return "retry"
	default:
		return "unknown"
	}
}

func (c *Copier) writeOutputFIFO(stream string, fifo io.Writer, data []byte) outputWriteDecision {
	// Retry EAGAIN here, resuming from the unwritten suffix: a partial
	// write followed by backpressure must not re-send the already-written
	// prefix (that would duplicate output). Bounded by context cancellation
	// so a stalled reader cannot pin the worker forever.
	remaining := data
	for {
		attempt := writeOutputFIFOData(stream, fifo, remaining)
		decision := c.applyOutputWritePolicy(attempt)
		if decision != outputWriteRetry {
			return decision
		}
		if attempt.written > 0 {
			remaining = remaining[attempt.written:]
		}
		select {
		case <-c.ctx.Done():
			return outputWriteStop
		case <-time.After(fifoWriteRetryDelay(attempt)):
		}
	}
}

func writeOutputFIFOData(stream string, fifo io.Writer, data []byte) outputWriteAttempt {
	attempt := outputWriteAttempt{
		stream:  stream,
		dataLen: len(data),
	}
	if fifo == nil {
		attempt.err = io.ErrClosedPipe
		attempt.nilFIFO = true
		return attempt
	}

	// Loop to handle short writes: io.Writer may return n < len(p) with
	// err == nil when the pipe buffer is temporarily full. Without this
	// loop the remaining bytes would be silently dropped.
	totalWritten := 0
	for totalWritten < len(data) {
		n, err := fifo.Write(data[totalWritten:])
		totalWritten += n
		if err != nil {
			attempt.written = totalWritten
			attempt.err = err
			return attempt
		}
		if n == 0 {
			// Zero-byte write with no error: cannot make progress, treat as
			// a short write to avoid an infinite loop.
			attempt.written = totalWritten
			attempt.err = io.ErrShortWrite
			return attempt
		}
	}
	attempt.written = totalWritten
	return attempt
}

func (c *Copier) applyOutputWritePolicy(attempt outputWriteAttempt) outputWriteDecision {
	switch attempt.failure() {
	case outputWriteOK:
		log.Debugf("[IO] TTY→%s: successfully wrote %d bytes to FIFO for %s", attempt.stream, attempt.dataLen, c.config.ContainerID)
		return outputWriteContinue
	case outputWriteNilFIFO:
		log.Errorf("[IO] %s FIFO is nil for %s", attempt.stream, c.config.ContainerID)
		c.publishEvent(IOError, attempt.err)
		return outputWriteStop
	case outputWriteClosed:
		return c.applyClosedOutputPolicy(attempt)
	case outputWriteAgain:
		return c.applyAgainOutputPolicy(attempt)
	case outputWriteShort:
		log.Errorf("[IO] %s short write for %s: wrote %d/%d bytes", attempt.stream, c.config.ContainerID, attempt.written, attempt.dataLen)
		c.publishEvent(IOError, io.ErrShortWrite)
		return outputWriteStop
	default:
		log.Errorf("[IO] %s write error for %s: %v", attempt.stream, c.config.ContainerID, attempt.err)
		c.publishEvent(IOError, attempt.err)
		return outputWriteStop
	}
}

func (c *Copier) applyClosedOutputPolicy(attempt outputWriteAttempt) outputWriteDecision {
	if c.config.Terminal {
		log.Infof("[IO] %s FIFO closed by client for %s (TTY mode), stopping copier and publishing disconnect event", attempt.stream, c.config.ContainerID)
		c.publishEvent(StdinClosed, nil)
		return outputWriteStop
	}
	// Non-TTY mode: the attach client's read end closed. Stop the session
	// and publish the disconnect event (same as TTY mode) so the attached
	// flag clears and auto-close can apply; a later reattach restarts the
	// session with fresh FIFOs. The container itself keeps running.
	log.Infof("[IO] %s FIFO closed/no reader for %s (Non-TTY), stopping copier (container continues)", attempt.stream, c.config.ContainerID)
	c.publishEvent(StdinClosed, nil)
	return outputWriteStop
}

func fifoWriteRetryDelay(attempt outputWriteAttempt) time.Duration {
	if isBrokenPipe(attempt.err) || isENXIO(attempt.err) {
		return outputWriteNoReaderDelay
	}
	return outputWriteRetryDelay
}

func (c *Copier) applyAgainOutputPolicy(attempt outputWriteAttempt) outputWriteDecision {
	if isBrokenPipe(attempt.err) || isENXIO(attempt.err) {
		log.Debugf("[IO] %s FIFO has no reader for %s, waiting for attach",
			attempt.stream, c.config.ContainerID)
		return outputWriteRetry
	}
	// EAGAIN means the attach client is reading too slowly (backpressure),
	// not that the reader is gone (that would be EPIPE/closed). Retry after
	// a short delay instead of dropping data or killing the output worker:
	// the latter would leave the session half-dead with no recovery path.
	if n := atomic.AddUint32(&c.eagainWarnCount, 1); n == 1 || n%100 == 0 {
		log.Warnf("[IO] %s FIFO write EAGAIN for %s, reader not ready (wrote %d/%d bytes), retrying (occurrence %d)",
			attempt.stream, c.config.ContainerID, attempt.written, attempt.dataLen, n)
	} else {
		log.Debugf("[IO] %s FIFO write EAGAIN for %s, retrying (occurrence %d)",
			attempt.stream, c.config.ContainerID, n)
	}
	return outputWriteRetry
}

func (a outputWriteAttempt) failure() outputWriteFailure {
	if a.err == nil {
		if a.written == a.dataLen {
			return outputWriteOK
		}
		return outputWriteShort
	}
	if a.nilFIFO {
		return outputWriteNilFIFO
	}
	if isClosed(a.err) {
		return outputWriteClosed
	}
	// EPIPE/ENXIO mean the attach client is not reading this FIFO. ctr
	// start -d and later `ctr task attach` reuse the same paths without a
	// new Start RPC, so the output worker must wait for the next reader
	// instead of tearing the session down. A truly closed fd is isClosed.
	if isBrokenPipe(a.err) || isENXIO(a.err) || isEAGAIN(a.err) {
		return outputWriteAgain
	}
	return outputWriteOther
}
