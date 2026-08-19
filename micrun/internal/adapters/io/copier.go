package io

import (
	"context"
	"io"
	"micrun/internal/domain/console"
	"micrun/internal/support/contextx"
	"micrun/internal/support/logger"
	"micrun/internal/support/panicsafe"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

const copierStopTimeout = 500 * time.Millisecond

type Copier struct {
	config Config
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// eagainWarnCount throttles the per-EAGAIN Warn below (slow attach
	// readers retry every 10ms; unthrottled that is ~100 WARN lines per
	// second per output stream). Only every 100th retry stays at Warn.
	eagainWarnCount uint32

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
	// streamsPreservedOnStop records that the winning stop path kept the
	// FIFO/TTY fds open (stopFromWorker(false) or the preserve-mode Stop
	// variants). A Session.stopLocked(CloseStreams) that loses the beginStop
	// CAS to such a path must NOT mark preservedStreams=false: the fds are
	// still open and would otherwise be orphaned forever. sync/atomic for a
	// lock-free read after Stop() returns.
	streamsPreservedOnStop atomic.Bool

	// Input interpreter owns user-facing console semantics.
	input *console.InputInterpreter

	// For local echo in TTY mode (to avoid sending exit to RTOS)
	stdoutFifoForEcho io.WriteCloser

	// TTY ready flag (to publish TTYReady event only once)
	ttyReadyPublished atomicBool

	ttyWaiter    epollWaiter
	ttyErrWaiter epollWaiter // dedicated waiter for stderr in split-output mode
	stdinWaiter  epollWaiter

	// EOF state for stdin (to handle detached mode where FIFO has no writer initially)
	stdinEOFSeen bool

	// Track whether we've received data from attach client
	// This helps distinguish between "initial EOF, waiting for attach" vs "EOF after data, attach done"
	attachClientConnected bool

	// liveClientPublished is true while a live stdin writer has been
	// announced. It is cleared when that writer goes away so a later
	// `ctr task attach` (same FIFOs, no second Start) can announce again.
	liveClientPublished atomic.Bool

	// stdinActivityGeneration increments whenever new stdin data reaches the copier.
	// It allows tests and logs to correlate "input was sent" with "output followed".
	stdinActivityGeneration uint64
	// observedOutputGeneration tracks the last stdin activity generation for which
	// we already emitted a post-input output marker.
	observedOutputGeneration uint64

	// Echo suppression (to avoid double echo when both PTY and RTOS echo)
	suppressEcho   bool
	echoSuppressor *console.EchoSuppressor

	// stopDone is closed once stop teardown (wait + waiter close, and for
	// stopFromWorker also stream close) has finished. External Stop paths
	// that lose the beginStop CAS still wait on this channel so reattach /
	// unmount cannot race with lingering workers.
	stopDone     chan struct{}
	stopDoneOnce sync.Once
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

// NewCopier creates a new copier.
func NewCopier(config Config) *Copier {
	config = normalizeConfig(config)
	ctx, cancel := context.WithCancel(contextx.OrBackground(config.Context))

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
		config:         config,
		ctx:            ctx,
		cancel:         cancel,
		ttyWaiter:      newEpollWaiter(pipe[0], pipe[1], true),
		ttyErrWaiter:   newEpollWaiter(pipe[0], -1, true),
		stdinWaiter:    newEpollWaiter(pipe[0], -1, false),
		input:          console.NewInputInterpreter(console.InputConfig{Terminal: config.Terminal, ExecMode: config.ExecMode, DetachKeys: config.DetachKeys}),
		suppressEcho:   config.Terminal,
		echoSuppressor: console.NewEchoSuppressor(256),
		stopDone:       make(chan struct{}),
	}
}

// SetStdoutFifoForEcho sets the stdout FIFO for local echo in TTY mode.

func (c *Copier) SetStdoutFifoForEcho(stdout io.WriteCloser) {
	c.stdoutFifoForEcho = stdout
}

// initEpoll initializes epoll for waiting on TTY data without busy polling.

func (c *Copier) publishEvent(typ EventType, err error) {
	if c.config.EventBus != nil {
		c.config.EventBus.Publish(Event{
			Type:        typ,
			ContainerID: c.config.ContainerID,
			Err:         err,
		})
	}
}

func (c *Copier) Start() error {
	plan := planCopierStart(c.stdinFifo, c.stdoutFIFO, c.stderrFIFO, c.ttyIn, c.ttyOut, c.ttyErr)

	// Copier workers run under a panic guard: a panic in one pump must not
	// kill the shim (and with it every managed domain). wg.Done runs inside
	// the worker's own defers, so Stop still unblocks after a lost worker.
	if plan.stdinToTTY {
		log.Debugf("[IO] Starting stdin→TTY copier goroutine for %s", c.config.ContainerID)
		c.wg.Add(1)
		panicsafe.Go("io copier stdin", c.copyStdin)
	} else {
		log.Debugf("[IO] Skipping stdin→TTY copier for %s: StdinFIFO=%q, ttyIn=%v", c.config.ContainerID, c.config.StdinFIFO, c.ttyIn != nil)
	}

	if plan.unifiedOutput {
		log.Infof("[IO] Copier started for %s (unified stdout/stderr, fd=%d)", c.config.ContainerID, plan.unifiedOutputFD)
		c.wg.Add(1)
		panicsafe.Go("io copier unified output", c.copyStdoutErrUnified)
		return nil
	}

	if plan.stdoutToFIFO {
		log.Debugf("[IO] Starting TTY→stdout copier goroutine for %s", c.config.ContainerID)
		c.wg.Add(1)
		panicsafe.Go("io copier stdout", c.copyStdout)
	} else {
		log.Debugf("[IO] Skipping TTY→stdout copier for %s: StdoutFIFO=%q, ttyOut=%v", c.config.ContainerID, c.config.StdoutFIFO, c.ttyOut != nil)
	}

	if plan.stderrToFIFO {
		log.Debugf("[IO] Starting TTY→stderr copier goroutine for %s", c.config.ContainerID)
		c.wg.Add(1)
		panicsafe.Go("io copier stderr", c.copyStderr)
	} else {
		log.Debugf("[IO] Skipping TTY→stderr copier for %s: StderrFIFO=%q, ttyErr=%v", c.config.ContainerID, c.config.StderrFIFO, c.ttyErr != nil)
	}

	log.Infof("[IO] Copier started for %s", c.config.ContainerID)
	return nil
}

type copierStartPlan struct {
	stdinToTTY      bool
	unifiedOutput   bool
	unifiedOutputFD int
	stdoutToFIFO    bool
	stderrToFIFO    bool
}

func planCopierStart(stdinFIFO io.ReadCloser, stdoutFIFO, stderrFIFO io.WriteCloser, ttyIn io.WriteCloser, ttyOut, ttyErr io.Reader) copierStartPlan {
	plan := copierStartPlan{
		stdinToTTY: stdinFIFO != nil && ttyIn != nil,
	}

	if stdoutFIFO != nil && stderrFIFO != nil {
		if outputFD, ok := sameTTYOutputFD(ttyOut, ttyErr); ok {
			plan.unifiedOutput = true
			plan.unifiedOutputFD = outputFD
			return plan
		}
	}

	plan.stdoutToFIFO = stdoutFIFO != nil && ttyOut != nil
	plan.stderrToFIFO = stderrFIFO != nil && ttyErr != nil
	return plan
}

func sameTTYOutputFD(stdout, stderr io.Reader) (int, bool) {
	stdoutFD, stdoutOK := fdOf(stdout)
	stderrFD, stderrOK := fdOf(stderr)
	if !stdoutOK || !stderrOK || stdoutFD != stderrFD {
		return 0, false
	}
	return stdoutFD, true
}

func (c *Copier) Stop() {
	if !c.beginStop("stopping copier") {
		// Another path already owns stop; still wait so callers (unmount /
		// reattach) do not race with workers that lost the CAS race after
		// stopFromWorker published an event.
		c.waitStopDone(copierStopTimeout)
		return
	}

	// Wait for workers to exit FIRST, then close streams. Closing FIFOs/TTYs
	// while copyStdout/copyStderr workers are still reading/writing them is
	// a use-after-close race (the same hazard stopFromWorker avoids by
	// deferring the close until after wg.Wait()).
	c.finishStop(copierStopTimeout, true)

	if err := c.closeFIFOs(); err != nil {
		log.Warnf("[IO] Failed to close FIFOs for %s: %v", c.config.ContainerID, err)
	}
	if err := c.closeTTYs(); err != nil {
		log.Warnf("[IO] Failed to close TTYs for %s: %v", c.config.ContainerID, err)
	}
	c.markStopDone()
	log.Infof("[IO] Copier stopped for %s", c.config.ContainerID)
}

// releaseResources stops a never-started copier without touching FIFO/TTY
// streams: the cancel-pipe fds are released (beginStop + waiter close), but
// the TTYs are owned by the session/caller and must not be closed here. Used
// for failed-start teardown so no fds leak while the caller keeps its TTY
// handles.
func (c *Copier) releaseResources() {
	if c == nil {
		return
	}
	if !c.beginStop("releasing copier resources") {
		c.waitStopDone(0)
		return
	}
	c.finishStop(0, false)
	c.markStopDone()
	log.Infof("[IO] Copier resources released for %s", c.config.ContainerID)
}

// stopFromWorker closes FIFOs/TTYs and stops the copier without joining the
// worker wait group. It must be used when the caller is itself a copier worker
// goroutine (e.g. exit/interrupt/detach detection from copyStdin), because
// wg.Wait() would otherwise wait for the caller to finish — a self-deadlock.
//
// The stream/TTY/waiter teardown is deferred to a background goroutine that
// waits for all peer workers to exit first (bounded by copierStopTimeout), so
// the close does not race with concurrent reads/writes on those streams, nor
// with a sibling worker still blocked in wait()/drainCancelPipe on the epoll
// or cancel-pipe fds.
func (c *Copier) stopFromWorker(closeStreams bool) {
	if !c.beginStop("stopping copier from worker") {
		return
	}
	if !closeStreams {
		// Record that the FIFO/TTY fds remain open so a Session.Stop() that
		// loses the CAS to us does not mark preservedStreams=false and orphan
		// them.
		c.streamsPreservedOnStop.Store(true)
	}
	// We cannot wg.Wait() here (self-deadlock), so spawn a goroutine that
	// waits for all workers (including the caller) to exit, then closes.
	// Wait is bounded: if a worker fails to exit (e.g. stuck on a
	// blocking fd), we still close after the timeout rather than leaking
	// FIFOs/TTYs/epoll/cancel-pipe forever.
	panicsafe.Go("io copier deferred stream close", func() {
		c.waitForWorkers(copierStopTimeout)
		if closeStreams {
			if err := c.closeFIFOs(); err != nil {
				log.Warnf("[IO] Failed to close FIFOs for %s: %v", c.config.ContainerID, err)
			}
			if err := c.closeTTYs(); err != nil {
				log.Warnf("[IO] Failed to close TTYs for %s: %v", c.config.ContainerID, err)
			}
		}
		// Close epoll waiters after all workers (including the caller)
		// have exited, so wait() is no longer running and close() is safe.
		// This must happen here because beginStop's CAS prevents any
		// external Stop() from reaching finishStop — external callers wait
		// on stopDone instead.
		c.ttyWaiter.close()
		c.ttyErrWaiter.close()
		c.stdinWaiter.close()
		c.markStopDone()
		log.Infof("[IO] Copier streams closed for %s (from worker)", c.config.ContainerID)
	})
	log.Infof("[IO] Copier stopped for %s (from worker, streams closed=%v)", c.config.ContainerID, closeStreams)
}

func (c *Copier) StopWithoutClosingFIFOs() {
	if !c.beginStop("stopping copier for reattach") {
		return
	}
	c.streamsPreservedOnStop.Store(true)

	// This may be invoked from within a worker goroutine (e.g. detach/exit
	// detection from copyStdin). In that case the calling worker is itself a
	// member of wg, so wg.Wait() would deadlock waiting for itself. Pass
	// wait=false so we only cancel/close waiters and let workers exit on their
	// own; external callers (session reattach) use StopWithoutClosingFIFOsAndWait.
	c.finishStop(0, false)
	c.markStopDone()
	log.Infof("[IO] Copier stopped for %s (FIFOs and TTYs preserved)", c.config.ContainerID)
}

// StopWithoutClosingFIFOsAndWait is the external-facing variant that waits for
// all copier workers to exit. Use it when calling from outside the copier
// goroutines (e.g. session reattach).
func (c *Copier) StopWithoutClosingFIFOsAndWait() {
	if !c.beginStop("stopping copier for reattach") {
		// stopFromWorker may already own the stop; wait for its teardown so
		// reattach does not wire a new session while old workers still hold
		// the FIFO/TTY fds.
		c.waitStopDone(copierStopTimeout)
		return
	}
	c.streamsPreservedOnStop.Store(true)

	c.finishStop(copierStopTimeout, true)
	c.markStopDone()
	log.Infof("[IO] Copier stopped for %s (FIFOs and TTYs preserved)", c.config.ContainerID)
}

// Stopped reports whether the copier has begun stopping — either via an
// external Stop/detach or because a worker died (fatal IO error / guest EOF
// routes through stopFromWorker). Lock-free; callers must tolerate a racing
// transition in either direction.
func (c *Copier) Stopped() bool {
	return c != nil && c.stopped.Load()
}

func (c *Copier) beginStop(reason string) bool {
	if !c.stopped.CompareAndSwap(false, true) {
		return false
	}
	log.Infof("[IO] %s for %s", reason, c.config.ContainerID)
	c.ttyWaiter.signalCancel()
	c.ttyErrWaiter.signalCancel()
	c.cancel()
	return true
}

// StreamsPreservedOnStop reports whether the stop path that tore down the
// copier kept the FIFO/TTY fds open (detach / preserve mode). A Session that
// issues a close-mode Stop but loses the beginStop CAS to a preserve-mode
// stop must consult this to avoid marking the (still-open) fds as closed.
func (c *Copier) StreamsPreservedOnStop() bool {
	if c == nil {
		return false
	}
	return c.streamsPreservedOnStop.Load()
}

func (c *Copier) finishStop(timeout time.Duration, wait bool) {
	if wait {
		c.waitForWorkers(timeout)
	}
	c.ttyWaiter.close()
	c.ttyErrWaiter.close()
	c.stdinWaiter.close()
}

func (c *Copier) markStopDone() {
	if c == nil {
		return
	}
	c.stopDoneOnce.Do(func() {
		if c.stopDone != nil {
			close(c.stopDone)
		}
	})
}

func (c *Copier) waitStopDone(timeout time.Duration) {
	if c == nil || c.stopDone == nil {
		return
	}
	if timeout <= 0 {
		<-c.stopDone
		return
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-c.stopDone:
	case <-timer.C:
		log.Warnf("[IO] Timeout waiting for copier stop completion for %s", c.config.ContainerID)
	}
}

func (c *Copier) waitForWorkers(timeout time.Duration) {
	if timeout <= 0 {
		c.wg.Wait()
		return
	}

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		log.Debugf("[IO] Copier workers exited for %s", c.config.ContainerID)
	case <-timer.C:
		log.Warnf("[IO] Copier workers timeout waiting for %s, continuing anyway", c.config.ContainerID)
	}
}

