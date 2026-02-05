package io

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"time"

	log "micrun/logger"
)

// BinaryIO handles IO through an external binary process.
// This is used by containerd tools like nerdctl for pluggable logging.
type BinaryIO struct {
	cmd       *exec.Cmd
	ctx       context.Context
	container string
	uri       *url.URL

	// Pipes for communicating with the binary process
	stdinR  *os.File // Binary reads from this (connected to container stdout)
	stdinW  *os.File // We write to this
	stdoutR *os.File // We read from this (connected to binary stdout for user input)
	stdoutW *os.File // Binary writes to this
	stderrR *os.File // Binary reads from this (connected to container stderr)
	stderrW *os.File // We write to this

	cancelFunc context.CancelFunc
	closed     bool
}

// NewBinaryIO creates a new BinaryIO handler.
func NewBinaryIO(ctx context.Context, containerID string, uri *url.URL) (*BinaryIO, error) {
	// Validate URI
	if uri.Scheme != "binary" {
		return nil, fmt.Errorf("invalid URI scheme: %s (expected 'binary')", uri.Scheme)
	}

	binaryPath := uri.Path
	if binaryPath == "" {
		return nil, fmt.Errorf("empty binary path in URI: %s", uri.String())
	}

	log.Infof("[BINARY] Creating binary IO for %s: binary=%s", containerID, binaryPath)

	// Create pipes for stdout
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipes: %w", err)
	}

	// Create pipes for stderr
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		return nil, fmt.Errorf("failed to create stderr pipes: %w", err)
	}

	// Create pipes for stdin (for user input from binary to container)
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		stderrR.Close()
		stderrW.Close()
		return nil, fmt.Errorf("failed to create stdin pipes: %w", err)
	}

	// Create context with cancel for the binary process
	binCtx, cancel := context.WithCancel(ctx)

	b := &BinaryIO{
		cmd:       nil,
		ctx:       binCtx,
		container: containerID,
		uri:       uri,
		stdinR:    stdinR,  // Binary reads from here (container stdout)
		stdinW:    stdinW,  // We write here
		stdoutR:   stdoutR, // We read from here (user input from binary)
		stdoutW:   stdoutW, // Binary writes here
		stderrR:   stderrR, // Binary reads from here (container stderr)
		stderrW:   stderrW, // We write here
		cancelFunc: cancel,
		closed:     false,
	}

	// Prepare command
	cmd := exec.CommandContext(binCtx, binaryPath)
	cmd.ExtraFiles = []*os.File{stdinR, stderrR, stdoutW}
	cmd.Env = append(os.Environ(), uri.Query().Encode())

	// Start the binary process
	log.Infof("[BINARY] Starting binary process: %s", binaryPath)
	if err := cmd.Start(); err != nil {
		b.Close()
		return nil, fmt.Errorf("failed to start binary process %s: %w", binaryPath, err)
	}

	b.cmd = cmd

	// Close the write end of the stdin pipe (we're the reader)
	stdinW.Close()

	log.Infof("[BINARY] Binary IO started for %s: pid=%d", containerID, cmd.Process.Pid)

	return b, nil
}

// Stdout returns the reader for container stdout (write to this to send data to binary)
func (b *BinaryIO) Stdout() io.Writer {
	return b.stdinW
}

// Stderr returns the writer for container stderr (write to this to send data to binary)
func (b *BinaryIO) Stderr() io.Writer {
	return b.stderrW
}

// Stdin returns the reader for user input (read from this to get data from binary)
func (b *BinaryIO) Stdin() io.Reader {
	return b.stdoutR
}

// Close stops the binary IO process.
func (b *BinaryIO) Close() error {
	if b.closed {
		return nil
	}
	b.closed = true

	log.Infof("[BINARY] Closing binary IO for %s", b.container)

	var errs []error

	// Close pipes
	if b.stdinW != nil {
		if err := b.stdinW.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close stdin write pipe: %w", err))
		}
	}
	if b.stderrW != nil {
		if err := b.stderrW.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close stderr write pipe: %w", err))
		}
	}
	if b.stdoutR != nil {
		if err := b.stdoutR.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close stdout read pipe: %w", err))
		}
	}

	// Cancel context to stop the binary process
	b.cancelFunc()

	// Wait for binary process to exit (with timeout)
	if b.cmd != nil && b.cmd.Process != nil {
		done := make(chan error, 1)
		go func() {
			done <- b.cmd.Wait()
		}()

		select {
		case err := <-done:
			if err != nil {
				log.Debugf("[BINARY] Binary process exited with error: %v", err)
			} else {
				log.Infof("[BINARY] Binary process exited cleanly")
			}
		case <-time.After(5 * time.Second):
			log.Warnf("[BINARY] Binary process did not exit within 5s, killing")
			if err := b.cmd.Process.Kill(); err != nil {
				errs = append(errs, fmt.Errorf("failed to kill binary process: %w", err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing binary IO: %v", errs)
	}
	return nil
}

// IsBinaryURI checks if a string is a binary:// URI
func IsBinaryURI(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "binary"
}
