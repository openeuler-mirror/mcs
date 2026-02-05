package io

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	log "micrun/logger"
)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// parseDetachKeys parses a detach key string into a byte sequence.
// Supports format like "ctrl-p,ctrl-q" -> []byte{16, 17}
// Returns nil if keys is empty or invalid.
func parseDetachKeys(keys string) []byte {
	if keys == "" {
		return nil
	}
	// Default to ctrl-p,ctrl-q if empty
	if keys == "ctrl-p,ctrl-q" || keys == "ctrl-p,ctrl-q" {
		return []byte{16, 17}
	}
	// Simple parser for ctrl-x,ctrl-y format
	result := []byte{}
	parts := splitString(keys, ",")
	for _, part := range parts {
		b := parseKeySequence(part)
		if b > 0 {
			result = append(result, b)
		}
	}
	if len(result) == 0 {
		// Fallback to default
		return []byte{16, 17}
	}
	return result
}

// parseKeySequence parses a single key sequence like "ctrl-p" -> 16
func parseKeySequence(s string) byte {
	s = trimString(s)
	switch s {
	case "ctrl-p", "Ctrl-P":
		return 16
	case "ctrl-q", "Ctrl-Q":
		return 17
	}
	return 0
}

// splitString splits a string by a separator.
func splitString(s, sep string) []string {
	result := []string{}
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

// trimString removes leading/trailing whitespace from a string.
func trimString(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// compressLineEndings compresses consecutive line endings (\r\n or \n) to a single \r\n.
// This handles RTOS firmware that outputs multiple consecutive line breaks.
func compressLineEndings(data []byte) []byte {
	if len(data) == 0 {
		return data
	}

	// Allocate result buffer (worst case: same size as input)
	result := make([]byte, 0, len(data))
	i := 0
	n := len(data)

	for i < n {
		if data[i] == '\r' {
			// Found CR, check if followed by LF
			if i+1 < n && data[i+1] == '\n' {
				// Found CRLF sequence
				j := i + 2
				// Count consecutive CRLF sequences
				for j+1 < n && data[j] == '\r' && data[j+1] == '\n' {
					j += 2
				}
				// Only emit single CRLF regardless of how many we found
				result = append(result, '\r', '\n')
				i = j
			} else {
				// Standalone CR, keep it
				result = append(result, data[i])
				i++
			}
		} else if data[i] == '\n' {
			// Found LF without preceding CR
			j := i + 1
			// Count consecutive LF sequences
			for j < n && data[j] == '\n' {
				j++
			}
			// Only emit single CRLF regardless of how many LFs we found
			result = append(result, '\r', '\n')
			i = j
		} else {
			// Normal character
			result = append(result, data[i])
			i++
		}
	}

	return result
}

// Copier handles bidirectional data copying between FIFOs and TTY.
type Copier struct {
	config Config
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// FIFOs
	stdinFifo  io.ReadCloser
	stdoutFIFO io.WriteCloser
	stderrFIFO io.WriteCloser

	// TTYs
	ttyIn  io.WriteCloser
	ttyOut io.Reader
	ttyErr io.Reader

	// Control
	stopped atomicBool

	// Line buffer for exit command detection (TTY mode only)
	lineBuf []byte

	// Track previous character for \r\n handling
	prevWasCR bool

	// For local echo in TTY mode (to avoid sending exit to RTOS)
	stdoutFifoForEcho io.WriteCloser

	// TTY ready flag (to publish TTYReady event only once)
	ttyReadyPublished atomicBool

	// cancelPipe is used to wake up Poll when context is cancelled.
	// Read fd is included in Poll, write fd is closed in Stop().
	// This enables zero-CPU idle waiting with instant cancellation.
	cancelPipeR, cancelPipeW int

	// epollFd is used for waiting on TTY data without busy polling (stdout).
	// Created when needed, closed in Stop(). Use -1 to indicate not created.
	epollFd int

	// stdinEpollFd is used for waiting on stdin FIFO data without busy polling.
	// Separate from epollFd because TTY and stdin use different epoll instances.
	// Created when needed, closed in Stop(). Use -1 to indicate not created.
	stdinEpollFd int

	// stdinEpollDisabled indicates that stdin epoll should not be used
	// (set to true after epoll failures, falls back to sleep-based polling)
	stdinEpollDisabled bool

	// EOF state for stdin (to handle detached mode where FIFO has no writer initially)
	stdinEOFSeen bool

	// Track whether we've received data from attach client
	// This helps distinguish between "initial EOF, waiting for attach" vs "EOF after data, attach done"
	attachClientConnected bool

	// Detach sequence detection (Ctrl+P, Ctrl+Q)
	detachKeys    []byte  // Parsed detach key sequence (e.g., []byte{16, 17} for ctrl-p,ctrl-q)
	detachBuf     []byte  // Buffer for tracking partial detach sequences
	detachPending bool    // True when we've matched part of the detach sequence

	// Echo suppression (to avoid double echo when both PTY and RTOS echo)
	suppressEcho    bool   // Whether to suppress echo from RTOS
	sentChars       []byte // Characters we sent to TTY (to match against echo)
	sentCharsCursor int    // Current position in sentChars buffer

	// Pre-allocated buffers for internal use (avoid repeated allocations)
	drainBuf [1]byte // Buffer for draining cancel pipe
}

type atomicBool struct {
	value int32
}

func (b *atomicBool) Load() bool { return atomic.LoadInt32(&b.value) != 0 }
func (b *atomicBool) Store(v bool) {
	atomic.StoreInt32(&b.value, boolToInt(v))
}
func (b *atomicBool) CompareAndSwap(old, new bool) bool {
	return atomic.CompareAndSwapInt32(&b.value, boolToInt(old), boolToInt(new))
}

func boolToInt(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// NewCopier creates a new copier.
func NewCopier(config Config) *Copier {
	ctx, cancel := context.WithCancel(context.Background())

	// Parse detach keys (default to ctrl-p,ctrl-q if not specified)
	detachKeys := parseDetachKeys(config.DetachKeys)
	if len(detachKeys) == 0 && config.DetachKeys == "" {
		// Default to ctrl-p,ctrl-q for TTY mode
		if config.Terminal {
			detachKeys = []byte{16, 17} // ctrl-p, ctrl-q
		}
	}

	// Create cancel pipe for waking up Poll on context cancellation
	// The pipe is non-blocking on write to avoid blocking Stop()
	pipe := make([]int, 2)
	if err := unix.Pipe2(pipe, unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		// Fallback: if pipe creation fails, the worst case is Poll won't be interruptible
		// but the copier will still work (just waits for FIFO events)
		log.Errorf("[IO] Failed to create cancel pipe: %v", err)
		pipe[0], pipe[1] = -1, -1
	}

	return &Copier{
		config:      config,
		ctx:         ctx,
		cancel:      cancel,
		cancelPipeR: pipe[0],
		cancelPipeW: pipe[1],
		epollFd:     -1,
		detachKeys:  detachKeys,
		detachBuf:   make([]byte, 0, 8),    // Buffer for partial detach sequences
		// Enable RTOS echo suppression only in TTY mode to avoid double echo:
		// - TTY mode: PTY echoes locally, so we suppress RTOS echo
		// - Non-TTY mode: user terminal does NOT echo locally, we need RTOS echo
		suppressEcho: config.Terminal,
		sentChars:    make([]byte, 0, 256), // Buffer for tracking sent characters
	}
}

// SetStdoutFifoForEcho sets the stdout FIFO for local echo in TTY mode.
// This is used to echo user input locally before detecting exit/detach command.
func (c *Copier) SetStdoutFifoForEcho(stdout io.WriteCloser) {
	c.stdoutFifoForEcho = stdout
}

// initEpoll initializes epoll for waiting on TTY data without busy polling.
// It adds the TTY fd and cancel pipe to the epoll set.
func (c *Copier) initEpoll(ttyFd int) error {
	// Create epoll instance
	epfd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return fmt.Errorf("epoll_create1 failed: %w", err)
	}
	c.epollFd = epfd

	// Add TTY fd to epoll for read events (edge-triggered for one-shot notification)
	event := unix.EpollEvent{
		Events: unix.EPOLLIN | unix.EPOLLET,
		Fd:     int32(ttyFd),
	}
	if err := unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, ttyFd, &event); err != nil {
		unix.Close(epfd)
		c.epollFd = -1
		return fmt.Errorf("epoll_ctl add TTY fd failed: %w", err)
	}

	// Add cancel pipe to epoll for wakeup (level-triggered)
	event = unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(c.cancelPipeR),
	}
	if c.cancelPipeR >= 0 {
		if err := unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, c.cancelPipeR, &event); err != nil {
			unix.Close(epfd)
			c.epollFd = -1
			return fmt.Errorf("epoll_ctl add cancel pipe failed: %w", err)
		}
	}

	log.Tracef("[IO] epoll initialized: fd=%d, ttyFd=%d, cancelPipeR=%d", epfd, ttyFd, c.cancelPipeR)
	return nil
}

