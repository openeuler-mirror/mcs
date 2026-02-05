package io

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	log "micrun/logger"

	"github.com/containerd/fifo"
)

// Session manages an IO session for a container.
// It handles FIFO creation, opening, and data copying.
type Session struct {
	config  Config
	copier  *Copier
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	started bool
}

// NewSession creates a new IO session.
func NewSession(config Config) (*Session, error) {
	if config.ContainerID == "" {
		return nil, fmt.Errorf("container ID is required")
	}

	// Apply defaults
	if config.StdinBufSize == 0 {
		config.StdinBufSize = 4 * 1024
	}
	if config.StdoutBufSize == 0 {
		config.StdoutBufSize = 32 * 1024
	}

	ctx, cancel := context.WithCancel(context.Background())

	s := &Session{
		config: config,
		ctx:    ctx,
		cancel: cancel,
	}

	// Create copier
	s.copier = NewCopier(config)
	s.copier.SetTTYs(config.TTYIn, config.TTYOut, config.TTYErr)

	return s, nil
}

// GetCopier returns the copier for setting callbacks.
func (s *Session) GetCopier() *Copier {
	return s.copier
}

// IsValidFIFOPath checks if a path is a valid FIFO path (not a URL or empty string).
func IsValidFIFOPath(path string) bool {
	if path == "" {
		return false
	}
	// Skip non-file paths (like binary:// URLs, fd:// URLs, etc.)
	if len(path) >= 9 && path[:9] == "binary://" {
		return false
	}
	if len(path) >= 5 && path[:5] == "fd://" {
		return false
	}
	if len(path) >= 8 && path[:8] == "socket://" {
		return false
	}
	return true
}

// GenerateStandardFIFOPath generates the standard containerd FIFO path for a container.
// Format: /run/containerd/io.containerd.runtime.v2.task/<namespace>/<container-id>/<stream>
// where stream is "stdin", "stdout", or "stderr".
func GenerateStandardFIFOPath(namespace, containerID, stream string) string {
	return filepath.Join("/run/containerd/io.containerd.runtime.v2.task", namespace, containerID, stream)
}

