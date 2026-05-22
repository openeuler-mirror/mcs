package io

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	"micrun/internal/support/definitions"
	"micrun/internal/support/lockutil"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"

	"github.com/containerd/fifo"
)

// Session manages an IO session for a container.
// It handles FIFO creation, opening, and data copying.
type Session struct {
	config           Config
	ttys             sessionTTYs
	fifoOps          fifoManager
	copier           *Copier
	eventBus         *EventBus
	parentCtx        context.Context
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.Mutex
	started          bool
	preservedStreams bool
}

type sessionTTYs struct {
	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader
}

type fifoManager interface {
	EnsureFIFO(path string) error
	OpenFIFO(ctx context.Context, path string, flags int) (io.ReadWriteCloser, error)
}

type defaultFIFOManager struct{}

func (defaultFIFOManager) EnsureFIFO(path string) error {
	return ensureFIFO(path)
}

func (defaultFIFOManager) OpenFIFO(ctx context.Context, path string, flags int) (io.ReadWriteCloser, error) {
	return fifo.OpenFifo(ctx, path, flags, 0)
}

var nonFileFIFOPathPrefixes = []string{
	"binary://",
	"fd://",
	"socket://",
}

// NewSession creates a new IO session.
func NewSession(config Config) (*Session, error) {
	if config.ContainerID == "" {
		return nil, fmt.Errorf("container ID is required")
	}
	config = normalizeConfig(config)

	s := &Session{
		config:  config,
		fifoOps: defaultFIFOManager{},
		ttys: sessionTTYs{
			stdin:  config.TTYIn,
			stdout: config.TTYOut,
			stderr: config.TTYErr,
		},
		parentCtx: contextx.OrBackground(config.Context),
	}
	s.renewContext()

	s.copier = s.newCopier(s.ttys)

	return s, nil
}

// Copier returns the copier for setting callbacks.
func (s *Session) Copier() *Copier {
	return s.copier
}

// IsValidFIFOPath checks if a path is a valid FIFO path (not a URL or empty string).
func IsValidFIFOPath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed != path {
		return false
	}
	if isNonFileIOPath(path) {
		return false
	}
	return true
}