// waitForData waits for data to be available on the TTY using epoll.
// Returns true if data is ready to read, false if context was canceled.
// Uses a short timeout (100ms) to periodically check context for fast cancellation response.
func (c *Copier) waitForData(ttyFd int) bool {
	// Initialize epoll on first call
	if c.epollFd < 0 {
		if err := c.initEpoll(ttyFd); err != nil {
			log.Errorf("[IO] Failed to init epoll, falling back to non-blocking poll: %v", err)
			// Fallback: just return true to try reading immediately
			return true
		}
	}

	const maxEvents = 4
	events := make([]unix.EpollEvent, maxEvents)

	// Loop with short timeout to ensure fast response to context cancellation
	// 100ms timeout balances: zero-CPU idle, fast cancellation (<100ms response time)
	const epollTimeoutMs = 100

	for {
		// Check if context is already canceled BEFORE epoll_wait
		select {
		case <-c.ctx.Done():
			log.Tracef("[IO] waitForData: context canceled before epoll_wait")
			return false
		default:
		}

		// Block until data is available, timeout, or cancel pipe event
		n, err := unix.EpollWait(c.epollFd, events, epollTimeoutMs)
		if err != nil {
			if err == unix.EINTR {
				// Interrupted by signal, check context and retry
				select {
				case <-c.ctx.Done():
					return false
				default:
					continue
				}
			}
			log.Errorf("[IO] epoll_wait failed: %v", err)
			return true // Continue despite error
		}

		// Check if context was canceled while waiting
		select {
		case <-c.ctx.Done():
			log.Tracef("[IO] waitForData: context canceled after epoll_wait")
			return false
		default:
		}

		// No events: timeout occurred, check context again at top of loop
		if n == 0 {
			continue
		}

		// Check if any event is ready
		for i := 0; i < n; i++ {
			if events[i].Fd == int32(c.cancelPipeR) {
				// Cancel pipe is readable - context was canceled
				// Drain the pipe (use pre-allocated buffer to avoid repeated allocations)
				for {
					_, err := unix.Read(c.cancelPipeR, c.drainBuf[:])
					if err != nil {
						break
					}
				}
				return false
			}
			if events[i].Fd == int32(ttyFd) && (events[i].Events&unix.EPOLLIN) != 0 {
				// TTY has data to read (no log - high frequency)
				return true
			}
		}
	}

	// Timeout or no relevant events - check context and retry
	select {
	case <-c.ctx.Done():
		return false
	default:
		return true // Try reading anyway (might have spurious wakeup)
	}
}

// initStdinEpoll initializes epoll for waiting on stdin FIFO without busy polling.
// This is used in copyStdin to wait for attach client connection.
// Uses stdinEpollFd (separate from epollFd used for TTY/stdout).
func (c *Copier) initStdinEpoll(stdinFd int) error {
	// Create epoll instance
	epfd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return fmt.Errorf("epoll_create1 failed: %w", err)
	}
	c.stdinEpollFd = epfd

	// Add stdin FIFO to epoll for read events (level-triggered for immediate detection)
	event := unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(stdinFd),
	}
	if err := unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, stdinFd, &event); err != nil {
		unix.Close(epfd)
		c.stdinEpollFd = -1
		return fmt.Errorf("epoll_ctl add stdin fd failed: %w", err)
	}

	// Add cancel pipe to epoll for wakeup (level-triggered)
	event = unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(c.cancelPipeR),
	}
	if c.cancelPipeR >= 0 {
		if err := unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, c.cancelPipeR, &event); err != nil {
			unix.Close(epfd)
			c.stdinEpollFd = -1
			return fmt.Errorf("epoll_ctl add cancel pipe failed: %w", err)
		}
	}

	log.Tracef("[IO] stdin epoll initialized: fd=%d, stdinFd=%d, cancelPipeR=%d", epfd, stdinFd, c.cancelPipeR)
	return nil
}

// waitForStdinOrCancel waits for stdin FIFO to be readable or context cancellation.
// Returns true if data is ready to read, false if context was canceled.
// This replaces the 100ms sleep in copyStdin for zero-CPU waiting.
// Uses stdinEpollFd (separate from epollFd used for TTY/stdout).
func (c *Copier) waitForStdinOrCancel(stdinFd int) bool {
	// If epoll was previously disabled due to errors, use sleep-based polling
	if c.stdinEpollDisabled {
		time.Sleep(100 * time.Millisecond)
		select {
		case <-c.ctx.Done():
			return false
		default:
			return true
		}
	}

	// Initialize epoll on first call
	if c.stdinEpollFd < 0 {
		if err := c.initStdinEpoll(stdinFd); err != nil {
			log.Warnf("[IO] Failed to init stdin epoll, disabling and falling back to sleep: %v", err)
			c.stdinEpollDisabled = true
			// Fallback: use 100ms sleep
			time.Sleep(100 * time.Millisecond)
			select {
			case <-c.ctx.Done():
				return false
			default:
				return true
			}
		}
	}

	const maxEvents = 4
	events := make([]unix.EpollEvent, maxEvents)

	// Loop with short timeout to ensure fast response to context cancellation
	const epollTimeoutMs = 100

	for {
		// Check if context is already canceled BEFORE epoll_wait
		select {
		case <-c.ctx.Done():
			log.Tracef("[IO] waitForStdinOrCancel: context canceled before epoll_wait")
			return false
		default:
		}

		// Block until data is available, timeout, or cancel pipe event
		n, err := unix.EpollWait(c.stdinEpollFd, events, epollTimeoutMs)
		if err != nil {
			if err == unix.EINTR {
				select {
				case <-c.ctx.Done():
					return false
				default:
					continue
				}
			}
			// epoll_wait failed with EINVAL or other error
			// This can happen when the stdin FIFO fd is in a bad state (no writer)
			// Close the epoll fd, disable epoll, and fall back to sleep-based polling
			log.Warnf("[IO] stdin epoll_wait failed: %v, disabling epoll and falling back to sleep", err)
			c.stdinEpollDisabled = true
			if c.stdinEpollFd >= 0 {
				unix.Close(c.stdinEpollFd)
				c.stdinEpollFd = -1
			}
			// Fall back to sleep-based waiting
			time.Sleep(100 * time.Millisecond)
			select {
			case <-c.ctx.Done():
				return false
			default:
				return true
			}
		}

		// Check if context was canceled while waiting
		select {
		case <-c.ctx.Done():
			log.Tracef("[IO] waitForStdinOrCancel: context canceled after epoll_wait")
			return false
		default:
		}

		// No events: timeout occurred, check context again at top of loop
		if n == 0 {
			continue
		}

		// Check if any event is ready
		for i := 0; i < n; i++ {
			if events[i].Fd == int32(c.cancelPipeR) {
				// Cancel pipe is readable - context was canceled
				// Drain the pipe (use pre-allocated buffer)
				for {
					_, err := unix.Read(c.cancelPipeR, c.drainBuf[:])
					if err != nil {
						break
					}
				}
				return false
			}
			if events[i].Fd == int32(stdinFd) && (events[i].Events&unix.EPOLLIN) != 0 {
				// stdin FIFO has data to read (no log - high frequency)
				return true
			}
		}
	}
}

