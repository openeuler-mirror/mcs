package shim

import (
	"context"
	"fmt"
	log "micrun/logger"
	"syscall"

	"github.com/containerd/containerd/api/types/task"
)

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
	c.status = task.Status_RUNNING
	log.Debugf("container status from %s => %s ", oldst, c.status)
	stdin, stdout, stderr, err := s.sandbox.IOStream(c.id, c.id)
	if err != nil {
		return err
	}
	log.Debugf("=> io stream: %v %v %v", stdin, stdout, stderr)

	c.stdinPipe = stdin

	if c.stdin != "" || c.stdout != "" || c.stderr != "" {
		tty, err := newTtyIO(ctx, c.id, c.stdin, c.stdout, c.stderr, c.terminal)
		if err != nil {
			return err
		}
		c.ttyio = tty

		intr := hostIntrHandler(ctx, s, c)
		go ioCopy(ctx, c.exitIOch, c.stdinCloser, tty, stdin, stdout, intr)
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

	go waitContainerExit(ctx, s, c)

	return nil
}

// hostIntrHandler turns host control characters into a best-effort Kill request.
// mica client RTOS can not handle POSIX signals, so micrun handles them in host side
func hostIntrHandler(ctx context.Context, s *shimService, c *shimContainer) func(syscall.Signal, string) {
	return func(sig syscall.Signal, reason string) {
		requestContainerKill(ctx, s, c, sig, reason)
	}
}
