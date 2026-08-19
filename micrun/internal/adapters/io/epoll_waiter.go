package io

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sys/unix"
	"micrun/internal/support/contextx"
	"micrun/internal/support/logger"
)

const epollMaxEvents = 8

// epollWaiter multiplexes one or more target fds onto a single epoll
// instance. All field access is guarded by mu; wait() snapshots the epfd
// under the lock and uses a per-call events buffer so concurrent waiters
// (e.g. copyStdout and copyStderr sharing one waiter in split mode) do not
// race on shared state.
type epollWaiter struct {
	mu            sync.Mutex
	epfd          int
	cancelPipeR   int
	cancelPipeW   int
	ownsCancel    bool
	disabled      bool
	closed        bool
	edgeTriggered bool
	registeredFds map[int]struct{} // target fds added to the epoll instance
	closeOnce     sync.Once
}

func newEpollWaiter(cancelPipeR, cancelPipeW int, edgeTriggered bool) epollWaiter {
	return epollWaiter{
		epfd:          -1,
		cancelPipeR:   cancelPipeR,
		cancelPipeW:   cancelPipeW,
		ownsCancel:    cancelPipeW >= 0,
		edgeTriggered: edgeTriggered,
		registeredFds: make(map[int]struct{}),
	}
}

// init creates the epoll instance and registers targetFd plus the cancel
// pipe. It is idempotent: if the waiter is already initialized, the fd is
// simply added to the existing instance (so concurrent waiters on different
// fds all get registered instead of leaking duplicate epoll instances).
func (w *epollWaiter) init(targetFd int) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return fmt.Errorf("waiter is closed")
	}

	if w.epfd < 0 {
		epfd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
		if err != nil {
			return fmt.Errorf("epoll_create1 failed: %w", err)
		}
		w.epfd = epfd
	}

	events := uint32(unix.EPOLLIN)
	if w.edgeTriggered {
		events |= unix.EPOLLET
	}

	if err := w.addFDLocked(targetFd, events); err != nil {
		// Drop the registeredFds bookkeeping along with the epfd: a later
		// init would otherwise see stale entries and skip EPOLL_CTL_ADD on
		// the fresh instance, leaving the fd permanently unwatched.
		w.closeEpfdLocked()
		return err
	}

	if w.cancelPipeR >= 0 {
		if err := w.addFDLocked(w.cancelPipeR, unix.EPOLLIN); err != nil {
			w.closeEpfdLocked()
			return err
		}
	}

	log.Tracef("[IO] epoll initialized: epfd=%d, targetFd=%d, cancelPipeR=%d, edgeTriggered=%v",
		w.epfd, targetFd, w.cancelPipeR, w.edgeTriggered)
	return nil
}

// addFDLocked registers fd on the epoll instance and records it in
// registeredFds. Caller must hold mu.
func (w *epollWaiter) addFDLocked(fd int, events uint32) error {
	if w.epfd < 0 {
		return fmt.Errorf("epoll not initialized")
	}
	if _, ok := w.registeredFds[fd]; ok {
		return nil
	}
	event := unix.EpollEvent{
		Events: events,
		Fd:     int32(fd),
	}
	if err := unix.EpollCtl(w.epfd, unix.EPOLL_CTL_ADD, fd, &event); err != nil {
		return fmt.Errorf("epoll_ctl add fd %d failed: %w", fd, err)
	}
	w.registeredFds[fd] = struct{}{}
	return nil
}