// isExitCommand checks if the given line is an "exit" command.
// It handles variations like "exit", "exit ", " exit", etc.
// Returns true if the trimmed line equals "exit" (case-insensitive).
func isExitCommand(line []byte) bool {
	// Trim leading and trailing whitespace
	trimmed := trimSpace(line)
	// Debug logging
	log.Tracef("[IO] isExitCommand: input=%q (%d bytes), trimmed=%q (%d bytes)",
		string(line), len(line), string(trimmed), len(trimmed))
	// Check case-insensitively for "exit"
	if len(trimmed) == 4 {
		c := string(trimmed)
		result := c == "exit" || c == "EXIT" || c == "Exit"
		log.Tracef("[IO] isExitCommand: len=4, string=%q, result=%v", c, result)
		return result
	}
	log.Tracef("[IO] isExitCommand: len=%d, returning false", len(trimmed))
	return false
}

// trimSpace trims leading and trailing whitespace from a byte slice.
// It handles spaces, tabs, newlines, and carriage returns.
func trimSpace(b []byte) []byte {
	start := 0
	end := len(b)
	// Trim leading whitespace (space, tab, newline, carriage return)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\n' || b[start] == '\r') {
		start++
	}
	// Trim trailing whitespace
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\n' || b[end-1] == '\r') {
		end--
	}
	result := b[start:end]
	log.Tracef("[IO] trimSpace: input=%q -> result=%q (start=%d, end=%d)", string(b), string(result), start, end)
	return result
}

// publishEvent publishes an event to the event bus.
func (c *Copier) publishEvent(typ EventType, data interface{}) {
	if c.config.EventBus != nil {
		c.config.EventBus.Publish(Event{
			Type:        typ,
			ContainerID: c.config.ContainerID,
			Data:        data,
		})
	}
}

// Start begins the IO copying.
func (c *Copier) Start() error {
	log.Infof("[IO] Starting copier for %s", c.config.ContainerID)

	// Start stdin → TTY copier
	if c.config.StdinFIFO != "" && c.ttyIn != nil {
		log.Debugf("[IO] Starting stdin→TTY copier goroutine for %s", c.config.ContainerID)
		c.wg.Add(1)
		go c.copyStdin()
	} else {
		log.Debugf("[IO] Skipping stdin→TTY copier: StdinFIFO=%q, ttyIn=%v", c.config.ContainerID, c.config.StdinFIFO, c.ttyIn != nil)
	}

	// Check if ttyOut and ttyErr point to the same file descriptor
	// This happens when stdout and stderr are merged (common in RTOS/mica clients)
	// When merged, we must use a single copier to avoid race conditions where
	// two goroutines compete to read from the same fd, causing data loss
	sameOutErr := false
	var outFd, errFd uintptr
	if c.ttyOut != nil && c.ttyErr != nil {
		if outFdObj, ok := c.ttyOut.(interface{ Fd() uintptr }); ok {
			outFd = outFdObj.Fd()
			if errFdObj, ok := c.ttyErr.(interface{ Fd() uintptr }); ok {
				errFd = errFdObj.Fd()
				sameOutErr = (outFd == errFd)
				if sameOutErr {
					log.Infof("[IO] Detected merged stdout/stderr (fd=%d), using unified copier for %s", outFd, c.config.ContainerID)
				}
			}
		}
	}

	if sameOutErr {
		// stdout and stderr are merged - use single unified copier
		// This prevents race condition where two goroutines compete for the same fd
		c.wg.Add(1)
		go c.copyStdoutErrUnified()
	} else {
		// Start separate TTY → stdout and TTY → stderr copiers
		// IMPORTANT: In Non-TTY detached mode, stdout FIFO may not have readers initially
		// We start the copier anyway, but handle write failures gracefully
		if c.config.StdoutFIFO != "" && c.ttyOut != nil {
			log.Debugf("[IO] Starting TTY→stdout copier goroutine for %s", c.config.ContainerID)
			c.wg.Add(1)
			go c.copyStdout()
		} else {
			log.Debugf("[IO] Skipping TTY→stdout copier: StdoutFIFO=%q, ttyOut=%v", c.config.ContainerID, c.config.StdoutFIFO, c.ttyOut != nil)
		}

		// Start TTY → stderr copier (if applicable)
		if c.config.StderrFIFO != "" && c.ttyErr != nil {
			log.Debugf("[IO] Starting TTY→stderr copier goroutine for %s", c.config.ContainerID)
			c.wg.Add(1)
			go c.copyStderr()
		} else {
			log.Debugf("[IO] Skipping TTY→stderr copier: StderrFIFO=%q, ttyErr=%v", c.config.ContainerID, c.config.StderrFIFO, c.ttyErr != nil)
		}
	}

	log.Infof("[IO] Copier started for %s", c.config.ContainerID)
	return nil
}

// Stop gracefully stops the copier.
func (c *Copier) Stop() {
	if c.stopped.CompareAndSwap(false, true) {
		log.Infof("[IO] [1/12] Stopping copier for %s", c.config.ContainerID)

		// Wake up any blocked Poll calls by closing cancel pipe write end
		log.Debugf("[IO] [2/12] About to close cancel pipe for %s", c.config.ContainerID)
		if c.cancelPipeW >= 0 {
			unix.Close(c.cancelPipeW)
			c.cancelPipeW = -1
		}
		log.Debugf("[IO] [3/12] Cancel pipe closed for %s", c.config.ContainerID)

		// Cancel context to signal goroutines to exit
		c.cancel()
		log.Debugf("[IO] [4/12] Context canceled for %s", c.config.ContainerID)

		// Close FIFOs FIRST to unblock any FIFO writes
		// This is critical: if attach client has stopped reading, FIFO writes will block
		// Closing FIFOs interrupts any in-progress writes, allowing goroutines to exit
		log.Debugf("[IO] [5/12] Closing FIFOs for %s", c.config.ContainerID)
		c.closeFIFOs()
		log.Debugf("[IO] [6/12] FIFOs closed for %s", c.config.ContainerID)

		// Close TTY readers to unblock goroutines from TTY reads
		// This ensures copyStdout and copyStderr exit their Read() calls
		log.Debugf("[IO] [7/12] Closing TTY readers for %s", c.config.ContainerID)
		if c.ttyOut != nil {
			if closer, ok := c.ttyOut.(io.Closer); ok {
				closer.Close()
			}
		}
		if c.ttyErr != nil {
			if closer, ok := c.ttyErr.(io.Closer); ok {
				closer.Close()
			}
		}
		log.Debugf("[IO] [8/12] TTY readers closed for %s", c.config.ContainerID)

		// Close TTY write end to stop sending data to RTOS
		if c.ttyIn != nil {
			c.ttyIn.Close()
		}
		log.Debugf("[IO] [9/12] TTY write end closed for %s", c.config.ContainerID)

		// Wait for goroutines with a timeout
		// Note: We don't wait indefinitely because FIFO writes might be stuck in kernel
		log.Debugf("[IO] [10/12] Waiting for goroutines to exit for %s", c.config.ContainerID)
		done := make(chan struct{})
		go func() {
			c.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
			log.Debugf("[IO] [11/12] Goroutines exited for %s", c.config.ContainerID)
		case <-time.After(500 * time.Millisecond):
			log.Warnf("[IO] [11/12] Goroutines timeout waiting for %s, continuing anyway", c.config.ContainerID)
		}

		// Clean up epoll fd
		if c.epollFd >= 0 {
			unix.Close(c.epollFd)
			c.epollFd = -1
		}

		// Clean up stdin epoll fd (separate from TTY epoll)
		if c.stdinEpollFd >= 0 {
			unix.Close(c.stdinEpollFd)
			c.stdinEpollFd = -1
		}

		// Clean up cancel pipe read end
		if c.cancelPipeR >= 0 {
			unix.Close(c.cancelPipeR)
			c.cancelPipeR = -1
		}

		log.Infof("[IO] [12/12] Copier stopped for %s", c.config.ContainerID)
	}
}