func isNonFileIOPath(path string) bool {
	for _, prefix := range nonFileFIFOPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// GenerateStandardFIFOPath generates the standard containerd FIFO path for a container.
// Format: /run/containerd/io.containerd.runtime.v2.task/<namespace>/<container-id>/<stream>
// where stream is "stdin", "stdout", or "stderr".
func GenerateStandardFIFOPath(namespace, containerID, stream string) string {
	return filepath.Join(
		defs.ContainerdTaskDir,
		fifoPathSegment(namespace),
		fifoPathSegment(containerID),
		fifoPathSegment(stream),
	)
}

func fifoPathSegment(value string) string {
	segment, err := validation.NormalizeSinglePathSegment(value)
	if err != nil {
		return "_"
	}
	return segment
}

func (s *Session) withLock(body func()) {
	lockutil.WithLock(&s.mu, body)
}

// Start creates FIFOs, opens them, and starts the copier.
func (s *Session) Start() error {
	var startErr error
	s.withLock(func() {
		if err := s.requireStoppedAndActiveParent(); err != nil {
			startErr = err
			return
		}
		startErr = s.startSessionLocked(sessionStartRequest{
			contextMode: sessionContextReuseIfActive,
			successLog:  "Started",
		})
	})
	if startErr != nil {
		return startErr
	}

	return nil
}

// Stop stops the IO session.
func (s *Session) Stop() {
	s.withLock(func() {
		s.stopLocked(sessionStopCloseStreams)
	})
}

// StopWithoutClosingFIFOs stops the IO session but keeps FIFOs open for reattach.
// This is used when the client detaches but the container continues running.
func (s *Session) StopWithoutClosingFIFOs() {
	s.withLock(func() {
		s.stopLocked(sessionStopPreserveStreams)
	})
}

type sessionStopMode int

const (
	sessionStopCloseStreams sessionStopMode = iota
	sessionStopPreserveStreams
)

func (s *Session) stopLocked(mode sessionStopMode) {
	if s.started {
		if mode == sessionStopPreserveStreams {
			log.Debugf("[SESSION] Stopping IO copier for %s (keeping FIFOs for reattach)", s.config.ContainerID)
			s.copier.StopWithoutClosingFIFOs()
			s.preservedStreams = true
		} else {
			log.Infof("[SESSION] Stopping IO session for %s (started was %v)", s.config.ContainerID, s.started)
			s.copier.Stop()
			s.preservedStreams = false
		}
	} else if mode == sessionStopCloseStreams && s.preservedStreams {
		log.Infof("[SESSION] Closing preserved IO streams for %s", s.config.ContainerID)
		s.closeCopierStreams(s.copier)
		s.preservedStreams = false
	}

	s.closeContext()
	s.closeEventBus()
	s.started = false
}

func (s *Session) closeContext() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func (s *Session) closeEventBus() {
	if s.eventBus != nil {
		s.eventBus.Close()
		s.eventBus = nil
	}
}

// Restart restarts the IO session by reopening FIFOs and starting a new copier.
// This allows reattaching to a running container after detach.
func (s *Session) Restart() error {
	return s.RestartWithTTYs(nil, nil)
}

// RestartWithTTYs restarts the IO session with fresh TTY handles.
// This is the preferred method for reattach scenarios where the original TTY
// handles may be closed. If freshTTYIn or freshTTYOut are provided, they will
// be used instead of the handles from the active session runtime state.
func (s *Session) RestartWithTTYs(freshTTYIn io.WriteCloser, freshTTYOut io.Reader) error {
	var restartErr error
	s.withLock(func() {
		if err := s.requireStoppedAndActiveParent(); err != nil {
			restartErr = err
			return
		}
		freshTTYs, err := newFreshTTYSet(freshTTYIn, freshTTYOut)
		if err != nil {
			restartErr = err
			return
		}

		restartErr = s.startSessionLocked(sessionStartRequest{
			freshTTYs:   freshTTYs,
			contextMode: sessionContextAlwaysRenew,
			beforeLog:   "Restarting",
			successLog:  "Restarted",
		})
	})
	if restartErr != nil {
		return restartErr
	}
	return nil
}

type sessionContextMode int

const (
	sessionContextReuseIfActive sessionContextMode = iota
	sessionContextAlwaysRenew
)

type sessionStartRequest struct {
	freshTTYs   freshTTYSet
	contextMode sessionContextMode
	beforeLog   string
	successLog  string
}

func (s *Session) startSessionLocked(request sessionStartRequest) error {
	if request.beforeLog != "" {
		log.Infof("[SESSION] %s IO session for %s", request.beforeLog, s.config.ContainerID)
	}
	if err := s.ensureFIFOs(); err != nil {
		return fmt.Errorf("failed to create FIFOs: %w", err)
	}
	s.prepareStartContext(request.contextMode)
	if err := s.startWithTTYSet(request.freshTTYs); err != nil {
		return err
	}

	s.started = true
	if request.successLog != "" {
		log.Infof("[SESSION] %s IO session for %s", request.successLog, s.config.ContainerID)
	}
	return nil
}

func (s *Session) prepareStartContext(mode sessionContextMode) {
	if mode == sessionContextAlwaysRenew || s.ctx == nil || s.ctx.Err() != nil {
		s.renewContext()
	}
}

func (s *Session) requireStoppedAndActiveParent() error {
	if s.started {
		return fmt.Errorf("already started")
	}
	if s.parentCtx.Err() != nil {
		return fmt.Errorf("session context canceled: %w", s.parentCtx.Err())
	}
	return nil
}

func (s *Session) startWithTTYSet(freshTTYs freshTTYSet) error {
	previousCopier := s.copier
	previousPreserved := s.preservedStreams

	streams, err := s.openFIFOs()
	if err != nil {
		return err
	}

	nextTTYs := freshTTYs.resolve(s.ttys)
	nextCopier := s.newCopier(nextTTYs)
	s.wireCopier(nextCopier, streams)

	if err := nextCopier.Start(); err != nil {
		err = fmt.Errorf("failed to start copier: %w", err)
		streams.closeAll()
		return err
	}

	s.copier = nextCopier
	if freshTTYs.provided {
		s.setTTYs(nextTTYs)
		log.Infof("[SESSION] Updated runtime TTY handles for %s with fresh TTY", s.config.ContainerID)
	}
	if previousPreserved {
		s.closeSupersededCopierStreams(previousCopier, s.copier)
		s.preservedStreams = false
	}

	return nil
}

func (s *Session) renewContext() {
	if s.eventBus != nil {
		s.eventBus.Close()
	}
	s.closeContext()

	ctx, cancel := context.WithCancel(s.parentCtx)
	s.ctx = ctx
	s.cancel = cancel
	s.eventBus = NewEventBus(ctx)
}

func (s *Session) newCopier(ttys sessionTTYs) *Copier {
	copier := NewCopier(s.copierConfig())
	copier.SetTTYs(ttys.stdin, ttys.stdout, ttys.stderr)
	return copier
}

func (s *Session) copierConfig() Config {
	config := s.config
	config.EventBus = s.eventBus
	config.Context = s.ctx
	return config
}

func (s *Session) eventStream() *eventStream {
	return &eventStream{ctx: s.ctx, bus: s.eventBus}
}

// EventStream returns a subscription view for the session's current IO event bus.
func (s *Session) EventStream() ports.IOEventStream {
	return s.eventStream()
}

func (s *Session) setTTYs(ttys sessionTTYs) {
	s.ttys = ttys
}

func validateFreshTTYPair(freshTTYIn io.WriteCloser, freshTTYOut io.Reader) error {
	_, err := newFreshTTYSet(freshTTYIn, freshTTYOut)
	return err
}

type freshTTYSet struct {
	stdin    io.WriteCloser
	stdout   io.Reader
	provided bool
}

func newFreshTTYSet(stdin io.WriteCloser, stdout io.Reader) (freshTTYSet, error) {
	hasStdin := !validation.IsNil(stdin)
	hasStdout := !validation.IsNil(stdout)
	if hasStdin != hasStdout {
		return freshTTYSet{}, fmt.Errorf("fresh TTY stdin and stdout must be provided together")
	}
	if !hasStdin {
		return freshTTYSet{}, nil
	}
	return freshTTYSet{
		stdin:    stdin,
		stdout:   stdout,
		provided: true,
	}, nil
}

func (set freshTTYSet) resolve(base sessionTTYs) sessionTTYs {
	if !set.provided {
		return base
	}
	return sessionTTYs{
		stdin:  set.stdin,
		stdout: set.stdout,
		stderr: set.stdout,
	}
}

type sessionFIFOStreams struct {
	stdin  io.ReadCloser
	stdout io.WriteCloser
	stderr io.WriteCloser
}

type sessionFIFOOpenSpec struct {
	stream string
	path   string
	flags  int
	assign func(*sessionFIFOStreams, io.ReadWriteCloser)
}

func (streams *sessionFIFOStreams) closeAll() []error {
	return mergeCloseErrors(
		closeStream("stdin FIFO", &streams.stdin),
		closeStream("stdout FIFO", &streams.stdout),
		closeStream("stderr FIFO", &streams.stderr),
	)
}

func (s *Session) openFIFOs() (sessionFIFOStreams, error) {
	streams := sessionFIFOStreams{}

	for _, spec := range s.fifoOpenSpecs() {
		file, err := s.openFIFO(spec.stream, spec.path, spec.flags)
		if err != nil {
			return sessionFIFOStreams{}, fifoOpenError(spec.stream, err, streams.closeAll()...)
		}
		if file != nil {
			spec.assign(&streams, file)
		}
	}

	return streams, nil
}

func (s *Session) fifoOpenSpecs() []sessionFIFOOpenSpec {
	return []sessionFIFOOpenSpec{
		{
			stream: "stdin",
			path:   s.config.StdinFIFO,
			flags:  syscall.O_RDONLY | syscall.O_NONBLOCK,
			assign: func(streams *sessionFIFOStreams, file io.ReadWriteCloser) {
				streams.stdin = file
			},
		},
		{
			stream: "stdout",
			path:   s.config.StdoutFIFO,
			flags:  syscall.O_WRONLY | syscall.O_NONBLOCK,
			assign: func(streams *sessionFIFOStreams, file io.ReadWriteCloser) {
				streams.stdout = file
			},
		},
		{
			stream: "stderr",
			path:   s.config.StderrFIFO,
			flags:  syscall.O_WRONLY | syscall.O_NONBLOCK,
			assign: func(streams *sessionFIFOStreams, file io.ReadWriteCloser) {
				streams.stderr = file
			},
		},
	}
}

func (s *Session) openFIFO(stream, path string, flags int) (io.ReadWriteCloser, error) {
	if path == "" {
		return nil, nil
	}
	if !IsValidFIFOPath(path) {
		log.Debugf("[SESSION] Skipping %s FIFO (non-file path): %s", stream, path)
		return nil, nil
	}
	log.Debugf("[SESSION] Opening %s FIFO %s for %s", stream, path, s.config.ContainerID)
	file, err := s.fifoOps.OpenFIFO(s.ctx, path, flags)
	if err != nil {
		log.Warnf("[SESSION] Failed to open %s FIFO %s: %v", stream, path, err)
		return nil, err
	}
	return file, nil
}

func fifoOpenError(stream string, openErr error, cleanupErrs ...error) error {
	cleanupErr := errors.Join(cleanupErrs...)
	if cleanupErr != nil {
		return fmt.Errorf("failed to open %s FIFO: %w; cleanup failed: %w", stream, openErr, cleanupErr)
	}
	return fmt.Errorf("failed to open %s FIFO: %w", stream, openErr)
}

func (s *Session) wireCopier(copier *Copier, streams sessionFIFOStreams) {
	copier.SetStdin(streams.stdin)
	copier.SetStdout(streams.stdout)
	copier.SetStderr(streams.stderr)

	if s.config.Terminal && streams.stdout != nil {
		log.Debugf("[SESSION] Setting stdoutFifoForEcho for %s (Terminal=true)", s.config.ContainerID)
		copier.SetStdoutFifoForEcho(streams.stdout)
	}
}

func (s *Session) closeCopierStreams(copier *Copier) {
	if copier == nil {
		return
	}
	if err := copier.closeFIFOs(); err != nil {
		log.Warnf("[SESSION] Failed to close preserved FIFOs for %s: %v", s.config.ContainerID, err)
	}
	if err := copier.closeTTYs(); err != nil {
		log.Warnf("[SESSION] Failed to close preserved TTYs for %s: %v", s.config.ContainerID, err)
	}
}

func (s *Session) closeSupersededCopierStreams(previous, current *Copier) {
	if previous == nil {
		return
	}
	if err := previous.closeFIFOs(); err != nil {
		log.Warnf("[SESSION] Failed to close superseded FIFOs for %s: %v", s.config.ContainerID, err)
	}
	if err := closeSupersededTTYs(previous, current); err != nil {
		log.Warnf("[SESSION] Failed to close superseded TTYs for %s: %v", s.config.ContainerID, err)
	}
}

func closeSupersededTTYs(previous, current *Copier) error {
	if current == nil {
		return previous.closeTTYs()
	}

	if !sameIOHandle(previous.ttyIn, current.ttyIn) {
		if err := joinCloseErrors(closeStream("TTY stdin", &previous.ttyIn)); err != nil {
			return err
		}
	}

	stdout := previous.ttyOut
	stderr := previous.ttyErr
	if sameIOHandle(stdout, current.ttyOut) || sameIOHandle(stdout, current.ttyErr) {
		stdout = nil
	}
	if sameIOHandle(stderr, current.ttyOut) || sameIOHandle(stderr, current.ttyErr) {
		stderr = nil
	}
	err := joinCloseErrors(closeTTYOutputReaders(&stdout, &stderr))
	previous.ttyIn = nil
	previous.ttyOut = nil
	previous.ttyErr = nil
	return err
}

func sameIOHandle(left, right any) bool {
	if validation.IsNil(left) || validation.IsNil(right) {
		return false
	}
	leftFD, leftHasFD := fdOf(left)
	rightFD, rightHasFD := fdOf(right)
	if leftHasFD || rightHasFD {
		return leftHasFD && rightHasFD && leftFD == rightFD
	}

	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	if leftValue.Type() != rightValue.Type() || !leftValue.Type().Comparable() {
		return false
	}
	return leftValue.Interface() == rightValue.Interface()
}

// IsRunning returns true if the IO session is currently running.
func (s *Session) IsRunning() bool {
	running := lockutil.WithLockValue(&s.mu, func() bool {
		log.Debugf("[SESSION] IsRunning for %s: %v", s.config.ContainerID, s.started)
		return s.started
	})
	return running
}

// ensureFIFOs creates all FIFOs if they don't exist.
func (s *Session) ensureFIFOs() error {
	for _, path := range uniqueValidFIFOPaths(s.fifoPaths()...) {
		if err := s.fifoOps.EnsureFIFO(path); err != nil {
			return err
		}
	}

	return nil
}

func (s *Session) fifoPaths() []string {
	return []string{s.config.StdinFIFO, s.config.StdoutFIFO, s.config.StderrFIFO}
}

func uniqueValidFIFOPaths(paths ...string) []string {
	validPaths := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if !IsValidFIFOPath(path) {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		validPaths = append(validPaths, path)
	}
	return validPaths
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
