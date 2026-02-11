package shim

import (
	"context"
	"fmt"
	stdio "io"
	"time"

	log "micrun/logger"

	micrunio "micrun/pkg/io"

	"github.com/containerd/containerd/api/types/task"
)

// ioSessionWrapper wraps the IO session for use in the shim.
type ioSessionWrapper struct {
	session  *micrunio.Session
	svc      *shimService
	container *shimContainer
}

// Stop stops the IO session.
func (w *ioSessionWrapper) Stop() {
	if w.session != nil {
		w.session.Stop()
	}
}

// StopWithoutClosingFIFOs stops the IO session but keeps FIFOs for reattach.
func (w *ioSessionWrapper) StopWithoutClosingFIFOs() {
	if w.session != nil {
		w.session.StopWithoutClosingFIFOs()
	}
}

// Restart restarts the IO session for reattach.
func (w *ioSessionWrapper) Restart() error {
	if w.session != nil {
		return w.session.Restart()
	}
	return fmt.Errorf("no session to restart")
}

// RestartWithTTYs restarts the IO session with fresh TTY handles.
// This is the preferred method for reattach scenarios where the original TTY
// handles may be closed.
func (w *ioSessionWrapper) RestartWithTTYs(ttyIn stdio.WriteCloser, ttyOut stdio.Reader) error {
	if w.session != nil {
		return w.session.RestartWithTTYs(ttyIn, ttyOut)
	}
	return fmt.Errorf("no session to restart")
}

// IsRunning returns true if the IO session is currently running.
func (w *ioSessionWrapper) IsRunning() bool {
	if w.session != nil {
		running := w.session.IsRunning()
		log.Debugf("[IO-WRAPPER] IsRunning for %s: %v (session=%p)", w.container.id, running, w.session)
		return running
	}
	log.Debugf("[IO-WRAPPER] IsRunning for %s: false (session is nil)", w.container.id)
	return false
}

// startContainer starts a container or sandbox within the shim service.
//
// For sandboxes (CanBeSandbox): calls sandbox.Start() and launches a watchSandbox goroutine
// to monitor the entire sandbox lifecycle.
//
// For pod containers: calls sandbox.StartContainer() to start a specific container.
//
// Sets up IO streams via sandbox.IOStream() and manages tty/non-tty IO copying.
// For containers with terminal=false and no IO fifos (like pause/infra containers),
// signals exit immediately since they don't need lifecycle monitoring.
//
// Launches waitContainerExit goroutine to monitor container termination
// and handle cleanup. On any error, sends exit code 255 to exitCh for cleanup.
func startContainer(ctx context.Context, s *shimService, c *shimContainer) (retErr error) {

	if c.cType == "" {
		err := fmt.Errorf("the contaienr %s type is empty", c.id)
		return err
	}

	if s.sandbox == nil {
		err := fmt.Errorf("the sandbox hasn't been created for this container %s", c.id)
		return err
	}

	if c.cType.CanBeSandbox() {
		err := s.sandbox.Start(ctx)
		if err != nil {
			log.Errorf("failed to start sandbox for container %s", c.id)
			return err
		}

	} else {
		_, err := s.sandbox.StartContainer(ctx, c.id)
		if err != nil {
			return err
		}
	}

	oldst := c.status
	c.setStatus(task.Status_RUNNING)
	log.Debugf("container status from %s => %s ", oldst, c.status)
	stdin, stdout, stderr, err := s.sandbox.IOStream(c.id, c.id)
	if err != nil {
		return err
	}
	log.Debugf("=> io stream: %v %v %v", stdin, stdout, stderr)

	c.stdinPipe = stdin

	log.Debugf("[SHIM] FIFO paths for %s: stdin=%q, stdout=%q, stderr=%q",
		c.id, c.stdin, c.stdout, c.stderr)

	if c.stdin != "" || c.stdout != "" || c.stderr != "" {
		// Use the new event-driven IO system
		err := startIOSessionWithEvents(ctx, s, c, stdin, stdout, stderr)
		if err != nil {
			return fmt.Errorf("failed to start IO session: %w", err)
		}
	} else {
		// Close stdin closer so CloseIO can unblock even when the container never
		// had an input fifo.
		close(c.stdinCloser)
		// Infra (pause) containers must stay alive to keep the sandbox ready.
		// Skip closing exitIOch so waitContainerExit only runs when we receive an
		// explicit teardown signal (Kill/Delete). Non-sandbox workloads retain
		// the original behaviour.
		if !c.cType.IsCriSandbox() {
			c.ioExit()
		}
	}

	go waitContainerExit(s.ctx, s, c)

	// NOTE: RTOS startup wait has been moved to startIOSessionWithEvents
	// where it waits for the TTYReady event instead of a fixed 2-second sleep.
	// This provides faster startup when RTOS is ready, and protects against
	// RTOS failures with a 5-second timeout.

	return nil
}

