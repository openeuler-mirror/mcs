package shim

import (
	"context"
	"io"
	defs "micrun/definitions"
	log "micrun/logger"
	cntr "micrun/pkg/micantainer"
	"sync"
	"syscall"
	"time"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/errdefs"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type shimContainer struct {
	s     *shimService
	spec  *specs.Spec
	ttyio *ttyIO
	id    string
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
	// TODO: we can simulate `exec` by sending commands to mica pty
	// execs map[string]*execTask // extensible in future
}

// newContainer creates a new container object for the shim.
func newContainer(s *shimService, r *taskAPI.CreateTaskRequest, cType cntr.ContainerType, ocispec *specs.Spec, mounted bool) (*shimContainer, error) {
	if r == nil {
		return nil, errdefs.ToGRPCf(errdefs.ErrInvalidArgument, "CreateTaskRequest points to nil")
	}

	if ocispec == nil {
		ocispec = &specs.Spec{}
	}

	c := &shimContainer{
		s:           s,
		spec:        ocispec,
		id:          r.ID,
		stdin:       r.Stdin,
		stdout:      r.Stdout,
		stderr:      r.Stderr,
		exitIOch:    make(chan struct{}),
		stdinCloser: make(chan struct{}),
		bundle:      r.Bundle,
		cType:       cType,
		status:      task.Status_CREATED,
		terminal:    r.Terminal,
		mounted:     mounted,
		pid:         shimPid,
	}

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

// waitContainerExit waits for the container to exit and updates its status.
func waitContainerExit(ctx context.Context, s *shimService, c *shimContainer) (int32, error) {
	// Wait for IO streams to close, or mock an exit after a timeout since micad
	// cannot yet detect client OS exit.
	defaultTimeout := 30 * time.Second
	// align pty lifecycle with container task lifecycle
	ptyAutoClose, ptyAutoCloseSet := getBoolAnnotation(c.spec, defs.AutoClose, true) // Default to true for backward compatibility
	ptyTimeout, timeoutSet := getDurationAnnotation(c.spec, defs.AutoCloseTimeout, defaultTimeout)

	// If pty_auto_disconnect is explicitly set to false, disable auto disconnect even if timeout is provided
	// If timeout is explicitly set but pty_auto_disconnect is not set, enable auto disconnect
	if ptyAutoCloseSet {
		// pty_auto_disconnect is explicitly set, use its value
	} else if timeoutSet {
		// timeout is set but pty_auto_disconnect is not explicitly set, enable auto disconnect
		ptyAutoClose = true
	}

	// TODO: keep this line until mica RTOS notifier finished
	ptyAutoClose = true

	autoClose := ptyAutoClose && !c.cType.IsCriSandbox()
	var timer *time.Timer
	if autoClose {
		timer = time.NewTimer(ptyTimeout)
		defer timer.Stop()
	}

	if autoClose && timer != nil {
		select {
		case <-c.exitIOch:
			log.Debugf("The container %s IO streams closed.", c.id)
		case <-ctx.Done():
			log.Infof("waitContainerExit canceled for %s: %v", c.id, ctx.Err())
			requestContainerKill(ctx, s, c, syscall.SIGKILL, "wait-canceled")
			<-c.exitIOch
		case <-timer.C:
			log.Debugf("Auto-disconnect %s terminal after %v timeout.", c.id, ptyTimeout)
			requestContainerKill(ctx, s, c, syscall.SIGKILL, "auto-timeout")
			<-c.exitIOch
		}
	} else {
		select {
		case <-c.exitIOch:
			log.Debugf("received exit signal for container %s.", c.id)
		case <-ctx.Done():
			log.Infof("waitContainerExit canceled for %s: %v", c.id, ctx.Err())
			requestContainerKill(ctx, s, c, syscall.SIGKILL, "wait-canceled")
			<-c.exitIOch
		}
	}

	timeStamp := time.Now()
	ret := 0

	s.mu.Lock()
	// Update container status and exit information.
	if c.cType.CanBeSandbox() {
		if s.sandbox != nil {
			sandboxID := s.sandbox.SandboxID()
			if err := s.sandbox.Stop(ctx, true); err != nil {
				log.Errorf("Failed to stop sandbox %s forcely.", sandboxID)
			}

			if err := s.sandbox.Delete(ctx); err != nil {
				log.Errorf("Failed to delete sandbox %s.", sandboxID)
			}
		} else {
			log.Debugf("Sandbox already deleted, skipping stop/delete in waitContainerExit")
		}
	} else {
		if s.sandbox != nil {
			if _, err := s.sandbox.StopContainer(ctx, c.id, true); err != nil {
				log.Errorf("Failed to stop pod container %s.", c.id)
			}
		} else {
			log.Debugf("Sandbox already deleted, skipping StopContainer for %s", c.id)
		}
	}
	c.status = task.Status_STOPPED
	c.exit = uint32(ret)
	c.exitTime = timeStamp

	log.Debugf("The container %s status is StatusStopped.", c.id)
	s.mu.Unlock()

	go func(ts time.Time, cid string, status int) {
		s.ec <- exitEvent{
			ts:     ts,
			cid:    cid,
			execid: "",
			pid:    shimPid,
			status: status,
		}
	}(timeStamp, c.id, int(ret))

	return int32(ret), nil
}