// wait blocks until data is available on targetFd, the cancel pipe fires,
// or ctx is done. It returns true when targetFd is readable.
func (w *epollWaiter) wait(ctx context.Context, targetFd int) bool {
	ctx = contextx.OrBackground(ctx)

	// Snapshot state under lock, then release before the blocking EpollWait.
	w.mu.Lock()
	if w.disabled || w.closed {
		w.mu.Unlock()
		return waitFallbackPoll(ctx)
	}
	if w.epfd < 0 {
		w.mu.Unlock()
		if err := w.init(targetFd); err != nil {
			log.Warnf("[IO] Failed to init epoll, falling back to sleep: %v", err)
			w.mu.Lock()
			w.disabled = true
			w.mu.Unlock()
			return waitFallbackPoll(ctx)
		}
		w.mu.Lock()
		// init() released the lock; close()/disable()/reset() may have run in
		// between and torn down the fresh epfd (or marked the waiter closed).
		// Re-check before snapshotting so we never EpollWait on a stale or
		// reused fd number.
		if w.closed || w.epfd < 0 {
			w.mu.Unlock()
			return waitFallbackPoll(ctx)
		}
	} else if !w.hasTargetFDLocked(targetFd) {
		// Waiter already initialized (possibly by a sibling worker on a
		// different fd); register this fd so this stream gets wakeups too.
		events := uint32(unix.EPOLLIN)
		if w.edgeTriggered {
			events |= unix.EPOLLET
		}
		if err := w.addFDLocked(targetFd, events); err != nil {
			log.Warnf("[IO] Failed to add fd %d to epoll, falling back to sleep: %v", targetFd, err)
			w.mu.Unlock()
			return waitFallbackPoll(ctx)
		}
	}
	epfd := w.epfd
	cancelPipeR := w.cancelPipeR
	w.mu.Unlock()

	const epollTimeoutMs = 100
	var events [epollMaxEvents]unix.EpollEvent

	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		n, err := unix.EpollWait(epfd, events[:], epollTimeoutMs)
		if err != nil {
			if err == unix.EINTR {
				select {
				case <-ctx.Done():
					return false
				default:
					continue
				}
			}
			log.Warnf("[IO] epoll_wait failed: %v, disabling", err)
			w.disable()
			return waitFallbackPoll(ctx)
		}

		select {
		case <-ctx.Done():
			return false
		default:
		}

		if n == 0 {
			continue
		}

		for i := 0; i < n; i++ {
			if events[i].Fd == int32(cancelPipeR) {
				w.drainCancelPipe(cancelPipeR)
				return false
			}
			if events[i].Fd == int32(targetFd) {
				// EPOLLIN: data available. EPOLLHUP/EPOLLERR: the peer
				// closed (e.g. attach client disconnected, guest domain
				// gone) — treat as readable so the read loop observes
				// EOF/EIO and exits instead of busy-spinning on an
				// epoll that fires immediately forever.
				if (events[i].Events&unix.EPOLLIN) != 0 || (events[i].Events&(unix.EPOLLHUP|unix.EPOLLERR)) != 0 {
					return true
				}
			}
		}
	}
}

// hasTargetFDLocked reports whether fd is already registered, tracked in
// registeredFds. Caller must hold mu. Do NOT probe with EPOLL_CTL_MOD: MOD
// replaces the event mask, silently stripping EPOLLET from edge-triggered
// registrations.
func (w *epollWaiter) hasTargetFDLocked(fd int) bool {
	_, ok := w.registeredFds[fd]
	return ok
}

func (w *epollWaiter) drainCancelPipe(cancelPipeR int) {
	for {
		var buf [1]byte
		n, err := unix.Read(cancelPipeR, buf[:])
		if err != nil || n == 0 {
			return
		}
	}
}

func (w *epollWaiter) signalCancel() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ownsCancel && w.cancelPipeW >= 0 {
		unix.Close(w.cancelPipeW)
		w.cancelPipeW = -1
	}
}

// closeEpfdLocked closes the epoll instance and forgets all registered fds.
// Caller must hold mu.
func (w *epollWaiter) closeEpfdLocked() {
	if w.epfd >= 0 {
		unix.Close(w.epfd)
		w.epfd = -1
	}
	w.registeredFds = make(map[int]struct{})
}

func (w *epollWaiter) disable() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.disabled = true
	w.closeEpfdLocked()
}

// isDisabled reports the disabled flag under the lock (disable/reenable
// write it from other goroutines).
func (w *epollWaiter) isDisabled() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.disabled
}

func (w *epollWaiter) reenable() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.disabled = false
	if w.epfd >= 0 {
		unix.Close(w.epfd)
		w.epfd = -1
	}
	w.registeredFds = make(map[int]struct{})
}

// reset closes the current epoll instance under the lock so the next
// init() creates a fresh one. Used when the target fd set changes (e.g.
// SetTTYs swapping TTY fds).
func (w *epollWaiter) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closeEpfdLocked()
}

func (w *epollWaiter) close() {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.closed = true
		if w.ownsCancel && w.cancelPipeW >= 0 {
			unix.Close(w.cancelPipeW)
			w.cancelPipeW = -1
		}
		if w.epfd >= 0 {
			unix.Close(w.epfd)
			w.epfd = -1
		}
		w.registeredFds = make(map[int]struct{})
		if w.ownsCancel && w.cancelPipeR >= 0 {
			unix.Close(w.cancelPipeR)
			w.cancelPipeR = -1
		}
	})
}
