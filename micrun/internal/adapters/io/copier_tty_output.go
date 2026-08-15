package io

import (
	"io"

	"micrun/internal/domain/console"
	log "micrun/internal/support/logger"
)

type ttyReadDecision int

const (
	ttyReadStop ttyReadDecision = iota
	ttyReadContinue
)

func (c *Copier) handleTTYReadError(source string, err error) ttyReadDecision {
	if isClosed(err) || err == io.EOF {
		log.Infof("[IO] %s closed for %s", source, c.config.ContainerID)
		return ttyReadStop
	}
	if isEAGAIN(err) {
		return ttyReadContinue
	}
	if isEINTR(err) {
		return ttyReadContinue
	}
	log.Errorf("[IO] %s read error for %s: %v", source, c.config.ContainerID, err)
	c.publishEvent(IOError, err)
	return ttyReadStop
}

func (c *Copier) publishTTYReadyOnce() {
	if !c.ttyReadyPublished.CompareAndSwap(false, true) {
		return
	}
	log.Infof("[IO] TTY ready (first byte received) for %s", c.config.ContainerID)
	c.publishEvent(TTYReady, nil)
}

func (c *Copier) waitForTTYRead(waiter *epollWaiter, source ttyReadSource, loopName string) bool {
	select {
	case <-c.ctx.Done():
		log.Infof("[IO] %s canceled for %s", loopName, c.config.ContainerID)
		return false
	default:
	}

	if !c.waitForData(waiter, source.fd) {
		log.Infof("[IO] %s: waitForData returned false (context canceled) for %s", loopName, c.config.ContainerID)
		return false
	}
	return true
}

func (c *Copier) outputWriteCanceled(loopName string) bool {
	select {
	case <-c.ctx.Done():
		log.Infof("[IO] %s canceled before FIFO write for %s", loopName, c.config.ContainerID)
		return true
	default:
		return false
	}
}

func (c *Copier) normalizeTTYOutput(normalizer *console.OutputNormalizer, data []byte, suppressEcho bool) []byte {
	normalized := normalizer.Normalize(data)
	if suppressEcho {
		return c.suppressRTOSEcho(normalized)
	}
	return normalized
}

// flushNormalizer drains any byte the normalizer is holding (e.g. a trailing
// bare CR) and writes it to whichever FIFO the loop was feeding. Called on
// loop exit so the final partial byte is not lost.
func (c *Copier) flushNormalizer(config ttyOutputLoopConfig) {
	if config.normalizer == nil {
		return
	}
	remaining := config.normalizer.Flush()
	if len(remaining) == 0 {
		return
	}
	if config.writeData != nil {
		config.writeData(remaining)
	}
}
