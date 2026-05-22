package io

import (
	"context"
	"io"
	"micrun/internal/domain/console"
	"micrun/internal/support/contextx"
	"micrun/internal/support/logger"
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

	// Input interpreter owns user-facing console semantics.
	input *console.InputInterpreter

	// For local echo in TTY mode (to avoid sending exit to RTOS)
	stdoutFifoForEcho io.WriteCloser

	// TTY ready flag (to publish TTYReady event only once)
	ttyReadyPublished atomicBool

	ttyWaiter   epollWaiter
	stdinWaiter epollWaiter

	// EOF state for stdin (to handle detached mode where FIFO has no writer initially)
	stdinEOFSeen bool

	// Track whether we've received data from attach client
	// This helps distinguish between "initial EOF, waiting for attach" vs "EOF after data, attach done"
	attachClientConnected bool

	// stdinActivityGeneration increments whenever new stdin data reaches the copier.
	// It allows tests and logs to correlate "input was sent" with "output followed".
	stdinActivityGeneration uint64
	// observedOutputGeneration tracks the last stdin activity generation for which
	// we already emitted a post-input output marker.
	observedOutputGeneration uint64

	// Echo suppression (to avoid double echo when both PTY and RTOS echo)
	suppressEcho   bool
	echoSuppressor *console.EchoSuppressor
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
		stdinWaiter:    newEpollWaiter(pipe[0], -1, false),
		input:          console.NewInputInterpreter(console.InputConfig{Terminal: config.Terminal, ExecMode: config.ExecMode, DetachKeys: config.DetachKeys}),
		suppressEcho:   config.Terminal,
		echoSuppressor: console.NewEchoSuppressor(256),
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

	if plan.stdinToTTY {
		log.Debugf("[IO] Starting stdin→TTY copier goroutine for %s", c.config.ContainerID)
		c.wg.Add(1)
		go c.copyStdin()
	} else {
		log.Debugf("[IO] Skipping stdin→TTY copier for %s: StdinFIFO=%q, ttyIn=%v", c.config.ContainerID, c.config.StdinFIFO, c.ttyIn != nil)
	}

	if plan.unifiedOutput {
		log.Infof("[IO] Copier started for %s (unified stdout/stderr, fd=%d)", c.config.ContainerID, plan.unifiedOutputFD)
		c.wg.Add(1)
		go c.copyStdoutErrUnified()
		return nil
	}

	if plan.stdoutToFIFO {
		log.Debugf("[IO] Starting TTY→stdout copier goroutine for %s", c.config.ContainerID)
		c.wg.Add(1)
		go c.copyStdout()
	} else {
		log.Debugf("[IO] Skipping TTY→stdout copier for %s: StdoutFIFO=%q, ttyOut=%v", c.config.ContainerID, c.config.StdoutFIFO, c.ttyOut != nil)
	}

	if plan.stderrToFIFO {
		log.Debugf("[IO] Starting TTY→stderr copier goroutine for %s", c.config.ContainerID)
		c.wg.Add(1)
		go c.copyStderr()
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
		return
	}

	if err := c.closeFIFOs(); err != nil {
		log.Warnf("[IO] Failed to close FIFOs for %s: %v", c.config.ContainerID, err)
	}
	if err := c.closeTTYs(); err != nil {
		log.Warnf("[IO] Failed to close TTYs for %s: %v", c.config.ContainerID, err)
	}

	c.finishStop(copierStopTimeout)
	log.Infof("[IO] Copier stopped for %s", c.config.ContainerID)
}

func (c *Copier) StopWithoutClosingFIFOs() {
	if !c.beginStop("stopping copier for reattach") {
		return
	}

	c.finishStop(0)
	log.Infof("[IO] Copier stopped for %s (FIFOs and TTYs preserved)", c.config.ContainerID)
}

func (c *Copier) beginStop(reason string) bool {
	if !c.stopped.CompareAndSwap(false, true) {
		return false
	}
	log.Infof("[IO] %s for %s", reason, c.config.ContainerID)
	c.ttyWaiter.signalCancel()
	c.cancel()
	return true
}

func (c *Copier) finishStop(timeout time.Duration) {
	c.waitForWorkers(timeout)
	c.ttyWaiter.close()
	c.stdinWaiter.close()
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

	// If TTY fd changed and epoll is active, reinitialize epoll with new TTY fd
	if newOK && oldFd != newFd && newFd > 0 && c.ttyWaiter.epfd >= 0 {
		log.Infof("[IO] TTY fd changed from %d to %d, reinitializing epoll for %s", oldFd, newFd, c.config.ContainerID)
		if c.ttyWaiter.epfd >= 0 {
			unix.Close(c.ttyWaiter.epfd)
			c.ttyWaiter.epfd = -1
		}
		if err := c.ttyWaiter.init(newFd); err != nil {
			log.Errorf("[IO] Failed to reinitialize epoll after TTY update: %v", err)
		} else {
			log.Infof("[IO] Epoll reinitialized with new TTY fd=%d for %s", newFd, c.config.ContainerID)
		}
	}
}