// StopWithoutClosingFIFOs stops the copier but keeps FIFOs open for reattach.
// This is used for detach (Ctrl+P Ctrl+Q) to allow later reconnection.
//
// IMPORTANT: We also keep TTYs open because they are managed by the sandbox
// and need to be reused for reattach. The TTYs will be closed when the
// container is deleted, not when the user detaches.
func (c *Copier) StopWithoutClosingFIFOs() {
	if c.stopped.CompareAndSwap(false, true) {
		log.Infof("[IO] Stopping copier for %s (keeping FIFOs and TTYs for reattach)", c.config.ContainerID)

		// Wake up any blocked Poll calls by closing cancel pipe write end
		if c.cancelPipeW >= 0 {
			unix.Close(c.cancelPipeW)
			c.cancelPipeW = -1
		}

		// NOTE: We do NOT close TTYs here because:
		// 1. TTYs are managed by the sandbox, not the copier
		// 2. TTYs need to remain open for reattach after detach
		// 3. Closing TTYs here causes "file already closed" errors on reattach

		// Cancel context to signal goroutines to exit
		c.cancel()

		// Wait for goroutines to finish
		c.wg.Wait()

		// Clean up epoll fd
		if c.epollFd >= 0 {
			unix.Close(c.epollFd)
			c.epollFd = -1
		}

		// Clean up stdin epoll fd (separate from TTY epoll)
		if c.stdinEpollFd >= 0 {
			unix.Close(c.stdinEpollFd)
			c.stdinEpollFd = -1
		}

		// Clean up cancel pipe read end
		if c.cancelPipeR >= 0 {
			unix.Close(c.cancelPipeR)
			c.cancelPipeR = -1
		}

		// NOTE: We do NOT close FIFOs here
		// The FIFOs remain open for potential reattach
		// They will be closed when:
		// 1. A new attach creates new FIFOs
		// 2. The container is deleted

		log.Infof("[IO] Copier stopped for %s (FIFOs preserved)", c.config.ContainerID)
	}
}

func (c *Copier) closeFIFOs() {
	if c.stdinFifo != nil {
		c.stdinFifo.Close()
	}
	if c.stdoutFIFO != nil {
		c.stdoutFIFO.Close()
	}
	if c.stderrFIFO != nil {
		c.stderrFIFO.Close()
	}
}

// copyStdin copies from stdin FIFO to TTY.
//
// TTY mode (terminal=true): Character-by-character processing for exit/detach detection
// and backspace handling. Local echo is enabled.
//
// Non-TTY mode (terminal=false): Direct passthrough with \n -> \r\n conversion.
// No local echo, no exit/detach detection (suitable for scripting/automation).
func (c *Copier) copyStdin() {
	defer c.wg.Done()

	buf := make([]byte, c.config.StdinBufSize)

	log.Infof("[IO] stdin→TTY copier started for %s (terminal=%v)", c.config.ContainerID, c.config.Terminal)

	for {
		n, err := c.stdinFifo.Read(buf)
		// Only log errors and significant events, not every read (high-frequency)
		if err != nil && !isClosed(err) && err != io.EOF && !isEAGAIN(err) {
			log.Tracef("[IO] stdin FIFO read: n=%d, err=%v", n, err)
		}
		if err != nil {
			// Check for EOF or closed
			if isClosed(err) || err == io.EOF {
				// Check if context is cancelled FIRST
				// This handles the back command case where context is canceled
				select {
				case <-c.ctx.Done():
					log.Infof("[IO] stdin→TTY copier canceled for %s (exiting on EOF)", c.config.ContainerID)
					return
				default:
				}

				// If attach client has connected and sent data, EOF means client is done - exit
				if c.attachClientConnected {
					log.Infof("[IO] stdin EOF for %s (attach client closed stdin, exiting)", c.config.ContainerID)
					c.publishEvent(StdinClosed, nil)
					return
				}

				// No attach client yet, this is initial EOF - wait for attach
				if !c.stdinEOFSeen {
					log.Infof("[IO] stdin EOF for %s (no attach client yet, waiting)", c.config.ContainerID)
					c.stdinEOFSeen = true
				}
				// Use epoll to wait for stdin FIFO to become readable (zero-CPU)
				stdinFd := -1
				if fdObj, ok := c.stdinFifo.(interface{ Fd() uintptr }); ok {
					stdinFd = int(fdObj.Fd())
				}
				if !c.waitForStdinOrCancel(stdinFd) {
					// Context was canceled
					return
				}
				continue
			}
			// Check for EAGAIN (non-blocking read with no data)
			if isEAGAIN(err) {
				// Reset EOF state when we get EAGAIN after EOF (writer may have connected)
				if c.stdinEOFSeen {
					log.Infof("[IO] stdin writer detected for %s (reattach detected)", c.config.ContainerID)
					c.stdinEOFSeen = false
					// Re-enable epoll now that a writer is connected
					// Close old epoll fd so it gets re-initialized fresh
					if c.stdinEpollDisabled {
						log.Infof("[IO] Re-enabling stdin epoll for %s (writer connected)", c.config.ContainerID)
						c.stdinEpollDisabled = false
						if c.stdinEpollFd >= 0 {
							unix.Close(c.stdinEpollFd)
							c.stdinEpollFd = -1
						}
					}
				}
				continue
			}
			log.Errorf("[IO] stdin read error for %s: %v", c.config.ContainerID, err)
			c.publishEvent(IOError, err)
			return
		}

		if n == 0 {
			continue
		}

		// Reset EOF state when we successfully read data
		if c.stdinEOFSeen {
			log.Infof("[IO] stdin data received for %s (writer connected)", c.config.ContainerID)
			c.stdinEOFSeen = false
			// Mark that attach client has connected and sent data
			// This helps us distinguish between "initial EOF" vs "attach client done"
			if !c.attachClientConnected {
				log.Infof("[IO] Attach client connected for %s", c.config.ContainerID)
				c.attachClientConnected = true
			}
			// Re-enable epoll now that a writer is connected
			if c.stdinEpollDisabled {
				log.Infof("[IO] Re-enabling stdin epoll for %s (data received, writer connected)", c.config.ContainerID)
				c.stdinEpollDisabled = false
				if c.stdinEpollFd >= 0 {
					unix.Close(c.stdinEpollFd)
					c.stdinEpollFd = -1
				}
			}
		} else if n > 0 && !c.attachClientConnected {
			// First data received - attach client is now connected
			log.Infof("[IO] First data received for %s (attach client connected)", c.config.ContainerID)
			c.attachClientConnected = true
			// Also re-enable epoll on first data
			if c.stdinEpollDisabled {
				log.Infof("[IO] Re-enabling stdin epoll for %s (first data received)", c.config.ContainerID)
				c.stdinEpollDisabled = false
				if c.stdinEpollFd >= 0 {
					unix.Close(c.stdinEpollFd)
					c.stdinEpollFd = -1
				}
			}
		}

		// Use different processing logic for TTY vs non-TTY mode
		if c.config.Terminal {
			c.copyStdinTTY(buf[:n])
		} else {
			c.copyStdinNonTTY(buf[:n])
		}
	}
}