// Start creates FIFOs, opens them, and starts the copier.
func (s *Session) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("already started")
	}

	// Ensure FIFOs exist
	if err := s.ensureFIFOs(); err != nil {
		return fmt.Errorf("failed to create FIFOs: %w", err)
	}

	// Open FIFOs
	var stdinFifo io.ReadCloser
	var stdoutFifo io.WriteCloser
	var stderrFifo io.WriteCloser
	var err error

	// Open stdin (read-only, non-blocking)
	if s.config.StdinFIFO != "" && IsValidFIFOPath(s.config.StdinFIFO) {
		stdinFifo, err = fifo.OpenFifo(s.ctx, s.config.StdinFIFO,
			syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return fmt.Errorf("failed to open stdin FIFO: %w", err)
		}
	} else if s.config.StdinFIFO != "" && !IsValidFIFOPath(s.config.StdinFIFO) {
		log.Debugf("[SESSION] Skipping stdin FIFO (non-file path): %s", s.config.StdinFIFO)
	}

	// Open stdout (write-only, non-blocking for unblock on close)
	// Use O_WRONLY (not O_RDWR) so that when shim closes the FIFO,
	// the reader (nerdctl attach) immediately receives EOF and exits
	if s.config.StdoutFIFO != "" && IsValidFIFOPath(s.config.StdoutFIFO) {
		log.Infof("[SESSION] Opening stdout FIFO %s in O_WRONLY|O_NONBLOCK mode for %s", s.config.StdoutFIFO, s.config.ContainerID)
		stdoutFifo, err = fifo.OpenFifo(s.ctx, s.config.StdoutFIFO,
			syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			log.Warnf("[SESSION] Failed to open stdout FIFO %s: %v", s.config.StdoutFIFO, err)
			if stdinFifo != nil {
				stdinFifo.Close()
			}
			return fmt.Errorf("failed to open stdout FIFO: %w", err)
		}
		log.Infof("[SESSION] Successfully opened stdout FIFO %s for %s", s.config.StdoutFIFO, s.config.ContainerID)
	} else if s.config.StdoutFIFO != "" && !IsValidFIFOPath(s.config.StdoutFIFO) {
		log.Debugf("[SESSION] Skipping stdout FIFO (non-file path): %s", s.config.StdoutFIFO)
	}

	// Open stderr if provided (write-only for same reason as stdout)
	if s.config.StderrFIFO != "" && IsValidFIFOPath(s.config.StderrFIFO) {
		log.Infof("[SESSION] Opening stderr FIFO %s in O_WRONLY|O_NONBLOCK mode for %s", s.config.StderrFIFO, s.config.ContainerID)
		stderrFifo, err = fifo.OpenFifo(s.ctx, s.config.StderrFIFO,
			syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			log.Warnf("[SESSION] Failed to open stderr FIFO %s: %v", s.config.StderrFIFO, err)
			if stdinFifo != nil {
				stdinFifo.Close()
			}
			if stdoutFifo != nil {
				stdoutFifo.Close()
			}
			return fmt.Errorf("failed to open stderr FIFO: %w", err)
		}
		log.Infof("[SESSION] Successfully opened stderr FIFO %s for %s", s.config.StderrFIFO, s.config.ContainerID)
	} else if s.config.StderrFIFO != "" && !IsValidFIFOPath(s.config.StderrFIFO) {
		log.Debugf("[SESSION] Skipping stderr FIFO (non-file path): %s", s.config.StderrFIFO)
	}

	// Set FIFOs in copier
	s.copier.SetStdin(stdinFifo)
	s.copier.SetStdout(stdoutFifo)
	s.copier.SetStderr(stderrFifo)

	// Set stdout FIFO for local echo in TTY mode (for exit/detach command detection)
	if s.config.Terminal && stdoutFifo != nil {
		log.Infof("[SESSION] Setting stdoutFifoForEcho for %s (Terminal=true)", s.config.ContainerID)
		s.copier.SetStdoutFifoForEcho(stdoutFifo)
	} else {
		log.Infof("[SESSION] NOT setting stdoutFifoForEcho for %s (Terminal=%v, stdoutFifo=%v)", s.config.ContainerID, s.config.Terminal, stdoutFifo != nil)
	}

	// Start copier
	if err := s.copier.Start(); err != nil {
		return fmt.Errorf("failed to start copier: %w", err)
	}

	s.started = true
	log.Infof("[SESSION] Started IO session for %s", s.config.ContainerID)

	return nil
}

// Stop stops the IO session.
func (s *Session) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		log.Debugf("[SESSION] Stop called but session not started for %s", s.config.ContainerID)
		return
	}

	log.Infof("[SESSION] Stopping IO session for %s (started was %v)", s.config.ContainerID, s.started)

	// Stop copier
	s.copier.Stop()

	// Cancel context
	s.cancel()

	// NOTE: Do NOT close the event bus here!
	// The event bus must persist across attach/detach cycles.
	// It will be closed when the container is deleted, not when IO session stops.
	// Closing the event bus here causes the event handler to exit,
	// and subsequent restarts won't have any event handlers listening.

	s.started = false
}

// StopWithoutClosingFIFOs stops the IO session but keeps FIFOs open for reattach.
// This is used when the client detaches but the container continues running.
func (s *Session) StopWithoutClosingFIFOs() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return
	}

	log.Debugf("[SESSION] Stopping IO copier for %s (keeping FIFOs for reattach)", s.config.ContainerID)

	// Stop copier WITHOUT closing FIFOs
	s.copier.StopWithoutClosingFIFOs()

	// Cancel context
	s.cancel()

	// Note: We don't close the event bus to allow reconnection
	// The event bus will be closed when the container is deleted

	s.started = false
}