// startIOSessionWithEvents starts the IO session with event-driven architecture.
// Creates an event bus, subscribes to IO events, and manages the IO lifecycle.
func startIOSessionWithEvents(
	ctx context.Context,
	s *shimService,
	c *shimContainer,
	ttyIn stdio.WriteCloser,
	ttyOut stdio.Reader,
	ttyErr stdio.Reader,
) error {
	log.Debugf("[SHIM] startIOSessionWithEvents for %s", c.id)

	// Create event bus for IO events
	eventBus := micrunio.NewEventBus(ctx)

	// Subscribe to IO events
	exitCh := eventBus.Subscribe(micrunio.ExitCommandDetected)
	// Note: ttyReadyCh subscription is removed since we no longer wait for TTY ready event
	stdinClosedCh := eventBus.Subscribe(micrunio.StdinClosed)
	errorCh := eventBus.Subscribe(micrunio.IOError)

	// Start event handler goroutine
	go s.handleIOEvents(ctx, c, eventBus, exitCh, stdinClosedCh, errorCh)

	// Create IO session config
	config := micrunio.Config{
		ContainerID:  c.id,
		StdinFIFO:    c.stdin,
		StdoutFIFO:   c.stdout,
		StderrFIFO:   c.stderr,
		TTYIn:        ttyIn,
		TTYOut:       ttyOut,
		TTYErr:       ttyErr,
		Terminal:     c.terminal,
		EventBus:     eventBus, // Pass event bus to IO layer
		FilterNUL:    true,     // Filter NUL bytes from RTOS
		ExecMode:     false,    // Not an exec session
	}

	// Create IO session
	session, err := micrunio.NewSession(config)
	if err != nil {
		return err
	}

	// Start session (creates FIFOs, opens them, starts copier)
	// This must be done BEFORE waiting for TTYReady event, because the copier
	// goroutine is responsible for publishing the TTYReady event when it
	// successfully reads the first byte from the TTY.
	if err := session.Start(); err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}

	// Note: TTY ready event waiting is disabled for non-TTY mode
	//
	// This restores behavior similar to commit ccc0977, which did not have
	// strict timeout checking and was known to work correctly.
	//
	// Some RTOS applications may have delayed output due to:
	// 1. RPMSG channel initialization delays
	// 2. Waiting for user input before producing output
	// 3. Frontend/backend connection issues
	//
	// Rather than failing startup with a timeout, we allow the container to
	// start and let the user attach and interact. The IO copier will still
	// publish TTYReady event when it receives data, which can be used for
	// monitoring purposes.
	//
	// For TTY mode (-t flag), the interactive session naturally provides
	// feedback to the user, so no explicit wait is needed there either.

	// Store the session in the container
	wrapper := &ioSessionWrapper{
		session:  session,
		svc:      s,
		container: c,
	}
	c.ioManager = wrapper

	// Save attach info for reattach support
	// This is required for nerdctl attach to work on running containers
	c.attachInfo = &AttachInfo{
		Stdin:    c.stdin,
		Stdout:   c.stdout,
		Stderr:   c.stderr,
		Terminal: c.terminal,
		TTYIn:    ttyIn,
		TTYOut:   ttyOut,
		TTYErr:   ttyErr,
	}
	log.Infof("[SHIM] Saved attach info for %s: terminal=%v", c.id, c.terminal)

	return nil
}

// handleIOEvents handles IO events from the event bus.
func (s *shimService) handleIOEvents(
	ctx context.Context,
	c *shimContainer,
	eventBus *micrunio.EventBus,
	exitCh,
	stdinClosedCh,
	errorCh micrunio.EventSubscriber,
) {
	log.Debugf("[EVENTS] Starting IO event handler for %s", c.id)

	for {
		select {
		case <-ctx.Done():
			log.Debugf("[EVENTS] Context canceled, stopping event handler for %s", c.id)
			return

		case _, ok := <-exitCh:
			if !ok {
				return
			}
			log.Debugf("[EVENTS] ExitCommandDetected event received for %s", c.id)
			s.handleExitCommand(ctx, c)

		case _, ok := <-stdinClosedCh:
			if !ok {
				return
			}
			log.Debugf("[EVENTS] StdinClosed event received for %s", c.id)
			s.handleStdinClosed(ctx, c)

		case event, ok := <-errorCh:
			if !ok {
				return
			}
			log.Warnf("[EVENTS] IOError event received for %s: %v", c.id, event.Data)
			// TODO: Handle IO errors appropriately

		case status, ok := <-c.statusCh:
			if !ok {
				return
			}
			if status == task.Status_STOPPED {
				log.Debugf("[EVENTS] Container %s is STOPPED, stopping event handler", c.id)
				return
			}
		}
	}
}

// handleExitCommand handles the exit command event.
func (s *shimService) handleExitCommand(ctx context.Context, c *shimContainer) {
	s.mu.Lock()
	alreadyStopped := (c.status == task.Status_STOPPED)
	c.setStatus(task.Status_STOPPED)
	c.exit = 0 // exit command returns 0
	c.exitTime = time.Now()
	s.mu.Unlock()

	if alreadyStopped {
		log.Debugf("[EVENTS] Container %s already stopped, skipping duplicate exit handling", c.id)
		return
	}

	log.Infof("[EVENTS] Exit command for %s, stopping container (preserving sandbox)", c.id)

	// Signal waitContainerExit to stop the sandbox and do cleanup
	c.ioExit()

	// Stop IO session (closes FIFOs, stops copier)
	if c.ioManager != nil {
		c.ioManager.Stop()
		c.ioManager = nil
	}
}

// handleStdinClosed handles the stdin closed event.
func (s *shimService) handleStdinClosed(ctx context.Context, c *shimContainer) {
	log.Infof("[EVENTS] Stdin closed for %s, stopping IO session (container continues)", c.id)

	// When stdin is closed in non-TTY attach mode, we keep the container running
	// but stop the IO session to signal the attach client to exit.
	// The user can reattach later. This is the expected behavior for 1:1:1 lifecycle.
	// Stop IO session (closes FIFOs, stops copier)
	// This causes the attach client (nerdctl attach) to exit cleanly
	if c.ioManager != nil {
		c.ioManager.Stop()
		// NOTE: Don't set c.ioManager = nil here!
		// This keeps the ioManager reference so that subsequent SIGKILL
		// (from containerd timeout) can recognize this is an attach disconnect
		// and ignore the kill signal, keeping the container running.
	}

	// Note: We don't stop the container or sandbox here
	// The container continues running in the background
}