// copyStdinTTY handles TTY mode stdin processing with character-by-character
// handling for exit command detection and backspace support.
//
// Uses lookahead detection: checks after each character if the buffer
// matches "exit", and if so at line end, stops without having sent it to TTY.
func (c *Copier) copyStdinTTY(data []byte) {
	for i := 0; i < len(data); i++ {
		ch := data[i]

		// Check for detach sequence (e.g., Ctrl+P, Ctrl+Q)
		// Ctrl+P = 16, Ctrl+Q = 17
		if len(c.detachKeys) > 0 && c.checkDetachSequence(ch) {
			log.Infof("[IO] Detach sequence detected for %s", c.config.ContainerID)
			// Publish detach event for shim to handle
			c.publishEvent(DetachDetected, nil)
			c.lineBuf = c.lineBuf[:0] // Clear line buffer
			c.detachBuf = c.detachBuf[:0]
			c.detachPending = false
			// Stop IO copier but don't send exit to RTOS
			c.Stop()
			return
		}

		// Handle backspace (BS 0x08 or DEL 0x7f)
		// Send to TTY for proper backspace handling
		if ch == 0x08 || ch == 0x7f {
			// Send backspace to TTY
			if _, err := c.ttyIn.Write([]byte{ch}); err != nil {
				log.Errorf("[IO] TTY write error for backspace: %v", err)
			}
			// Also remove from local line buffer for exit command detection
			if len(c.lineBuf) > 0 {
				c.lineBuf = c.lineBuf[:len(c.lineBuf)-1]
				// Local echo: send backspace sequence to stdout FIFO
				// \b \b moves cursor back, writes space, moves back again (erases character)
				if c.stdoutFifoForEcho != nil {
					c.stdoutFifoForEcho.Write([]byte{'\b', ' ', '\b'})
				}
			}
			continue
		}

		// Handle \r\n sequence to avoid duplicate command processing
		// When terminal sends \r\n, we want to:
		// 1. Send both characters to TTY
		// 2. But only trigger command processing once
		originalCh := ch
		isLineEnd := false
		if ch == '\r' {
			// CR is always a line end in TTY mode
			// Set flag to skip duplicate processing if \n follows
			c.prevWasCR = true
			isLineEnd = true
		} else if ch == '\n' {
			if c.prevWasCR {
				// This is \r\n sequence - already processed as line end at \r
				// Just clear the flag, don't trigger command processing again
				c.prevWasCR = false
				isLineEnd = false // Skip processing, already done at \r
			} else {
				// Standalone \n - also line end
				isLineEnd = true
			}
		} else {
			c.prevWasCR = false
		}

		// At line end, check if buffer is "exit" BEFORE sending line ending
		if isLineEnd {
			// Remove line ending chars from buffer for command check
			lineWithoutTerm := c.lineBuf
			for len(lineWithoutTerm) > 0 && (lineWithoutTerm[len(lineWithoutTerm)-1] == '\r' || lineWithoutTerm[len(lineWithoutTerm)-1] == '\n') {
				lineWithoutTerm = lineWithoutTerm[:len(lineWithoutTerm)-1]
			}

			log.Tracef("[IO] TTY: lineWithoutTerm=%q (len=%d)", string(lineWithoutTerm), len(lineWithoutTerm))

			// Check for "exit" command - stop BEFORE sending line ending to RTOS
			if isExitCommand(lineWithoutTerm) {
				log.Infof("[IO] 'exit' command detected for %s, stopping IO copier", c.config.ContainerID)

				// Send newline to stdout FIFO for clean exit display
				if c.stdoutFIFO != nil {
					c.stdoutFIFO.Write([]byte{'\r', '\n'})
				}

				c.publishEvent(ExitCommandDetected, nil)
				c.lineBuf = c.lineBuf[:0]
				c.Stop()
				return
			}

			// Not exit command - clear line buffer for next command
			// Skip adding line ending chars (\r, \n) to lineBuf - they will still be sent to TTY below
			c.lineBuf = c.lineBuf[:0]
		} else {
			// Only add non-line-ending chars to line buffer
			c.lineBuf = append(c.lineBuf, originalCh)
		}

		// Local echo: write printable characters to stdout FIFO for immediate display
		// This provides instant feedback without relying on unreliable RTOS echo
		if c.stdoutFifoForEcho != nil {
			if _, err := c.stdoutFifoForEcho.Write([]byte{originalCh}); err != nil {
				log.Infof("[IO] Local echo FAILED for %s: %v", c.config.ContainerID, err)
			}
		}

		// Send original character to TTY immediately for echo
		// Note: "exit" characters are sent here, but line ending is intercepted above
		if c.ttyIn == nil {
			log.Errorf("[IO] TTY write FAILED for %s: ttyIn is nil!", c.config.ContainerID)
			return
		}
		n, err := c.ttyIn.Write([]byte{originalCh})
		if err != nil {
			log.Errorf("[IO] TTY write error for %s: %v", c.config.ContainerID, err)
			return
		}
		log.Tracef("[IO] TTY write OK for %s: wrote %d bytes (ch=%d %q)", c.config.ContainerID, n, originalCh, originalCh)

		// Track sent characters for echo suppression (in TTY mode)
		// This helps filter out RTOS echo to avoid double echo
		if c.suppressEcho && originalCh != '\r' && originalCh != '\n' {
			// Only track printable characters for echo suppression
			// Line endings are not echoed by RTOS, so we don't need to suppress them
			c.sentChars = append(c.sentChars, originalCh)
			// Limit buffer size to prevent unbounded growth
			if len(c.sentChars) > 256 {
				// Keep only the most recent 128 chars
				copy(c.sentChars, c.sentChars[128:])
				c.sentChars = c.sentChars[:256]
				if c.sentCharsCursor >= 128 {
					c.sentCharsCursor -= 128
				}
			}
			log.Tracef("[IO] Tracking sent char: %d (%q), total tracked: %d", originalCh, originalCh, len(c.sentChars))
		}
	}
}

// checkDetachSequence checks if the character completes the detach sequence.
// Returns true if detach sequence is detected.
func (c *Copier) checkDetachSequence(ch byte) bool {
	// Debug logging for all detach sequence checks
	log.Tracef("[IO] checkDetachSequence: ch=%d (%q), detachPending=%v, detachKeys=%v",
		ch, ch, c.detachPending, c.detachKeys)

	// Fast path: first character must match first key
	if !c.detachPending {
		if len(c.detachKeys) > 0 && ch == c.detachKeys[0] {
			// First character matches, start tracking
			c.detachPending = true
			c.detachBuf = append(c.detachBuf[:0], ch)
			log.Infof("[IO] Detach sequence START (ch=%d matched detachKeys[0]=d), pending=true",
				ch, c.detachKeys[0])
			// Don't echo the detach character
			if c.stdoutFifoForEcho != nil && len(c.detachKeys) > 1 {
				// Backspace the character we just echoed
				c.stdoutFifoForEcho.Write([]byte{'\b', ' ', '\b'})
			}
			return len(c.detachKeys) == 1 // Single key detach sequence
		}
		log.Tracef("[IO] checkDetachSequence: ch=%d doesn't match detachKeys[0]=d, continuing",
			ch, c.detachKeys[0])
		return false
	}

	// We have a pending partial match, check if this continues the sequence
	c.detachBuf = append(c.detachBuf, ch)
	log.Tracef("[IO] Detach pending: added ch=%d to detachBuf, now %v",
		ch, c.detachBuf)
	if len(c.detachBuf) >= len(c.detachKeys) {
		// Check if we've matched the full sequence
		matched := true
		for i := 0; i < len(c.detachKeys); i++ {
			if c.detachBuf[len(c.detachBuf)-len(c.detachKeys)+i] != c.detachKeys[i] {
				matched = false
				break
			}
		}
		if matched {
			log.Infof("[IO] Detach sequence COMPLETE detected")
			// Backspace the second character too
			if c.stdoutFifoForEcho != nil && len(c.detachKeys) > 1 {
				c.stdoutFifoForEcho.Write([]byte{'\b', ' ', '\b'})
			}
			return true
		}
		log.Tracef("[IO] Detach sequence partial match didn't complete")
	}

	// Sequence didn't match, flush the buffer to TTY and reset
	// But we need to send the pending characters first
	if len(c.detachBuf) > 0 {
		log.Infof("[IO] Detach sequence didn't match, flushing %d chars to TTY: %v",
			len(c.detachBuf), c.detachBuf)
		c.ttyIn.Write(c.detachBuf)
	}
	c.detachBuf = c.detachBuf[:0]
	c.detachPending = false
	log.Tracef("[IO] Detach sequence reset, pending=false")
	return false
}

