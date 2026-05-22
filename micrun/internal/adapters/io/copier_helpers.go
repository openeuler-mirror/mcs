package io

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"micrun/internal/support/logger"
)

func boolToInt(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

func shouldMarkPostInputOutput(inputGeneration, observedGeneration uint64, outputLen int) bool {
	if outputLen <= 0 {
		return false
	}
	if inputGeneration == 0 {
		return false
	}
	return inputGeneration != observedGeneration
}

func resetPointer[T any](target *T) {
	var zero T
	*target = zero
}

func logNonTTYInputActivity(containerID string, data []byte) {
	if log.Log.IsLevelEnabled(logrus.DebugLevel) {
		log.Debugf("[IO] Non-TTY: received data for %s: %q (%d bytes)", containerID, string(data), len(data))
		log.Tracef("[IO] Non-TTY: hex data for %s: %s", containerID, hexDump(data))
		return
	}

	log.Infof("[IO] Non-TTY stdin activity for %s (%d bytes)", containerID, len(data))
}

// copyStdout copies from TTY to stdout FIFO.

func isClosed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EBADF) {
		return true
	}
	if isEAGAIN(err) {
		return false
	}
	return errorTextContains(err, "closed", "use of closed")
}

func isBrokenPipe(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EPIPE) {
		return true
	}
	return errorTextContains(err, "broken pipe", "EPIPE")
}

func isEAGAIN(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
		return true
	}
	return errorTextContains(err, "EAGAIN", "resource temporarily unavailable")
}

func errorTextContains(err error, fragments ...string) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	for _, fragment := range fragments {
		if strings.Contains(text, fragment) {
			return true
		}
	}
	return false
}

// hexDump converts a byte slice to a hexadecimal string representation.

func hexDump(data []byte) string {
	var b strings.Builder
	b.Grow(len(data) * 4)
	for _, d := range data {
		fmt.Fprintf(&b, "\\x%02x", d)
	}
	return b.String()
}

// suppressRTOSEcho suppresses echo from RTOS to avoid double echo.
// In TTY mode, the PTY already echoes locally, so we filter out RTOS echo.
// This works by:
// 1. Checking if received characters match what we recently sent to TTY
// 2. If they match, they're likely RTOS echo - suppress them
// 3. If they don't match, they're RTOS output - keep them
//
// This is a best-effort heuristic that works for most cases.
// It may suppress legitimate output in rare cases (e.g., if RTOS outputs
