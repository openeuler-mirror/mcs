// Package io provides the IO system for micrun container runtime.
// It handles bidirectional data copying between FIFOs and TTY for RTOS containers.
package io

import (
	"context"
	"io"
	"time"
)

const (
	defaultTTYWriteDelay     = 20 * time.Millisecond
	defaultTTYWriteLineDelay = 300 * time.Millisecond
)

// Config holds the configuration for an IO session.
type Config struct {
	// Context is the parent lifecycle for the session and copier. Nil uses
	// context.Background.
	Context context.Context

	// ContainerID is the ID of the container
	ContainerID string

	// FIFO paths (from containerd)
	StdinFIFO  string
	StdoutFIFO string
	StderrFIFO string

	// TTY interfaces (from RPMSG device)
	TTYIn  io.WriteCloser // stdin to TTY
	TTYOut io.Reader      // stdout from TTY
	TTYErr io.Reader      // stderr from TTY (optional)

	// Terminal mode
	Terminal bool

	// Buffer sizes
	StdinBufSize  int
	StdoutBufSize int

	// TTYWriteDelay throttles stdin writes to the RPMSG TTY. Some RTOS shells
	// drop bytes when a nested terminal forwards a large paste as a burst.
	TTYWriteDelay time.Duration

	// TTYWriteLineDelay gives the RTOS shell time to process a submitted
	// command before the next pasted line is forwarded.
	TTYWriteLineDelay time.Duration

	// Event bus for publishing IO events (decouples IO layer from shim layer)
	EventBus *EventBus

	// Enable NUL byte filtering (RTOS sends NUL bytes)
	FilterNUL bool

	// ExecMode indicates this is an exec session (not the main container)
	// In exec mode, detach sequence is not detected.
	ExecMode bool

	// DetachKeys is the key sequence for detaching from a terminal container
	// (for example, "ctrl-p,ctrl-q"). Empty string uses the default sequence.
	// Detach detection is disabled for non-terminal and exec sessions.
	DetachKeys string
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		StdinBufSize:      4 * 1024,
		StdoutBufSize:     32 * 1024,
		TTYWriteDelay:     defaultTTYWriteDelay,
		TTYWriteLineDelay: defaultTTYWriteLineDelay,
		Terminal:          true,
		FilterNUL:         true,
	}
}

func normalizeConfig(config Config) Config {
	defaults := DefaultConfig()
	if config.StdinBufSize == 0 {
		config.StdinBufSize = defaults.StdinBufSize
	}
	if config.StdoutBufSize == 0 {
		config.StdoutBufSize = defaults.StdoutBufSize
	}
	if config.TTYWriteDelay == 0 {
		config.TTYWriteDelay = defaults.TTYWriteDelay
	}
	if config.TTYWriteLineDelay == 0 {
		config.TTYWriteLineDelay = defaults.TTYWriteLineDelay
	}
	return config
}

// CopyStats tracks statistics for data copying.
type CopyStats struct {
	StdinBytes  int64
	StdoutBytes int64
	StderrBytes int64
}