// Restart restarts the IO session by reopening FIFOs and starting a new copier.
// This allows reattaching to a running container after detach.
func (s *Session) Restart() error {
	return s.RestartWithTTYs(nil, nil)
}

// RestartWithTTYs restarts the IO session with fresh TTY handles.
// This is the preferred method for reattach scenarios where the original TTY
// handles may be closed. If freshTTYIn or freshTTYOut are provided, they will
// be used instead of the handles from s.config.
func (s *Session) RestartWithTTYs(freshTTYIn io.WriteCloser, freshTTYOut io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("already started")
	}

	log.Infof("[SESSION] Restarting IO session for %s", s.config.ContainerID)

	// Create a new context
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	s.cancel = cancel

	// Open FIFOs
	var stdinFifo io.ReadCloser
	var stdoutFifo io.WriteCloser
	var stderrFifo io.WriteCloser
	var err error

	// Open stdin (read-only, non-blocking)
	if s.config.StdinFIFO != "" && IsValidFIFOPath(s.config.StdinFIFO) {
		stdinFifo, err = fifo.OpenFifo(s.ctx, s.config.StdinFIFO,
			syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return fmt.Errorf("failed to open stdin FIFO: %w", err)
		}
	} else if s.config.StdinFIFO != "" && !IsValidFIFOPath(s.config.StdinFIFO) {
		log.Debugf("[SESSION] Skipping stdin FIFO (non-file path): %s", s.config.StdinFIFO)
	}

	// Open stdout (write-only, non-blocking for unblock on close)
	// Use O_WRONLY (not O_RDWR) so that when shim closes the FIFO,
	// the reader (nerdctl attach) immediately receives EOF and exits
	if s.config.StdoutFIFO != "" && IsValidFIFOPath(s.config.StdoutFIFO) {
		log.Infof("[SESSION] Opening stdout FIFO %s in O_WRONLY|O_NONBLOCK mode for %s", s.config.StdoutFIFO, s.config.ContainerID)
		stdoutFifo, err = fifo.OpenFifo(s.ctx, s.config.StdoutFIFO,
			syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			log.Warnf("[SESSION] Failed to open stdout FIFO %s: %v", s.config.StdoutFIFO, err)
			if stdinFifo != nil {
				stdinFifo.Close()
			}
			return fmt.Errorf("failed to open stdout FIFO: %w", err)
		}
		log.Infof("[SESSION] Successfully opened stdout FIFO %s for %s", s.config.StdoutFIFO, s.config.ContainerID)
	} else if s.config.StdoutFIFO != "" && !IsValidFIFOPath(s.config.StdoutFIFO) {
		log.Debugf("[SESSION] Skipping stdout FIFO (non-file path): %s", s.config.StdoutFIFO)
	}

	// Open stderr if provided (write-only for same reason as stdout)
	if s.config.StderrFIFO != "" && IsValidFIFOPath(s.config.StderrFIFO) {
		log.Infof("[SESSION] Opening stderr FIFO %s in O_WRONLY|O_NONBLOCK mode for %s", s.config.StderrFIFO, s.config.ContainerID)
		stderrFifo, err = fifo.OpenFifo(s.ctx, s.config.StderrFIFO,
			syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			log.Warnf("[SESSION] Failed to open stderr FIFO %s: %v", s.config.StderrFIFO, err)
			if stdinFifo != nil {
				stdinFifo.Close()
			}
			if stdoutFifo != nil {
				stdoutFifo.Close()
			}
			return fmt.Errorf("failed to open stderr FIFO: %w", err)
		}
		log.Infof("[SESSION] Successfully opened stderr FIFO %s for %s", s.config.StderrFIFO, s.config.ContainerID)
	} else if s.config.StderrFIFO != "" && !IsValidFIFOPath(s.config.StderrFIFO) {
		log.Debugf("[SESSION] Skipping stderr FIFO (non-file path): %s", s.config.StderrFIFO)
	}

	// Determine which TTY handles to use:
	// 1. Fresh TTY handles if provided (preferred for reattach)
	// 2. Otherwise fall back to existing config handles
	ttyIn := freshTTYIn
	ttyOut := freshTTYOut
	ttyErr := freshTTYOut
	if ttyIn == nil {
		ttyIn = s.config.TTYIn
	}
	if ttyOut == nil {
		ttyOut = s.config.TTYOut
	}
	if ttyErr == nil {
		ttyErr = s.config.TTYErr
	}

	// Update config with the TTY handles we're using
	// This ensures future restarts use the correct handles
	if freshTTYIn != nil {
		s.config.TTYIn = freshTTYIn
		s.config.TTYOut = freshTTYOut
		s.config.TTYErr = freshTTYOut
		log.Infof("[SESSION] Updated config TTY handles for %s with fresh TTY", s.config.ContainerID)
	}

	// Create a new copier with the config
	s.copier = NewCopier(s.config)
	// IMPORTANT: Set TTY handles BEFORE starting the copier
	// This ensures the copier goroutines use the correct file descriptors
	s.copier.SetTTYs(ttyIn, ttyOut, ttyErr)

	// Set FIFOs in copier
	s.copier.SetStdin(stdinFifo)
	s.copier.SetStdout(stdoutFifo)
	s.copier.SetStderr(stderrFifo)

	// Set stdout FIFO for local echo in TTY mode
	if s.config.Terminal && stdoutFifo != nil {
		s.copier.SetStdoutFifoForEcho(stdoutFifo)
	}

	// Start copier
	if err := s.copier.Start(); err != nil {
		return fmt.Errorf("failed to start copier: %w", err)
	}

	s.started = true
	log.Infof("[SESSION] Restarted IO session for %s", s.config.ContainerID)

	return nil
}

