package shim

import (
	"io"
	cntr "micrun/internal/domain/container"
	ports "micrun/internal/ports"
	ann "micrun/internal/support/annotations"
	log "micrun/internal/support/logger"
	"sync"
	"sync/atomic"
	"time"

	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/errdefs"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type IOManager = ports.IOManager
type AttachInfo = ports.AttachInfo

type shimContainer struct {
	s    *shimService
	spec *specs.Spec
	id   string
	// io
	stdin       string
	stdout      string
	stderr      string
	stdinPipe   io.WriteCloser
	stdinCloser chan struct{}
	exitIOch    chan struct{}
	exitIoOnce  sync.Once
	bundle      string // abs path of the bundle directory
	cType       cntr.ContainerType
	status      task.Status
	exit        uint32
	terminal    bool
	pid         uint32 // shim pid
	exitTime    time.Time
	mounted     bool
	ioMu        sync.Mutex  // protects ioManager field
	ioManager   IOManager   // IO session from adapters/io
	attachInfo  *AttachInfo // Saved for reattach
	// IO mode classification for handling different TTY/detach scenarios
	ioMode IOMode
	// Attach state management
	attached        atomic.Bool   // Current attach status
	attachChanged   chan struct{} // signals attach<->detach transitions (buffered 1)
	attachEventMu   sync.Mutex    // guards lastAttachEvent
	lastAttachEvent time.Time     // publish timestamp of the newest applied attach event
	// recovered marks tasks reconstructed from persisted state: they have no
	// attach session and no OCI spec, so the wait policy must not auto-close
	// (kill) them after a timeout.
	recovered bool
	// deleteMu serializes the StopContainer+DeleteContainer sequence in
	// deleteContainer: two concurrent Delete RPCs for the same container
	// would both attempt guestControl.Remove and the loser would surface a
	// spurious error.
	deleteMu sync.Mutex
}

var _ ports.Task = (*shimContainer)(nil)

// newContainer creates a new container object for the shim.
func newContainer(s *shimService, r ports.TaskCreateRequest, cType cntr.ContainerType, ocispec *specs.Spec, mounted bool) (*shimContainer, error) {
	if r.ID == "" {
		return nil, errdefs.ToGRPCf(errdefs.ErrInvalidArgument, "CreateTaskRequest ID is empty")
	}

	if ocispec == nil {
		ocispec = &specs.Spec{}
	}

	// Determine IO mode for this container
	ioMode := DetermineIOMode(r)

	// Generate FIFO paths based on IO mode (classified handling)
	stdin, stdout, stderr := GenerateFIFOPaths(r, s.namespace)

	c := &shimContainer{
		s:             s,
		spec:          ocispec,
		id:            r.ID,
		stdin:         stdin,
		stdout:        stdout,
		stderr:        stderr,
		exitIOch:      make(chan struct{}),
		stdinCloser:   make(chan struct{}),
		bundle:        r.Bundle,
		cType:         cType,
		status:        task.Status_CREATED,
		terminal:      r.Terminal,
		mounted:       mounted,
		pid:           s.ShimPID(),
		ioMode:        ioMode,
		attachChanged: make(chan struct{}, 1),
	}

	// Attached means a live client. Create-time FIFO paths do not imply
	// one (`ctr task start -d` still supplies FIFOs). EnsureAttach/Resize
	// set this; CloseIO and stdin-close clear it so auto-close can run.
	c.attached.Store(false)

	log.Infof("[SHIM] Container %s: IO mode=%s, attached=%v, supportsAttach=%v",
		c.id, ioMode.String(), c.attached.Load(), ioMode.SupportsAttach)

	return c, nil
}

func (c *shimContainer) ioExit() {
	log.Debugf("close shim container io channel")
	if c == nil {
		return
	}

	c.exitIoOnce.Do(func() {
		close(c.exitIOch)
	})
}

func (c *shimContainer) IOExit() {
	c.ioExit()
}

// IsRecovered reports whether this task was rebuilt from persisted state
// after a shim restart (see the recovered field).
func (c *shimContainer) IsRecovered() bool {
	return c.recovered
}

func (c *shimContainer) ID() string {
	return c.id
}

func (c *shimContainer) Bundle() string {
	return c.bundle
}

func (c *shimContainer) PID() uint32 {
	return c.pid
}

func (c *shimContainer) Status() task.Status {
	return c.status
}

func (c *shimContainer) SetStatus(status task.Status) {
	c.setStatus(status)
}

func (c *shimContainer) Terminal() bool {
	return c.terminal
}

func (c *shimContainer) StdinPath() string {
	return c.stdin
}

func (c *shimContainer) StdoutPath() string {
	return c.stdout
}

func (c *shimContainer) StderrPath() string {
	return c.stderr
}

func (c *shimContainer) ExitStatus() uint32 {
	return c.exit
}

func (c *shimContainer) ExitTime() time.Time {
	return c.exitTime
}

func (c *shimContainer) SetExitInfo(status uint32, exitedAt time.Time) {
	c.exit = status
	c.exitTime = exitedAt
}

func (c *shimContainer) StdinPipe() io.WriteCloser {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	return c.stdinPipe
}

func (c *shimContainer) StdinCloser() chan struct{} {
	return c.stdinCloser
}

func (c *shimContainer) ExitChan() chan struct{} {
	return c.exitIOch
}

func (c *shimContainer) CanBeSandbox() bool {
	return c.cType.CanBeSandbox()
}

func (c *shimContainer) IsCriSandbox() bool {
	return c.cType.IsCriSandbox()
}

func (c *shimContainer) Annotations() map[string]string {
	if c.recovered {
		// A recovered task never had an attach session: auto-close (kill
		// after the default 30s timeout) must not apply, its exit is driven
		// solely by the guest domain disappearing. Synthesize the disabling
		// annotation so the wait policy resolves autoClose=false.
		return map[string]string{ann.AutoClose: "false"}
	}
	if c.spec == nil || c.spec.Annotations == nil {
		return nil
	}
	return c.spec.Annotations
}

func (c *shimContainer) IOManager() ports.IOManager {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	return c.ioManager
}

func (c *shimContainer) SetIOManager(manager ports.IOManager) {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	c.ioManager = manager
}

func (c *shimContainer) AttachInfo() *ports.AttachInfo {
	return c.attachInfo
}

func (c *shimContainer) SetAttachInfo(info *ports.AttachInfo) {
	c.attachInfo = info
}

func (c *shimContainer) SetStdinPipe(pipe io.WriteCloser) {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	c.stdinPipe = pipe
}

func (c *shimContainer) SetAttached(attached bool) (previous bool) {
	prev := c.attached.Swap(attached)
	if prev != attached {
		// Notify state transitions (attach <-> detach) without blocking the
		// caller: the auto-close watcher consumes this to start the grace
		// timer AT the detach moment instead of sampling at timer expiry
		// (which made the effective grace depend on cycle phase and could
		// collapse to ~0 right after a long attach session).
		select {
		case c.attachChanged <- struct{}{}:
		default:
		}
	}
	return prev
}

func (c *shimContainer) IsAttached() bool {
	return c.attached.Load()
}

// AttachChanges returns a notification channel that receives a signal on
// every attach-state transition. Optional capability consumed by the
// lifecycle auto-close watcher via interface assertion.
func (c *shimContainer) AttachChanges() <-chan struct{} {
	return c.attachChanged
}

// ApplyAttachEvent applies an attach-state event sourced from the IO event
// bus, dropping events that are stale relative to the newest applied one.
// The fan-in of per-type subscriber channels cannot preserve cross-type
// publish order (ClientAttached published before ClientDetached can be
// delivered after it), and a flipped final value either suspends auto-close
// forever or kills a live session after the grace window. The bus stamps
// every event with its publish time, so "older than the newest applied
// event" reliably identifies the late delivery. A zero timestamp applies
// unconditionally (manual/test events).
func (c *shimContainer) ApplyAttachEvent(attached bool, at time.Time) bool {
	c.attachEventMu.Lock()
	if !at.IsZero() && !at.After(c.lastAttachEvent) {
		c.attachEventMu.Unlock()
		return false
	}
	c.lastAttachEvent = at
	c.attachEventMu.Unlock()
	c.SetAttached(attached)
	return true
}

// setStatus updates the task status.
// Must be called while holding the runtime lock to ensure thread safety.
//
// This is the single choke point for status writes: illegal lifecycle
// transitions (per ports.CanTransitTaskStatus) are rejected here as a final
// safety net, so a caller that raced past its service-level guards (the
// recurring bug class in this codebase) cannot e.g. resurrect a STOPPED task
// or move a started task back to CREATED.
func (c *shimContainer) setStatus(status task.Status) {
	if c.status == status {
		return
	}
	if !ports.CanTransitTaskStatus(c.status, status) {
		log.Warnf("[SHIM] task %s: rejecting illegal status transition %s -> %s", c.id, c.status, status)
		return
	}
	c.status = status
}
