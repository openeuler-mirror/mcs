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
	"golang.org/x/sys/unix"
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
	// Open with a raw non-blocking fd when requested: containerd/fifo
	// strips O_NONBLOCK and opens blocking, which would hang copier
	// workers on a stalled reader forever — EAGAIN backpressure, the
	// drain-until-EAGAIN loop and ctx cancellation would never fire. Go's
	// os.File would also re-block via the netpoller, so the raw unix
	// syscalls in nonBlockingFIFO are mandatory.
	if flags&syscall.O_NONBLOCK != 0 {
		return openNonBlockingFIFO(path, flags)
	}
	return fifo.OpenFifo(ctx, path, flags, 0)
}

// nonBlockingFIFO wraps a FIFO fd opened with O_NONBLOCK and issues raw
// unix read/write syscalls, returning EAGAIN to the caller instead of
// blocking (or re-blocking in the netpoller like os.File does).
type nonBlockingFIFO struct {
	fd int
}

func openNonBlockingFIFO(path string, flags int) (*nonBlockingFIFO, error) {
	fd, err := unix.Open(path, flags|unix.O_CLOEXEC, 0)
	if err == nil {
		return &nonBlockingFIFO{fd: fd}, nil
	}
	// O_WRONLY|O_NONBLOCK fails with ENXIO when no reader exists yet.
	// Detached ctr/nerdctl start generates FIFOs for later attach, so the
	// writer must be obtained without waiting for a client. Hold a dummy
	// read end only long enough to open the write fd, then drop it so a
	// later write sees EPIPE until a real reader attaches.
	if !isWriterOnlyFIFOFlags(flags) || !isENXIO(err) {
		return nil, err
	}
	hold, holdErr := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if holdErr != nil {
		return nil, err
	}
	fd, err = unix.Open(path, flags|unix.O_CLOEXEC, 0)
	_ = unix.Close(hold)
	if err != nil {
		return nil, err
	}
	return &nonBlockingFIFO{fd: fd}, nil
}

func isWriterOnlyFIFOFlags(flags int) bool {
	return flags&unix.O_ACCMODE == unix.O_WRONLY || flags&syscall.O_ACCMODE == syscall.O_WRONLY
}