// IsRunning returns true if the IO session is currently running.
func (s *Session) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	log.Debugf("[SESSION] IsRunning for %s: %v", s.config.ContainerID, s.started)
	return s.started
}

// UpdateTTYs updates the TTY handles for the session and its copier.
// This is used during reattach to replace stale closed TTY file descriptors
// with freshly opened ones.
func (s *Session) UpdateTTYs(stdin, stdout *os.File) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Infof("[SESSION] Updating TTY handles for %s", s.config.ContainerID)

	// Update config with new TTY handles
	s.config.TTYIn = stdin
	s.config.TTYOut = stdout
	// TTYErr is same as TTYOut for RPMSG TTY
	s.config.TTYErr = stdout

	// Update copier with new TTY handles
	if s.copier != nil {
		s.copier.SetTTYs(stdin, stdout, stdout)
		log.Infof("[SESSION] TTY handles updated in copier for %s", s.config.ContainerID)
	}
}

// ensureFIFOs creates all FIFOs if they don't exist.
func (s *Session) ensureFIFOs() error {
	paths := []string{s.config.StdinFIFO, s.config.StdoutFIFO, s.config.StderrFIFO}

	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := ensureFIFO(path); err != nil {
			return err
		}
	}

	return nil
}

// ensureFIFO creates a FIFO if it doesn't exist.
func ensureFIFO(path string) error {
	// Skip non-file paths (like binary:// URLs, fd:// URLs, etc.)
	if !IsValidFIFOPath(path) {
		log.Debugf("[SESSION] Skipping FIFO creation for non-file path: %s", path)
		return nil
	}

	// Check if exists
	if stat, err := os.Stat(path); err == nil {
		// Exists, verify it's a FIFO
		if stat.Mode()&os.ModeNamedPipe == 0 {
			return fmt.Errorf("existing file %s is not a FIFO", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	// Create directory
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Create FIFO
	if err := syscall.Mkfifo(path, 0600); err != nil {
		return fmt.Errorf("failed to create FIFO %s: %w", path, err)
	}

	log.Debugf("[SESSION] Created FIFO: %s", path)
	return nil
}