// isExitCommandWithNewline checks if the given data ends with "exit" command followed by newline.
// This is used for non-TTY mode where data arrives in chunks.
// Handles case-insensitive "exit" with optional leading/trailing whitespace.
func isExitCommandWithNewline(data []byte) bool {
	// Find the last newline in the data
	lastNewline := -1
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] == '\n' {
			lastNewline = i
			break
		}
	}
	if lastNewline == -1 {
		return false // No newline found
	}

	// Extract the line before the newline (excluding the newline itself)
	line := data[:lastNewline]
	trimmed := trimSpace(line)

	// Check case-insensitively for "exit"
	if len(trimmed) == 4 {
		c := string(trimmed)
		return c == "exit" || c == "EXIT" || c == "Exit"
	}
	return false
}

// copyStdinNonTTY handles non-TTY mode stdin processing with direct passthrough.
// Converts LF to CRLF since RTOS expects \r\n for line endings.
// Supports exit command detection for non-TTY mode.
//
// Note: Detach (like Ctrl+P Ctrl+Q) is NOT supported in non-TTY mode.
// Use TTY mode for detach support, as nerdctl/ctr non-TTY attach
// has no detach mechanism and will wait until container exits.
func (c *Copier) copyStdinNonTTY(data []byte) {
	log.Infof("[IO] Non-TTY: received data: %q (%d bytes)", string(data), len(data))
	// Debug: print hex data for diagnosis
	log.Tracef("[IO] Non-TTY: hex data: %s", hexDump(data))

	// Split data by newlines to handle multi-line input properly
	// This allows detection of "exit" commands even when multiple lines
	// are read in a single FIFO read
	lines := bytes.Split(data, []byte{'\n'})

	for i, line := range lines {
		// Check if this is the last line and if it's empty (trailing \n)
		// We don't process empty trailing lines
		isLastLine := i == len(lines)-1
		if isLastLine && len(line) == 0 {
			// Skip empty trailing line from final \n
			continue
		}

		// Add back the newline that was removed by Split
		if !isLastLine {
			// Not the last line, original had \n
			lineWithNewline := append(line, '\n')
			line = lineWithNewline
		}

		// Check for "exit" command (stop container)
		// Note: "back" command is NOT supported in non-TTY mode because
		// nerdctl/ctr non-TTY attach has no detach mechanism - it will
		// wait forever until the container exits. Use TTY mode (Ctrl+P Ctrl+Q)
		// for detach support.
		if isExitCommandWithNewline(line) {
			log.Infof("[IO] Non-TTY: 'exit' command detected for %s", c.config.ContainerID)
			// Publish exit command event for shim to handle
			c.publishEvent(ExitCommandDetected, nil)
			// Stop IO copier
			c.Stop()
			return
		}

		// For non-TTY mode, we need to convert \n to \r\n because:
		// 1. Linux/ctr sends \n for newline
		// 2. RTOS shell expects \r\n for proper command processing
		converted := c.convertLFToCRLF(line)

		// Track sent characters for echo suppression (in interactive mode)
		// This helps filter out RTOS echo to avoid double echo
		if c.suppressEcho {
			// Only track printable characters (excluding control chars like \r, \n)
			for _, ch := range line {
				if ch >= 32 && ch <= 126 { // Printable ASCII range
					c.sentChars = append(c.sentChars, ch)
					// Limit buffer size
					if len(c.sentChars) > 256 {
						copy(c.sentChars, c.sentChars[128:])
						c.sentChars = c.sentChars[:256]
					}
				}
			}
		}

		// Log the data being sent for debugging
		log.Debugf("[IO] Non-TTY: sending %q (%d bytes) to TTY", string(converted), len(converted))

		if _, err := c.ttyIn.Write(converted); err != nil {
			log.Errorf("[IO] TTY write error for %s: %v", c.config.ContainerID, err)
			return
		}

		log.Debugf("[IO] Non-TTY: wrote %d bytes (converted from %d)", len(converted), len(line))
	}
}

// convertLFToCRLF converts standalone LF (\n) to CRLF (\r\n).
// This is needed for non-TTY mode where clients send \n but RTOS expects \r\n.
// Preserves existing CRLF sequences to avoid double conversion.
func (c *Copier) convertLFToCRLF(data []byte) []byte {
	// First pass: check if we need any conversion
	needsConversion := false
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' && (i == 0 || data[i-1] != '\r') {
			needsConversion = true
			break
		}
	}

	if !needsConversion {
		return data
	}

	// Second pass: do the conversion
	result := make([]byte, 0, len(data)*2)
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' && (i == 0 || data[i-1] != '\r') {
			// Standalone LF, convert to CRLF
			result = append(result, '\r', '\n')
		} else {
			result = append(result, data[i])
		}
	}
	return result
}

// copyStdout copies from TTY to stdout FIFO.
// Uses epoll for zero-CPU idle waiting when no data is available.
func (c *Copier) copyStdout() {
	defer c.wg.Done()

	log.Infof("[IO] TTY→stdout copier started for %s", c.config.ContainerID)

	buf := make([]byte, c.config.StdoutBufSize)
	filterBuf := make([]byte, 0, c.config.StdoutBufSize)
	totalRead := 0

	// Get TTY file descriptor for epoll
	ttyFd := -1
	if fdObj, ok := c.ttyOut.(interface{ Fd() uintptr }); ok {
		ttyFd = int(fdObj.Fd())
	} else {
		log.Warnf("[IO] TTY→stdout: TTY does not support Fd(), falling back to busy polling for %s", c.config.ContainerID)
	}

	for {
		// Check if context is canceled
		select {
		case <-c.ctx.Done():
			log.Infof("[IO] TTY→stdout copier canceled for %s", c.config.ContainerID)
			return
		default:
		}

		// Wait for data to be available using epoll (zero-CPU when idle)
		// If epoll is not available, waitForData falls back to immediate return
		if !c.waitForData(ttyFd) {
			log.Infof("[IO] TTY→stdout copier: waitForData returned false (context canceled) for %s", c.config.ContainerID)
			return
		}

		// Read from the TTY
		// Use unix.Read() for true non-blocking behavior
		// os.File.Read() may internally buffer and block even with O_NONBLOCK flag
		var n int
		var err error
		if ttyFd >= 0 {
			n, err = unix.Read(ttyFd, buf)
		} else {
			n, err = c.ttyOut.Read(buf)
		}

		if err != nil {
			if isClosed(err) || err == io.EOF {
				log.Infof("[IO] TTY stdout closed for %s", c.config.ContainerID)
				return
			}
			if isEAGAIN(err) {
				// Non-blocking read returned no data
				// With epoll, this should rarely happen since we wait for EPOLLIN
				// Just continue to the next iteration (waitForData will handle it)
				continue
			}
			log.Errorf("[IO] TTY read error for %s: %v", c.config.ContainerID, err)
			c.publishEvent(IOError, err)
			return
		}

		if n == 0 {
			continue
		}

		// Publish TTYReady event on first successful read
		if c.ttyReadyPublished.CompareAndSwap(false, true) {
			log.Infof("[IO] TTY ready (first byte received) for %s", c.config.ContainerID)
			c.publishEvent(TTYReady, nil)
		}

		totalRead += n
		// Log first reads for debugging (with hex dump for diagnosis)
		if totalRead <= 500 || n < 20 {
			log.Debugf("[IO] TTY→stdout read %d bytes: %q, hex: %s", n, string(buf[:min(n, 100)]), hexDump(buf[:n]))
		}

		// Filter NUL bytes if enabled
		data := buf[:n]
		if c.config.FilterNUL {
			filterBuf = filterNUL(filterBuf[:0], data)
			data = filterBuf
		}

		// Compress consecutive line endings (handles RTOS firmware that outputs multiple \r\n)
		data = compressLineEndings(data)

		// Suppress echo from RTOS to avoid double echo (PTY + RTOS both echo)
		// In TTY mode, the PTY already echoes locally, so we filter out RTOS echo
		if c.suppressEcho {
			data = c.suppressRTOSEcho(data)
		}

		if len(data) == 0 {
			continue
		}

		// Check if context is canceled before writing to FIFO
		select {
		case <-c.ctx.Done():
			log.Infof("[IO] TTY→stdout copier canceled before FIFO write for %s", c.config.ContainerID)
			return
		default:
		}

		// Write to FIFO (no reopen support - we don't support multiple attach)
		if _, err := c.stdoutFIFO.Write(data); err != nil {
			if isClosed(err) || isBrokenPipe(err) {
				// FIFO closed by client (detach or ctr disconnect)
				if c.config.Terminal {
					// TTY mode: publish disconnect event so shim can clean up
					log.Infof("[IO] stdout FIFO closed by client for %s (TTY mode), stopping copier and publishing disconnect event", c.config.ContainerID)
					c.publishEvent(StdinClosed, nil)
					return
				} else {
					// Non-TTY mode: FIFO may not have readers (detached mode)
					// Don't exit the copier, just log and continue
					// This allows the copier to survive through attach/detach cycles
					log.Infof("[IO] stdout FIFO closed/no reader for %s (Non-TTY detached mode), discarding %d bytes and continuing", c.config.ContainerID, len(data))
					// Continue running, don't exit
					continue
				}
			}
			log.Errorf("[IO] stdout write error for %s: %v", c.config.ContainerID, err)
			c.publishEvent(IOError, err)
			return
		}

		// Add debug log after successful write
		log.Debugf("[IO] TTY→stdout: successfully wrote %d bytes to FIFO for %s", len(data), c.config.ContainerID)
	}
}