func (f *nonBlockingFIFO) Read(p []byte) (int, error) {
	if f == nil || f.fd < 0 {
		return 0, io.ErrClosedPipe
	}
	n, err := unix.Read(f.fd, p)
	// Honor the io.Reader contract: report the bytes already transferred even
	// when an error accompanies them. Returning (0, err) after a partial read
	// would silently drop those bytes; callers (e.g. writeOutputFIFOData) are
	// written to advance by n before inspecting err.
	if err != nil {
		return n, err
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

func (f *nonBlockingFIFO) Write(p []byte) (int, error) {
	if f == nil || f.fd < 0 {
		return 0, io.ErrClosedPipe
	}
	n, err := unix.Write(f.fd, p)
	// Honor the io.Writer contract: report partial progress alongside the
	// error. Returning (0, err) when the syscall transferred n>0 bytes would
	// make callers re-send the already-written prefix and duplicate output.
	if err != nil {
		return n, err
	}
	return n, nil
}

func (f *nonBlockingFIFO) Close() error {
	if f == nil || f.fd < 0 {
		return nil
	}
	err := unix.Close(f.fd)
	f.fd = -1
	return err
}

// Fd exposes the raw fd so the copier can register it with epoll.
func (f *nonBlockingFIFO) Fd() uintptr {
	if f == nil {
		return ^uintptr(0)
	}
	return uintptr(f.fd)
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
			s.copier.StopWithoutClosingFIFOsAndWait()
			s.preservedStreams = true
		} else {
			log.Infof("[SESSION] Stopping IO session for %s (started was %v)", s.config.ContainerID, s.started)
			s.copier.Stop()
			// copier.Stop() may have lost the beginStop CAS to a preserve-mode
			// stop (stopFromWorker(false) on detach, or StopWithoutClosingFIFOs).
			// In that case the FIFO/TTY fds are still open. A close-mode stop
			// is a final teardown (task Delete / container exit): no reattach
			// will follow and no later Stop is guaranteed, so take over the
			// preserved fds now instead of leaking them until process exit.
			if s.copier.StreamsPreservedOnStop() {
				s.closeSupersededCopierStreams(s.copier, nil)
			}
			s.preservedStreams = false
		}
	} else {
		// Never-started or failed-start copier: its cancel-pipe fds are
		// still open. Release them WITHOUT closing the TTY streams — the
		// caller owns those handles (closing them here would break the
		// console for a later retry/reattach).
		if s.copier != nil {
			s.copier.releaseResources()
		}
		if mode == sessionStopCloseStreams && s.preservedStreams {
			log.Infof("[SESSION] Closing preserved IO streams for %s", s.config.ContainerID)
			s.closeCopierStreams(s.copier)
			s.preservedStreams = false
		}
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
	return s.RestartWithSubscriber(freshTTYIn, freshTTYOut, nil)
}

// RestartWithSubscriber is like RestartWithTTYs but calls onNewBus after the
// event bus is renewed and BEFORE the copier starts. This closes the window
// in which a self-stopping control event (ExitCommand/Detach) published by
// the freshly-started copier could be permanently lost because no subscriber
// was registered on the new bus yet.
func (s *Session) RestartWithSubscriber(freshTTYIn io.WriteCloser, freshTTYOut io.Reader, onNewBus func(ports.IOEventStream)) error {
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

		restartErr = s.startSessionLockedWithHook(sessionStartRequest{
			freshTTYs:   freshTTYs,
			contextMode: sessionContextAlwaysRenew,
			beforeLog:   "Restarting",
			successLog:  "Restarted",
		}, onNewBus)
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
	return s.startSessionLockedWithHook(request, nil)
}

func (s *Session) startSessionLockedWithHook(request sessionStartRequest, onNewBus func(ports.IOEventStream)) error {
	if request.beforeLog != "" {
		log.Infof("[SESSION] %s IO session for %s", request.beforeLog, s.config.ContainerID)
	}
	if err := s.ensureFIFOs(); err != nil {
		return fmt.Errorf("failed to create FIFOs: %w", err)
	}
	s.prepareStartContext(request.contextMode)
	// Call the subscriber hook AFTER the new bus is created (renewContext)
	// but BEFORE the copier starts publishing. Without this ordering, a
	// self-stopping control event published by the copier's first read
	// could be permanently lost because no subscriber was registered yet.
	// Read ctx/bus directly: we already hold s.mu (this runs inside
	// startSessionLockedWithHook under s.withLock), so calling the
	// locking eventStream() would self-deadlock.
	if onNewBus != nil {
		onNewBus(&eventStream{ctx: s.ctx, bus: s.eventBus, session: s})
	}
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
		if s.copier.Stopped() {
			// The copier died from its own worker (fatal IO error / guest
			// EOF) and nothing cleared `started`. Converge the flag so a
			// reattach can restart the session instead of being rejected
			// with "already started" while IO is dead (scan item 2.4).
			log.Infof("[SESSION] Copier for %s died from a worker; allowing session restart", s.config.ContainerID)
			s.started = false
		} else {
			return fmt.Errorf("already started")
		}
	}
	if s.parentCtx.Err() != nil {
		return fmt.Errorf("session context canceled: %w", s.parentCtx.Err())
	}
	return nil
}

