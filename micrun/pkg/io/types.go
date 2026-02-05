// Package io provides the IO system for micrun container runtime.
// It handles bidirectional data copying between FIFOs and TTY for RTOS containers.
package io

import (
	"io"
)

// Config holds the configuration for an IO session.
type Config struct {
	// ContainerID is the ID of the container
	ContainerID string

	// FIFO paths (from containerd)
	StdinFIFO  string
	StdoutFIFO string
	StderrFIFO string

	// TTY interfaces (from RPMSG device)
	TTYIn  io.WriteCloser // stdin to TTY
	TTYOut io.Reader     // stdout from TTY
	TTYErr io.Reader     // stderr from TTY (optional)

	// Terminal mode
	Terminal bool

	// Buffer sizes
	StdinBufSize  int
	StdoutBufSize int

	// Event bus for publishing IO events (decouples IO layer from shim layer)
	EventBus *EventBus

	// Enable NUL byte filtering (RTOS sends NUL bytes)
	FilterNUL bool

	// ExecMode indicates this is an exec session (not the main container)
	// In exec mode, detach sequence is not detected.
	ExecMode bool

	// DetachKeys is the key sequence for detaching from a container (e.g., "ctrl-p,ctrl-q")
	// Empty string disables detach detection.
	DetachKeys string
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		StdinBufSize:  4 * 1024,
		StdoutBufSize: 32 * 1024,
		Terminal:      true,
		FilterNUL:     true,
	}
}

// CopyStats tracks statistics for data copying.
type CopyStats struct {
	StdinBytes  int64
	StdoutBytes int64
	StderrBytes int64
}
