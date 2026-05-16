package io

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"micrun/internal/support/contextx"
	log "micrun/internal/support/logger"
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
	closeOnce  sync.Once
	closeErr   error
}

type binaryPipes struct {
	stdinR  *os.File
	stdinW  *os.File
	stdoutR *os.File
	stdoutW *os.File
	stderrR *os.File
	stderrW *os.File
}

// NewBinaryIO creates a new BinaryIO handler.
func NewBinaryIO(ctx context.Context, containerID string, uri *url.URL) (*BinaryIO, error) {
	ctx = contextx.OrBackground(ctx)
	if uri == nil {
		return nil, fmt.Errorf("binary URI is required")
	}
	// Validate URI
	if uri.Scheme != "binary" {
		return nil, fmt.Errorf("invalid URI scheme: %s (expected 'binary')", uri.Scheme)
	}

	binaryPath := uri.Path
	if binaryPath == "" {
		return nil, fmt.Errorf("empty binary path in URI: %s", uri.String())
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	log.Infof("[BINARY] Creating binary IO for %s: binary=%s", containerID, binaryPath)

	pipes, err := newBinaryPipes()
	if err != nil {
		return nil, err
	}

	// Create context with cancel for the binary process
	binCtx, cancel := context.WithCancel(ctx)

	b := &BinaryIO{
		cmd:        nil,
		ctx:        binCtx,
		container:  containerID,
		uri:        uri,
		stdinR:     pipes.stdinR,  // Binary reads from here (container stdout)
		stdinW:     pipes.stdinW,  // We write here
		stdoutR:    pipes.stdoutR, // We read from here (user input from binary)
		stdoutW:    pipes.stdoutW, // Binary writes here
		stderrR:    pipes.stderrR, // Binary reads from here (container stderr)
		stderrW:    pipes.stderrW, // We write here
		cancelFunc: cancel,
	}

	// Prepare command
	cmd := exec.CommandContext(binCtx, binaryPath)
	cmd.ExtraFiles = []*os.File{pipes.stdinR, pipes.stderrR, pipes.stdoutW}
	cmd.Env = binaryEnv(os.Environ(), uri.Query())

	// Start the binary process
	log.Infof("[BINARY] Starting binary process: %s", binaryPath)
	if err := cmd.Start(); err != nil {
		b.Close()
		return nil, fmt.Errorf("failed to start binary process %s: %w", binaryPath, err)
	}

	b.cmd = cmd

	// Close child-side pipe ends in the parent after the child inherits them.
	b.closePipe("binary stdin read pipe", &b.stdinR)
	b.closePipe("binary stderr read pipe", &b.stderrR)
	b.closePipe("binary stdout write pipe", &b.stdoutW)

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
	if b == nil {
		return nil
	}

	b.closeOnce.Do(func() {
		log.Infof("[BINARY] Closing binary IO for %s", b.container)

		errs := mergeCloseErrors(
			b.closePipe("stdin read pipe", &b.stdinR),
			b.closePipe("stdin write pipe", &b.stdinW),
			b.closePipe("stderr read pipe", &b.stderrR),
			b.closePipe("stderr write pipe", &b.stderrW),
			b.closePipe("stdout read pipe", &b.stdoutR),
			b.closePipe("stdout write pipe", &b.stdoutW),
		)

		// Cancel context to stop the binary process
		b.cancelFunc()

		// Wait for binary process to exit (with timeout)
		if b.cmd != nil && b.cmd.Process != nil {
			done := make(chan error, 1)
			go func() {
				done <- b.cmd.Wait()
			}()

			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case err := <-done:
				if err != nil {
					log.Debugf("[BINARY] Binary process exited with error: %v", err)
				} else {
					log.Infof("[BINARY] Binary process exited cleanly")
				}
			case <-timer.C:
				log.Warnf("[BINARY] Binary process did not exit within 5s, killing")
				if err := b.cmd.Process.Kill(); err != nil {
					errs = mergeCloseErrors(errs, []error{fmt.Errorf("failed to kill binary process: %w", err)})
				}
			}
		}

		if err := joinCloseErrors(errs); err != nil {
			b.closeErr = fmt.Errorf("errors closing binary IO: %w", err)
			return
		}
		b.closeErr = nil
	})

	return b.closeErr
}

func (b *BinaryIO) closePipe(name string, file **os.File) []error {
	return closeStreamIfCloseable(name, file, func(err error) bool {
		return errors.Is(err, os.ErrClosed)
	})
}

func newBinaryPipes() (*binaryPipes, error) {
	pipes := &binaryPipes{}

	var err error
	pipes.stdinR, pipes.stdinW, err = os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	pipes.stdoutR, pipes.stdoutW, err = os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", errors.Join(err, pipes.close()))
	}

	pipes.stderrR, pipes.stderrW, err = os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", errors.Join(err, pipes.close()))
	}

	return pipes, nil
}

func (pipes *binaryPipes) close() error {
	return joinCloseErrors(
		closeStreamIfCloseable("binary stdin read pipe", &pipes.stdinR, func(err error) bool {
			return errors.Is(err, os.ErrClosed)
		}),
		closeStreamIfCloseable("binary stdin write pipe", &pipes.stdinW, func(err error) bool {
			return errors.Is(err, os.ErrClosed)
		}),
		closeStreamIfCloseable("binary stdout read pipe", &pipes.stdoutR, func(err error) bool {
			return errors.Is(err, os.ErrClosed)
		}),
		closeStreamIfCloseable("binary stdout write pipe", &pipes.stdoutW, func(err error) bool {
			return errors.Is(err, os.ErrClosed)
		}),
		closeStreamIfCloseable("binary stderr read pipe", &pipes.stderrR, func(err error) bool {
			return errors.Is(err, os.ErrClosed)
		}),
		closeStreamIfCloseable("binary stderr write pipe", &pipes.stderrW, func(err error) bool {
			return errors.Is(err, os.ErrClosed)
		}),
	)
}

func binaryEnv(base []string, query url.Values) []string {
	env := append([]string{}, base...)
	if len(query) == 0 {
		return env
	}

	overrides := binaryEnvOverrides(query)
	if len(overrides) == 0 {
		return env
	}

	for i, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if value, exists := overrides[key]; exists {
			env[i] = key + "=" + value
			delete(overrides, key)
		}
	}

	for _, key := range sortedEnvKeys(overrides) {
		env = append(env, key+"="+overrides[key])
	}
	return env
}

func binaryEnvOverrides(query url.Values) map[string]string {
	keys := make([]string, 0, len(query))
	for key := range query {
		if key == "" || strings.Contains(key, "=") {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	overrides := make(map[string]string, len(keys))
	for _, key := range keys {
		values := query[key]
		if len(values) == 0 {
			continue
		}
		overrides[key] = values[len(values)-1]
	}
	return overrides
}

func sortedEnvKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// IsBinaryURI checks if a string is a binary:// URI
func IsBinaryURI(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "binary"
}