func (s *Session) startWithTTYSet(freshTTYs freshTTYSet) error {
	previousCopier := s.copier
	// A worker-side preserve-stop (detach detected in the copier itself)
	// records the state on the copier, not on the session flag: the detach
	// event may still be in flight when a reattach restarts the session, so
	// the flag was never set. Treat the copier's own record as authoritative
	// too, or the previous FIFO/TTY fds leak when the restart only releases
	// the cancel-pipe.
	previousPreserved := s.preservedStreams || (previousCopier != nil && previousCopier.StreamsPreservedOnStop())

	streams, err := s.openFIFOs()
	if err != nil {
		// A preserved-streams stop left the previous copier's FIFO/TTY fds
		// open for reattach. If the restart fails here, those fds would
		// leak until the next successful restart or Delete. Close them now
		// and clear the preserved flag so the session is in a clean state.
		if previousPreserved {
			s.closeSupersededCopierStreams(previousCopier, nil)
			s.preservedStreams = false
		}
		return err
	}

	nextTTYs := freshTTYs.resolve(s.ttys)
	nextCopier := s.newCopier(nextTTYs)
	s.wireCopier(nextCopier, streams)

	if err := nextCopier.Start(); err != nil {
		err = fmt.Errorf("failed to start copier: %w", err)
		streams.closeAll()
		// Release the failed copier's cancel-pipe fds (its TTYs are the
		// caller's handles and are not closed by releaseResources).
		nextCopier.releaseResources()
		// A preserved-streams stop left the previous copier's FIFO/TTY fds
		// open for reattach. If the restart fails at copier start (after a
		// successful FIFO open), those fds would leak until the next
		// successful restart or Delete — the same hazard the openFIFOs
		// failure branch handles above. Close them now and clear the
		// preserved flag so the session is left in a clean state.
		if previousPreserved {
			s.closeSupersededCopierStreams(previousCopier, nil)
			s.preservedStreams = false
		}
		return err
	}

	s.copier = nextCopier
	if freshTTYs.provided {
		s.setTTYs(nextTTYs)
		log.Infof("[SESSION] Updated runtime TTY handles for %s with fresh TTY", s.config.ContainerID)
	}
	if previousPreserved {
		// The previous copier was already stopped by StopWithoutClosingFIFOs
		// (its cancel-pipe is released); only its FIFO/TTY handles remain
		// to be closed here.
		s.closeSupersededCopierStreams(previousCopier, s.copier)
		s.preservedStreams = false
	} else if previousCopier != nil {
		// First start, or start after a full-stop: the previous copier was
		// never started (NewSession reserved it) or already fully stopped.
		// Its cancel-pipe fds are still open — release them. releaseResources
		// does not touch FIFO/TTY handles (those are session/caller owned or
		// already closed by the full stop). beginStop is a CAS, so this is a
		// no-op when the copier was already stopped.
		previousCopier.releaseResources()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return &eventStream{ctx: s.ctx, bus: s.eventBus, session: s}
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

// IsRunning returns true if the IO session is currently pumping. The
// started flag alone is not enough: a copier that died from its own worker
// (fatal IO error, guest EOF — stopFromWorker) clears nothing on the
// session, so a session that can no longer deliver IO must not claim to
// run — EnsureAttach would no-op and reattach would stay silent until
// Delete (scan item 2.4). Session liveness therefore requires a live
// copier.
func (s *Session) IsRunning() bool {
	running := lockutil.WithLockValue(&s.mu, func() bool {
		alive := s.started && !s.copier.Stopped()
		log.Debugf("[SESSION] IsRunning for %s: %v (started=%v copierStopped=%v)",
			s.config.ContainerID, alive, s.started, s.copier.Stopped())
		return alive
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

	// Create FIFO. Concurrent ensure (or containerd) may create it between
	// Stat and Mkfifo; treat EEXIST as success when the path is already a FIFO.
	if err := syscall.Mkfifo(path, 0600); err != nil {
		if errors.Is(err, syscall.EEXIST) {
			stat, statErr := os.Stat(path)
			if statErr == nil && stat.Mode()&os.ModeNamedPipe != 0 {
				return nil
			}
			if statErr != nil {
				return fmt.Errorf("failed to create FIFO %s: %w (stat after EEXIST: %v)", path, err, statErr)
			}
			return fmt.Errorf("existing file %s is not a FIFO", path)
		}
		return fmt.Errorf("failed to create FIFO %s: %w", path, err)
	}

	log.Debugf("[SESSION] Created FIFO: %s", path)
	return nil
}