// copyStdoutErrUnified copies from a single TTY to both stdout and stderr FIFOs.
// This is used when stdout and stderr are merged (same file descriptor), which is
// common in RTOS/mica clients. Using a single reader prevents race conditions where
// two goroutines would compete to read from the same fd.
// Uses epoll for zero-CPU idle waiting when no data is available.
func (c *Copier) copyStdoutErrUnified() {
	defer c.wg.Done()

	log.Infof("[IO] Unified TTY→stdout+stderr copier started for %s", c.config.ContainerID)
	// Log FIFO state for debugging
	if c.stdoutFIFO == nil {
		log.Warnf("[IO] Unified: stdout FIFO is nil for %s - output will be discarded!", c.config.ContainerID)
	}
	if c.stderrFIFO == nil {
		log.Tracef("[IO] Unified: stderr FIFO is nil for %s (this is OK if merged with stdout)", c.config.ContainerID)
	} else {
		log.Debugf("[IO] Unified: stderr FIFO is set for %s but will NOT be written (unified mode only writes stdout to avoid duplicate output)", c.config.ContainerID)
	}

	buf := make([]byte, c.config.StdoutBufSize)
	filterBuf := make([]byte, 0, c.config.StdoutBufSize)
	totalRead := 0

	// Get TTY file descriptor for epoll
	ttyFd := -1
	if fdObj, ok := c.ttyOut.(interface{ Fd() uintptr }); ok {
		ttyFd = int(fdObj.Fd())
	} else {
		log.Warnf("[IO] Unified copier: TTY does not support Fd(), falling back to busy polling for %s", c.config.ContainerID)
	}

	for {
		// Check if context is canceled
		select {
		case <-c.ctx.Done():
			log.Infof("[IO] Unified copier canceled for %s", c.config.ContainerID)
			return
		default:
		}

		// Wait for data to be available using epoll (zero-CPU when idle)
		// If epoll is not available, waitForData falls back to immediate return
		if !c.waitForData(ttyFd) {
			log.Infof("[IO] Unified copier: waitForData returned false (context canceled) for %s", c.config.ContainerID)
			return
		}

		// Read from the single TTY (ttyOut and ttyErr are the same)
		// Use unix.Read() for true non-blocking behavior
		var n int
		var err error
		if ttyFd >= 0 {
			n, err = unix.Read(ttyFd, buf)
		} else {
			n, err = c.ttyOut.Read(buf)
		}

		if err != nil {
			if isClosed(err) || err == io.EOF {
				log.Infof("[IO] Unified TTY closed for %s", c.config.ContainerID)
				return
			}
			if isEAGAIN(err) {
				// Non-blocking read returned no data
				// With epoll, this should rarely happen since we wait for EPOLLIN
				// But it can happen due to edge-triggered mode or race conditions
				// Just continue to the next iteration (waitForData will handle it)
				continue
			}
			log.Errorf("[IO] Unified TTY read error for %s: %v", c.config.ContainerID, err)
			c.publishEvent(IOError, err)
			return
		}

		if n == 0 {
			continue
		}

		// Publish TTYReady event on first successful read
		if c.ttyReadyPublished.CompareAndSwap(false, true) {
			log.Infof("[IO] TTY ready (first byte received) for %s", c.config.ContainerID)
			c.publishEvent(TTYReady, nil)
		}

		totalRead += n
		// Log first reads for debugging
		if totalRead <= 500 || n < 20 {
			log.Debugf("[IO] Unified TTY read %d bytes: %q for %s", n, string(buf[:min(n, 100)]), c.config.ContainerID)
		}

		// Filter NUL bytes if enabled
		data := buf[:n]
		if c.config.FilterNUL {
			filterBuf = filterNUL(filterBuf[:0], data)
			data = filterBuf
		}

		if len(data) == 0 {
			continue
		}

		// Compress consecutive line endings (for TTY mode)
		if c.config.Terminal {
			data = compressLineEndings(data)
		}

		// Suppress echo from RTOS to avoid double echo (PTY + RTOS both echo)
		// In TTY mode, the PTY already echoes locally, so we filter out RTOS echo
		if c.suppressEcho {
			data = c.suppressRTOSEcho(data)
		}

		if len(data) == 0 {
			continue
		}

		// Write to stdout FIFO
		if c.stdoutFIFO != nil {
			written, err := c.stdoutFIFO.Write(data)
			if err != nil {
				if isClosed(err) || isBrokenPipe(err) {
					if c.config.Terminal {
						log.Warnf("[IO] Unified: stdout FIFO closed by client for %s (TTY mode), stopping", c.config.ContainerID)
						return
					}
					// Non-TTY mode: continue even if stdout FIFO has no reader
					log.Infof("[IO] Unified: stdout FIFO closed/no reader for %s (Non-TTY), continuing", c.config.ContainerID)
				} else if isEAGAIN(err) {
					// Non-blocking write would block - reader might not be ready yet
					log.Warnf("[IO] Unified: stdout FIFO write EAGAIN for %s, reader not ready (wrote %d/%d bytes)", c.config.ContainerID, written, len(data))
					// In TTY mode, this is fatal - we can't deliver output to user
					if c.config.Terminal {
						log.Warnf("[IO] Unified: Stopping due to stdout FIFO EAGAIN in TTY mode for %s", c.config.ContainerID)
						return
					}
				} else {
					log.Errorf("[IO] Unified: stdout write error for %s: %v", c.config.ContainerID, err)
					c.publishEvent(IOError, err)
					return
				}
			} else {
				// Log successful writes (first few for debugging)
				if totalRead <= 100 {
					log.Debugf("[IO] Unified: wrote %d bytes to stdout FIFO for %s", written, c.config.ContainerID)
				}
			}
		}

		// NOTE: In unified copier mode, we only write to stdout FIFO.
		// We skip writing to stderr FIFO because:
		// 1. The data source is the same TTY for both stdout and stderr
		// 2. Writing to both FIFOs causes duplicate output when client reads both
		// 3. Clients that need separate stdout/stderr should not use unified mode
		// The stderr FIFO is still opened to prevent blocking on containerd side,
		// but we don't write data to it.
	}
}

