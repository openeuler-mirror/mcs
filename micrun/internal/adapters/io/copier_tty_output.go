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

func (c *Copier) waitForTTYRead(source ttyReadSource, loopName string) bool {
	select {
	case <-c.ctx.Done():
		log.Infof("[IO] %s canceled for %s", loopName, c.config.ContainerID)
		return false
	default:
	}

	if !c.waitForData(source.fd) {
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
