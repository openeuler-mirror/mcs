package shim

import (
	"io"
	cntr "micrun/internal/domain/container"
	ports "micrun/internal/ports"
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
	ioManager   IOManager   // IO session from adapters/io
	attachInfo  *AttachInfo // Saved for reattach
	// IO mode classification for handling different TTY/detach scenarios
	ioMode IOMode
	// Attach state management
	attached atomic.Bool // Current attach status
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

	// Initial attached state: foreground modes start as attached
	isAttached := ioMode.IsForeground

	c := &shimContainer{
		s:           s,
		spec:        ocispec,
		id:          r.ID,
		stdin:       stdin,
		stdout:      stdout,
		stderr:      stderr,
		exitIOch:    make(chan struct{}),
		stdinCloser: make(chan struct{}),
		bundle:      r.Bundle,
		cType:       cType,
		status:      task.Status_CREATED,
		terminal:    r.Terminal,
		mounted:     mounted,
		pid:         s.ShimPID(),
		ioMode:      ioMode,
	}

	c.attached.Store(isAttached)

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
	if c.spec == nil || c.spec.Annotations == nil {
		return nil
	}
	return c.spec.Annotations
}

func (c *shimContainer) IOManager() ports.IOManager {
	return c.ioManager
}

func (c *shimContainer) SetIOManager(manager ports.IOManager) {
	c.ioManager = manager
}

func (c *shimContainer) AttachInfo() *ports.AttachInfo {
	return c.attachInfo
}

func (c *shimContainer) SetAttachInfo(info *ports.AttachInfo) {
	c.attachInfo = info
}

func (c *shimContainer) SetStdinPipe(pipe io.WriteCloser) {
	c.stdinPipe = pipe
}

func (c *shimContainer) SetAttached(attached bool) (previous bool) {
	return c.attached.Swap(attached)
}

// setStatus updates the task status.
// Must be called while holding the runtime lock to ensure thread safety.
func (c *shimContainer) setStatus(status task.Status) {
	c.status = status
}
