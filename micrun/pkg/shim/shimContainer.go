package shim

import (
	"context"
	"io"
	defs "micrun/definitions"
	log "micrun/logger"
	cntr "micrun/pkg/micantainer"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/errdefs"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// IOManager manages IO sessions for a container.
// It supports starting, stopping, and restarting IO sessions for attach.
type IOManager interface {
	Stop()
	StopWithoutClosingFIFOs()
	Restart() error
	IsRunning() bool
}

// AttachInfo holds information needed to reattach to a container.
type AttachInfo struct {
	Stdin    string
	Stdout   string
	Stderr   string
	Terminal bool
	TTYIn    io.WriteCloser
	TTYOut   io.Reader
	TTYErr   io.Reader
}

type shimContainer struct {
	s     *shimService
	spec  *specs.Spec
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
	statusCh    chan task.Status // Status change notification channel
	exit        uint32
	terminal    bool
	pid         uint32 // shim pid
	exitTime    time.Time
	mounted     bool
	ioManager   IOManager // IO session from pkg/io
	attachInfo  *AttachInfo // Saved for reattach
	// IO mode classification for handling different TTY/detach scenarios
	ioMode      IOMode
	// Attach state management
	isAttached  bool       // Current attach status
	attachLock  sync.Mutex // Protects isAttached
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
		statusCh:    make(chan task.Status, 1),
		terminal:    r.Terminal,
		mounted:     mounted,
		pid:         shimPid,
		ioMode:      ioMode,
		isAttached:  isAttached,
	}

	log.Infof("[SHIM] Container %s: IO mode=%s, attached=%v, supportsAttach=%v",
		c.id, ioMode.String(), c.isAttached, ioMode.SupportsAttach)

	return c, nil
}

func (c *shimContainer) ioExit() {
	// DEBUG: Print stack trace to find who is calling ioExit
	log.Debugf("close shim container io channel")
	if c == nil {
		return
	}
	// DEBUG: Print caller information
	pc, _, _, ok := runtime.Caller(1)
	if ok {
		fn := runtime.FuncForPC(pc)
		log.Infof("[DEBUG] ioExit() called from: %s", fn.Name())
	}
	// Print full stack trace to see the complete call path
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	log.Infof("[DEBUG] ioExit() call stack:\n%s", buf[:n])

	c.exitIoOnce.Do(func() {
		close(c.exitIOch)
	})
}

// waitContainerExit waits for the container to exit and updates its status.
func waitContainerExit(ctx context.Context, s *shimService, c *shimContainer) (int32, error) {
	// Wait for IO streams to close, or mock an exit after a timeout since micad
	// cannot yet detect client OS exit.
	const defaultTimeout = 30 * time.Second

	// Step 1: Check for deprecated annotation name and error if found
	const oldAutoCloseTimeout = defs.ContainerPrefix + "auto_disconnect_timeout"
	if hasAnnotation(c.spec, oldAutoCloseTimeout) {
		log.Errorf("[TIMEOUT] Annotation '%s' is not supported, use '%s' instead for %s",
			oldAutoCloseTimeout, defs.AutoCloseTimeout, c.id)
	}

	// Step 2: Parse annotations
	ptyAutoClose, ptyAutoCloseSet := getBoolAnnotation(c.spec, defs.AutoClose, true)
	ptyTimeout, timeoutSet := getDurationAnnotation(c.spec, defs.AutoCloseTimeout, defaultTimeout)

	// Step 3: Apply priority rules (timeout has higher priority)
	if timeoutSet {
		// Zero timeout means explicitly disabled (infinite connection)
		if ptyTimeout == 0 {
			ptyAutoClose = false
			log.Infof("[TIMEOUT] Auto-close disabled by zero timeout for %s", c.id)
		} else {
			if ptyAutoCloseSet && !ptyAutoClose {
				// Conflict: timeout takes priority over auto_close=false
				log.Warnf("[TIMEOUT] auto_close_timeout=%v takes priority over auto_close=false for %s",
					ptyTimeout, c.id)
			}
			ptyAutoClose = true
			log.Infof("[TIMEOUT] Auto-close enabled with timeout %v for %s", ptyTimeout, c.id)
		}
	} else if ptyAutoCloseSet && !ptyAutoClose {
		// Priority 2: auto_close explicitly set to false → disable
		log.Infof("[TIMEOUT] Auto-close disabled by annotation for %s", c.id)
	} else if ptyAutoCloseSet && ptyAutoClose {
		// Priority 3: auto_close explicitly set to true → enable with default timeout
		ptyTimeout = defaultTimeout
		log.Infof("[TIMEOUT] Auto-close enabled with default timeout %v for %s (auto_close=true)", ptyTimeout, c.id)
	} else {
		// Priority 4: Neither set → use default behavior
		log.Infof("[TIMEOUT] Auto-close enabled with default timeout %v for %s (default behavior)", ptyTimeout, c.id)
	}

	// Step 4: Mode-specific logging (no behavior changes)
	// All modes have default timeout applied in Step 3.
	// Users can explicitly disable timeout via auto_close=false or auto_close_timeout=0 annotations.
	// This step only logs the IO mode for debugging.
	if !c.ioMode.IsForeground {
		if !c.ioMode.IsTTY {
			log.Infof("[TIMEOUT] Non-TTY background mode, auto-close=%v for %s", ptyAutoClose, c.id)
		} else {
			log.Infof("[TIMEOUT] TTY background mode, auto-close=%v for %s", ptyAutoClose, c.id)
		}
	} else {
		log.Infof("[TIMEOUT] Foreground mode, auto-close=%v for %s", ptyAutoClose, c.id)
	}

	// Step 5: Sandbox exclusion
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
			// Clear attachInfo to ensure this kill is not treated as attach disconnect
			// This is critical for auto-timeout to actually stop the container
			s.mu.Lock()
			c.attachInfo = nil
			s.mu.Unlock()
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

	// Stop the sandbox WITHOUT holding the lock
	// This prevents blocking State() API and other operations
	// Lifecycle: 1:1:1 (shim:sandbox:RTOS)
	// When container task exits, RTOS stops, sandbox stops, but shim continues running.
	// The shim must continue running to serve API requests (State, Delete, etc.).
	// Sandbox deletion only happens when explicitly requested via Delete API or exit command.
	if c.cType.CanBeSandbox() {
		// For sandbox containers: Stop the sandbox (and RTOS) when container exits.
		// This is the 1:1:1 lifecycle - when RTOS stops, sandbox stops.
		s.mu.Lock()
		sandboxToStop := s.sandbox
		s.mu.Unlock()

		if sandboxToStop != nil {
			sandboxID := sandboxToStop.SandboxID()
			if err := sandboxToStop.Stop(ctx, true); err != nil {
				log.Errorf("Failed to stop sandbox %s: %v", sandboxID, err)
			}
		} else {
			log.Debugf("Sandbox already deleted, skipping stop in waitContainerExit")
		}
	} else {
		// For pod containers: stop the individual container but not the sandbox
		s.mu.Lock()
		sandboxToStop := s.sandbox
		s.mu.Unlock()

		if sandboxToStop != nil {
			if _, err := sandboxToStop.StopContainer(ctx, c.id, true); err != nil {
				log.Errorf("Failed to stop pod container %s.", c.id)
			}
		} else {
			log.Debugf("Sandbox already deleted, skipping StopContainer for %s", c.id)
		}
	}

	// Now acquire the lock briefly to update status
	s.mu.Lock()
	c.setStatus(task.Status_STOPPED)
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

// getBoolAnnotation parses a boolean annotation from the container spec with a default value.
// Returns (value, isExplicitlySet) where isExplicitlySet indicates if the annotation was provided.
func getBoolAnnotation(spec *specs.Spec, key string, defaultValue bool) (bool, bool) {
	if spec == nil || spec.Annotations == nil {
		return defaultValue, false
	}

	if value, ok := spec.Annotations[key]; ok {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed, true
		}
		// Check if value is numeric (user may have intended to set timeout duration)
		if _, err := strconv.Atoi(value); err == nil {
			// Suggest using auto_close_timeout instead of auto_close for numeric values
			log.Warnf("Boolean annotation '%s' has numeric value '%s'. "+
				"Did you mean to use '%s' for timeout duration?",
				key, value, defs.AutoCloseTimeout)
		} else {
			log.Warnf("Failed to parse boolean annotation '%s' with value '%s', using default: %v",
				key, value, defaultValue)
		}
	}
	return defaultValue, false
}

