package io

import (
	"io"

	"micrun/internal/support/logger"
)

type outputWriteDecision int

const (
	outputWriteStop outputWriteDecision = iota
	outputWriteContinue
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
	default:
		return "unknown"
	}
}

func (c *Copier) writeOutputFIFO(stream string, fifo io.Writer, data []byte) outputWriteDecision {
	attempt := writeOutputFIFOData(stream, fifo, data)
	return c.applyOutputWritePolicy(attempt)
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

	written, err := fifo.Write(data)
	attempt.written = written
	attempt.err = err
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
	log.Infof("[IO] %s FIFO closed/no reader for %s (Non-TTY detached mode), discarding %d bytes and continuing", attempt.stream, c.config.ContainerID, attempt.dataLen)
	return outputWriteContinue
}

func (c *Copier) applyAgainOutputPolicy(attempt outputWriteAttempt) outputWriteDecision {
	log.Warnf("[IO] %s FIFO write EAGAIN for %s, reader not ready (wrote %d/%d bytes)", attempt.stream, c.config.ContainerID, attempt.written, attempt.dataLen)
	if c.config.Terminal {
		c.publishEvent(IOError, attempt.err)
		return outputWriteStop
	}
	return outputWriteContinue
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
	if isClosed(a.err) || isBrokenPipe(a.err) {
		return outputWriteClosed
	}
	if isEAGAIN(a.err) {
		return outputWriteAgain
	}
	return outputWriteOther
}
