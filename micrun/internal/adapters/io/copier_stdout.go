package io

import (
	"micrun/internal/domain/console"
	"micrun/internal/support/logger"
)

type ttyOutputLoopConfig struct {
	normalizer         *console.OutputNormalizer
	suppressEcho       bool
	publishTTYReady    bool
	checkWriteCanceled bool
	errorSource        string
	logRead            func(totalRead int, n int, sample []byte)
	writeData          func([]byte) outputWriteDecision
}

func (c *Copier) copyTTYOutputLoop(source ttyReadSource, loopName string, config ttyOutputLoopConfig) {
	buf := make([]byte, c.config.StdoutBufSize)
	totalRead := 0
	for {
		if !c.waitForTTYRead(source, loopName) {
			return
		}

		n, err := source.read(buf)
		if err != nil {
			sourceName := config.errorSource
			if sourceName == "" {
				sourceName = loopName
			}
			if c.handleTTYReadError(sourceName, err) == ttyReadStop {
				return
			}
			continue
		}

		if n == 0 {
			continue
		}

		totalRead += n
		if config.publishTTYReady {
			c.publishTTYReadyOnce()
		}

		if config.logRead != nil {
			config.logRead(totalRead, n, buf[:min(n, 100)])
		}

		data := c.normalizeTTYOutput(config.normalizer, buf[:n], config.suppressEcho)
		if len(data) == 0 {
			continue
		}

		c.logPostInputOutput(len(data))

		if config.checkWriteCanceled && c.outputWriteCanceled(loopName) {
			return
		}

		if config.writeData != nil && config.writeData(data) == outputWriteStop {
			return
		}
	}
}

func (c *Copier) copyStdout() {
	defer c.wg.Done()

	log.Infof("[IO] TTY→stdout copier started for %s", c.config.ContainerID)

	normalizer := console.NewOutputNormalizer(console.OutputConfig{
		FilterNUL:           c.config.FilterNUL,
		CompressLineEndings: true,
	})

	source := newTTYReadSource("TTY→stdout", c.config.ContainerID, c.ttyOut)
	c.copyTTYOutputLoop(source, "TTY→stdout", ttyOutputLoopConfig{
		normalizer:         normalizer,
		suppressEcho:       c.suppressEcho,
		publishTTYReady:    true,
		checkWriteCanceled: true,
		errorSource:        "TTY stdout",
		logRead: func(totalRead, n int, sample []byte) {
			// Log first reads for debugging (with hex dump for diagnosis)
			if totalRead <= 500 || n < 20 {
				log.Debugf("[IO] TTY→stdout read %d bytes: %q, hex: %s", n, string(sample), hexDump(sample))
			}
		},
		writeData: func(data []byte) outputWriteDecision {
			return c.writeOutputFIFO("stdout", c.stdoutFIFO, data)
		},
	})
}

// copyStdoutErrUnified copies from a single TTY to both stdout and stderr FIFOs.
// This is used when stdout and stderr are merged (same file descriptor), which is
// common in RTOS/mica clients. Using a single reader prevents race conditions where
// two goroutines would compete to read from the same fd.

func (c *Copier) copyStdoutErrUnified() {
	defer c.wg.Done()

	log.Infof("[IO] Unified TTY→stdout+stderr copier started for %s", c.config.ContainerID)
	// Log FIFO state for debugging
	if c.stdoutFIFO == nil {
		log.Warnf("[IO] Unified: stdout FIFO is nil for %s - output will be discarded!", c.config.ContainerID)
	}
	if c.stderrFIFO == nil {
		log.Tracef("[IO] Unified: stderr FIFO is nil for %s (this is OK if merged with stdout)", c.config.ContainerID)
	} else {
		log.Debugf("[IO] Unified: stderr FIFO is set for %s but will NOT be written (unified mode only writes stdout to avoid duplicate output)", c.config.ContainerID)
	}

	normalizer := console.NewOutputNormalizer(console.OutputConfig{
		FilterNUL:           c.config.FilterNUL,
		CompressLineEndings: c.config.Terminal,
	})

	source := newTTYReadSource("Unified copier", c.config.ContainerID, c.ttyOut)
	c.copyTTYOutputLoop(source, "Unified TTY", ttyOutputLoopConfig{
		normalizer:         normalizer,
		suppressEcho:       c.suppressEcho,
		publishTTYReady:    true,
		checkWriteCanceled: false,
		errorSource:        "Unified TTY",
		logRead: func(totalRead, n int, sample []byte) {
			// Log first reads for debugging
			if totalRead <= 500 || n < 20 {
				log.Debugf("[IO] Unified TTY read %d bytes: %q for %s", n, string(sample), c.config.ContainerID)
			}
		},
		writeData: func(data []byte) outputWriteDecision {
			if c.stdoutFIFO != nil {
				return c.writeOutputFIFO("stdout", c.stdoutFIFO, data)
			}
			return outputWriteContinue
		},
	})
	// NOTE: In unified copier mode, we only write to stdout FIFO.
	// We skip writing to stderr FIFO because:
	// 1. The data source is the same TTY for both stdout and stderr
	// 2. Writing to both FIFOs causes duplicate output when client reads both
	// 3. Clients that need separate stdout/stderr should not use unified mode
	// The stderr FIFO is still opened to prevent blocking on containerd side,
	// but we don't write data to it.
}

// copyStderr copies from TTY to stderr FIFO.

func (c *Copier) copyStderr() {
	defer c.wg.Done()

	normalizer := console.NewOutputNormalizer(console.OutputConfig{FilterNUL: c.config.FilterNUL})

	log.Infof("[IO] TTY→stderr copier started for %s", c.config.ContainerID)

	source := newTTYReadSource("TTY→stderr", c.config.ContainerID, c.ttyErr)
	c.copyTTYOutputLoop(source, "TTY→stderr copier", ttyOutputLoopConfig{
		normalizer:         normalizer,
		suppressEcho:       false,
		publishTTYReady:    false,
		checkWriteCanceled: false,
		errorSource:        "TTY stderr",
		writeData: func(data []byte) outputWriteDecision {
			return c.writeOutputFIFO("stderr", c.stderrFIFO, data)
		},
	})
}

func (c *Copier) suppressRTOSEcho(data []byte) []byte {
	result := c.echoSuppressor.Suppress(data)
	if result.LostSync {
		expectedByte := "out of range"
		if result.ExpectedValid {
			expectedByte = string([]byte{result.Expected})
		}
		log.Tracef("[IO] Echo suppression lost sync at pos %d, resetting (expected %s, got %d (%q))",
			result.Position, expectedByte, result.Got, result.Got)
	}
	if result.Suppressed > 0 {
		log.Tracef("[IO] Suppressed %d echoed characters from RTOS", result.Suppressed)
	}
	return result.Data
}