// getDurationAnnotation parses a duration annotation from the container spec with a default value.
// Supports both duration string format (e.g., "300s", "5m") and plain integer seconds (e.g., "300").
// Returns (value, isExplicitlySet) where isExplicitlySet indicates if the annotation was provided.
func getDurationAnnotation(spec *specs.Spec, key string, defaultValue time.Duration) (time.Duration, bool) {
	if spec == nil || spec.Annotations == nil {
		return defaultValue, false
	}

	if value, ok := spec.Annotations[key]; ok {
		log.Debugf("[getDurationAnnotation] Parsing annotation %s with value: %s", key, value)
		// Try parsing as duration string first (e.g., "300s", "5m")
		duration, parseErr := time.ParseDuration(value)
		log.Debugf("[getDurationAnnotation] time.ParseDuration result: duration=%v err=%v", duration, parseErr)
		if parseErr == nil {
			// Allow 0 as a special value meaning "no timeout" (infinite connection)
			// Also allow any positive duration
			// Reject negative values
			if duration < 0 {
				log.Errorf("annotation %s has invalid negative duration %s, using default %v", key, value, defaultValue)
				return defaultValue, true
			}
			// Zero or positive values are valid
			log.Infof("[getDurationAnnotation] Successfully parsed duration: %s -> %v", value, duration)
			return duration, true
		} else {
			// Fallback to parsing as plain integer seconds (for backward compatibility)
			log.Debugf("[getDurationAnnotation] time.ParseDuration failed, trying strconv.ParseInt")
			if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
				duration := time.Duration(seconds) * time.Second
				// Reject negative values
				if duration < 0 {
					log.Errorf("annotation %s has invalid negative duration %s, using default %v", key, value, defaultValue)
					return defaultValue, true
				}
				// Zero or positive values are valid
				log.Infof("[getDurationAnnotation] Successfully parsed as integer seconds: %s -> %v", value, duration)
				return duration, true
			} else {
				log.Warnf("annotation %s parse error: %v, defaulting to %v", key, err, defaultValue)
			}
		}
	}
	return defaultValue, false
}

// hasAnnotation checks if an annotation key exists and is non-empty.
func hasAnnotation(spec *specs.Spec, key string) bool {
	if spec == nil || spec.Annotations == nil {
		return false
	}
	val, ok := spec.Annotations[key]
	return ok && val != ""
}

// setStatus updates the container status and sends notification to statusCh.
// Must be called while holding s.mu lock to ensure thread safety.
func (c *shimContainer) setStatus(status task.Status) {
	c.status = status

	// Non-blocking send of status change notification
	select {
	case c.statusCh <- status:
	default:
		// Channel full or closed, skip notification
	}
}
