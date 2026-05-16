package io

import (
	"context"
	"fmt"

	"golang.org/x/sys/unix"
	"micrun/internal/support/contextx"
	"micrun/internal/support/logger"
)

type epollWaiter struct {
	epfd          int
	cancelPipeR   int
	cancelPipeW   int
	ownsCancel    bool
	disabled      bool
	edgeTriggered bool
	events        [epollMaxEvents]unix.EpollEvent
	drainBuf      [1]byte
}

const epollMaxEvents = 4

func newEpollWaiter(cancelPipeR, cancelPipeW int, edgeTriggered bool) epollWaiter {
	return epollWaiter{
		epfd:          -1,
		cancelPipeR:   cancelPipeR,
		cancelPipeW:   cancelPipeW,
		ownsCancel:    cancelPipeW >= 0,
		edgeTriggered: edgeTriggered,
	}
}

func (w *epollWaiter) init(targetFd int) error {
	epfd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return fmt.Errorf("epoll_create1 failed: %w", err)
	}
	w.epfd = epfd

	events := unix.EPOLLIN
	if w.edgeTriggered {
		events |= unix.EPOLLET
	}
	event := unix.EpollEvent{
		Events: uint32(events),
		Fd:     int32(targetFd),
	}
	if err := unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, targetFd, &event); err != nil {
		unix.Close(epfd)
		w.epfd = -1
		return fmt.Errorf("epoll_ctl add target fd failed: %w", err)
	}

	if w.cancelPipeR >= 0 {
		pipeEvent := unix.EpollEvent{
			Events: unix.EPOLLIN,
			Fd:     int32(w.cancelPipeR),
		}
		if err := unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, w.cancelPipeR, &pipeEvent); err != nil {
			unix.Close(epfd)
			w.epfd = -1
			return fmt.Errorf("epoll_ctl add cancel pipe failed: %w", err)
		}
	}

	log.Tracef("[IO] epoll initialized: epfd=%d, targetFd=%d, cancelPipeR=%d, edgeTriggered=%v",
		epfd, targetFd, w.cancelPipeR, w.edgeTriggered)
	return nil
}

func (w *epollWaiter) wait(ctx context.Context, targetFd int) bool {
	ctx = contextx.OrBackground(ctx)
	if w.disabled {
		return waitFallbackPoll(ctx)
	}

	if w.epfd < 0 {
		if err := w.init(targetFd); err != nil {
			log.Warnf("[IO] Failed to init epoll, falling back to sleep: %v", err)
			w.disabled = true
			return waitFallbackPoll(ctx)
		}
	}

	const epollTimeoutMs = 100

	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		n, err := unix.EpollWait(w.epfd, w.events[:], epollTimeoutMs)
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
			if w.events[i].Fd == int32(w.cancelPipeR) {
				w.drainCancelPipe()
				return false
			}
			if w.events[i].Fd == int32(targetFd) && (w.events[i].Events&unix.EPOLLIN) != 0 {
				return true
			}
		}
	}
}

func (w *epollWaiter) drainCancelPipe() {
	for {
		if _, err := unix.Read(w.cancelPipeR, w.drainBuf[:]); err != nil {
			return
		}
	}
}

func (w *epollWaiter) signalCancel() {
	if w.ownsCancel && w.cancelPipeW >= 0 {
		unix.Close(w.cancelPipeW)
		w.cancelPipeW = -1
	}
}

func (w *epollWaiter) disable() {
	w.disabled = true
	if w.epfd >= 0 {
		unix.Close(w.epfd)
		w.epfd = -1
	}
}

func (w *epollWaiter) reenable() {
	w.disabled = false
	if w.epfd >= 0 {
		unix.Close(w.epfd)
		w.epfd = -1
	}
}

func (w *epollWaiter) close() {
	w.signalCancel()
	if w.epfd >= 0 {
		unix.Close(w.epfd)
		w.epfd = -1
	}
	if w.ownsCancel && w.cancelPipeR >= 0 {
		unix.Close(w.cancelPipeR)
		w.cancelPipeR = -1
	}
}