// copyStdin copies from stdin FIFO to TTY.
//
// TTY mode (terminal=true): Character-by-character processing for exit/detach detection
// and backspace handling. Local echo is enabled.
//
// Non-TTY mode (terminal=false): Direct passthrough with \n -> \r\n conversion.

func (c *Copier) SetStdin(fifo io.ReadCloser) {
	c.stdinFifo = fifo
}

func (c *Copier) SetStdout(fifo io.WriteCloser) {
	c.stdoutFIFO = fifo
}

func (c *Copier) SetStderr(fifo io.WriteCloser) {
	c.stderrFIFO = fifo
}

func (c *Copier) SetTTYs(ttyIn io.WriteCloser, ttyOut, ttyErr io.Reader) {
	oldFd, _ := fdOf(c.ttyIn)
	newFd, newOK := fdOf(ttyIn)
	log.Tracef("[IO] SetTTYs for %s: oldFd=%d, newFd=%d", c.config.ContainerID, oldFd, newFd)
	c.ttyIn = ttyIn
	c.ttyOut = ttyOut
	c.ttyErr = ttyErr

	// If TTY fd changed and epoll is active, reset the waiter so the next
	// wait() lazily re-initializes with the new TTY fd. reset() takes the
	// waiter lock, unlike direct epfd manipulation.
	if newOK && oldFd != newFd && newFd > 0 {
		log.Infof("[IO] TTY fd changed from %d to %d, resetting epoll for %s", oldFd, newFd, c.config.ContainerID)
		c.ttyWaiter.reset()
		c.ttyErrWaiter.reset()
	}
}