// copyStderr copies from TTY to stderr FIFO.
// Uses epoll for zero-CPU idle waiting when no data is available.
func (c *Copier) copyStderr() {
	defer c.wg.Done()

	buf := make([]byte, c.config.StdoutBufSize)

	log.Infof("[IO] TTY→stderr copier started for %s", c.config.ContainerID)

	// Get TTY file descriptor for epoll
	ttyFd := -1
	if fdObj, ok := c.ttyErr.(interface{ Fd() uintptr }); ok {
		ttyFd = int(fdObj.Fd())
	} else {
		log.Warnf("[IO] TTY→stderr: TTY does not support Fd(), falling back to busy polling for %s", c.config.ContainerID)
	}

	for {
		// Check if context is canceled
		select {
		case <-c.ctx.Done():
			log.Infof("[IO] TTY→stderr copier canceled for %s", c.config.ContainerID)
			return
		default:
		}

		// Wait for data to be available using epoll (zero-CPU when idle)
		// If epoll is not available, waitForData falls back to immediate return
		if !c.waitForData(ttyFd) {
			log.Infof("[IO] TTY→stderr copier: waitForData returned false (context canceled) for %s", c.config.ContainerID)
			return
		}

		// Read from the TTY
		var n int
		var err error
		if ttyFd >= 0 {
			n, err = unix.Read(ttyFd, buf)
		} else {
			n, err = c.ttyErr.Read(buf)
		}

		if err != nil {
			if isClosed(err) || err == io.EOF {
				return
			}
			if isEAGAIN(err) {
				// Non-blocking read returned no data
				// With epoll, this should rarely happen since we wait for EPOLLIN
				// Just continue to the next iteration (waitForData will handle it)
				continue
			}
			log.Errorf("[IO] TTY stderr read error for %s: %v", c.config.ContainerID, err)
			c.publishEvent(IOError, err)
			return
		}

		if n == 0 {
			continue
		}

		// Filter NUL bytes
		data := buf[:n]
		if c.config.FilterNUL {
			data = removeNUL(data)
		}

		if len(data) == 0 {
			continue
		}

		if _, err := c.stderrFIFO.Write(data); err != nil {
			if isClosed(err) || isBrokenPipe(err) {
				// FIFO closed by client (detach or ctr disconnect)
				if c.config.Terminal {
					// TTY mode: publish disconnect event so shim can clean up
					log.Infof("[IO] stderr FIFO closed by client for %s (TTY mode), stopping copier and publishing disconnect event", c.config.ContainerID)
					c.publishEvent(StdinClosed, nil)
					return
				} else {
					// Non-TTY mode: FIFO may not have readers (detached mode)
					// Don't exit the copier, just log and continue
					log.Infof("[IO] stderr FIFO closed/no reader for %s (Non-TTY detached mode), discarding %d bytes and continuing", c.config.ContainerID, len(data))
					// Continue running, don't exit
					continue
				}
			}
			log.Errorf("[IO] stderr write error for %s: %v", c.config.ContainerID, err)
			c.publishEvent(IOError, err)
			return
		}
	}
}

// SetStdin sets the stdin FIFO.
func (c *Copier) SetStdin(fifo io.ReadCloser) {
	c.stdinFifo = fifo
}

// SetStdout sets the stdout FIFO.
func (c *Copier) SetStdout(fifo io.WriteCloser) {
	c.stdoutFIFO = fifo
}

// SetStderr sets the stderr FIFO.
func (c *Copier) SetStderr(fifo io.WriteCloser) {
	c.stderrFIFO = fifo
}

// SetTTYs sets the TTY interfaces.
func (c *Copier) SetTTYs(ttyIn io.WriteCloser, ttyOut, ttyErr io.Reader) {
	var oldFd, newFd int
	if c.ttyIn != nil {
		if f, ok := c.ttyIn.(*os.File); ok {
			oldFd = int(f.Fd())
		}
	}
	if ttyIn != nil {
		if f, ok := ttyIn.(*os.File); ok {
			newFd = int(f.Fd())
		}
	}
	log.Tracef("[IO] SetTTYs for %s: oldFd=%d, newFd=%d", c.config.ContainerID, oldFd, newFd)
	c.ttyIn = ttyIn
	c.ttyOut = ttyOut
	c.ttyErr = ttyErr

	// If TTY fd changed and epoll is active, reinitialize epoll with new TTY fd
	if oldFd != newFd && newFd > 0 && c.epollFd >= 0 {
		log.Infof("[IO] TTY fd changed from %d to %d, reinitializing epoll for %s", oldFd, newFd, c.config.ContainerID)
		// Close old epoll
		unix.Close(c.epollFd)
		c.epollFd = -1
		// Reinitialize with new TTY fd
		if err := c.initEpoll(newFd); err != nil {
			log.Errorf("[IO] Failed to reinitialize epoll after TTY update: %v", err)
		} else {
			log.Infof("[IO] Epoll reinitialized with new TTY fd=%d for %s", newFd, c.config.ContainerID)
		}
	}
}

// Helper functions

func isClosed(err error) bool {
	if err == nil {
		return false
	}
	if err == io.EOF {
		return true
	}
	if isEAGAIN(err) {
		return false
	}
	// Check for "closed" string in error message
	return contains(err.Error(), "closed") || contains(err.Error(), "use of closed")
}

func isBrokenPipe(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "broken pipe") || contains(err.Error(), "EPIPE")
}

func isEAGAIN(err error) bool {
	if err == nil {
		return false
	}
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.EAGAIN || errno == syscall.EWOULDBLOCK
	}
	return contains(err.Error(), "EAGAIN") || contains(err.Error(), "resource temporarily unavailable")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// filterNUL removes NUL bytes from data, reusing the buffer.
func filterNUL(dst, src []byte) []byte {
	for _, b := range src {
		if b != 0 {
			dst = append(dst, b)
		}
	}
	return dst
}

// removeNUL removes NUL bytes from data.
func removeNUL(data []byte) []byte {
	result := make([]byte, 0, len(data))
	for _, b := range data {
		if b != 0 {
			result = append(result, b)
		}
	}
	return result
}

// isPrintable checks if a byte is a printable ASCII character (32-126).
// Used for local echo to supplement unreliable RPMSG TTY echo.
// Returns false for control characters (0-31, 127) including \r, \n, \t, etc.
func isPrintable(b byte) bool {
	return b >= 32 && b <= 126
}

// hexDump converts a byte slice to a hexadecimal string representation.
// Used for debugging non-TTY mode to show raw byte data.
func hexDump(data []byte) string {
	result := ""
	for i := 0; i < len(data); i++ {
		result += fmt.Sprintf("\\x%02x", data[i])
	}
	return result
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
// the same text we just typed), but this is acceptable for typical usage.
func (c *Copier) suppressRTOSEcho(data []byte) []byte {
	if len(c.sentChars) == 0 {
		// No tracked characters, nothing to suppress
		return data
	}

	result := make([]byte, 0, len(data))
	suppressedCount := 0

	for i := 0; i < len(data); i++ {
		ch := data[i]

		// Check if this character matches the next tracked character
		if c.sentCharsCursor < len(c.sentChars) && ch == c.sentChars[c.sentCharsCursor] {
			// Match found - this is likely RTOS echo, suppress it
			c.sentCharsCursor++
			suppressedCount++
		} else {
			// No match - this is RTOS output, keep it
			// Also reset tracking since echo is out of sync
			if suppressedCount > 0 {
				// Check bounds before accessing sentChars[c.sentCharsCursor]
				expectedByte := "out of range"
				if c.sentCharsCursor < len(c.sentChars) {
					expectedByte = string([]byte{c.sentChars[c.sentCharsCursor]})
				}
				log.Tracef("[IO] Echo suppression lost sync at pos %d, resetting (expected %s, got %d (%q))",
					c.sentCharsCursor, expectedByte, ch, ch)
			}
			result = append(result, ch)
			// Reset cursor AND clear buffer - we've lost sync, so don't try to resume
			// This prevents old tracked characters from interfering with new input
			c.sentCharsCursor = 0
			c.sentChars = c.sentChars[:0]
		}
	}

	// If we've consumed all tracked characters, reset for next line
	if c.sentCharsCursor >= len(c.sentChars) {
		c.sentChars = c.sentChars[:0]
		c.sentCharsCursor = 0
	}

	if suppressedCount > 0 {
		log.Tracef("[IO] Suppressed %d echoed characters from RTOS", suppressedCount)
	}

	return result
}

