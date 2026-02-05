package shim

import (
	"context"
	"syscall"

	log "micrun/logger"

	"github.com/containerd/containerd/api/types/task"
)

// requestContainerKill forwards a best-effort Kill request to the shim service.
// This is used for internal kills (timeout, context cancel), NOT for external Kill API calls.
// timeout = 5, hard coded.
func requestContainerKill(ctx context.Context, s *shimService, c *shimContainer, sig syscall.Signal, reason string) {
	if s == nil || c == nil {
		return
	}

	// NOTE: We deliberately do NOT call s.Kill() here because that would set
	// killedByAPI = true and prevent shim auto-exit. Instead, we directly
	// stop the sandbox/container to allow shim to exit naturally.
	//
	// External Kill API calls (from ctr task kill) set killedByAPI to keep
	// shim running for cleanup. Internal kills (timeout, cancel) should NOT
	// set this flag, allowing the shim to exit.

	log.Infof("[INTERNAL_KILL] Stopping container %s (reason: %s)", c.id, reason)

	s.mu.Lock()
	defer s.mu.Unlock()

	if c.cType.CanBeSandbox() {
		if s.sandbox != nil {
			log.Infof("[INTERNAL_KILL] Stopping sandbox for container %s (reason: %s)", c.id, reason)
			if err := s.sandbox.Stop(ctx, true); err != nil {
				log.Debugf("sandbox Stop returned: %v", err)
			}
			log.Infof("[INTERNAL_KILL] Deleting sandbox for container %s (reason: %s)", c.id, reason)
			if err := s.sandbox.Delete(ctx); err != nil {
				log.Debugf("sandbox Delete returned: %v", err)
			}
			s.sandbox = nil
		}
	} else {
		if s.sandbox != nil {
			if _, err := s.sandbox.StopContainer(ctx, c.id, true); err != nil {
				log.Debugf("StopContainer %s returned: %v", c.id, err)
			}
		}
	}

	c.setStatus(task.Status_STOPPED)
	// Do NOT set killedByAPI - allow shim to exit naturally
	c.ioExit()
	log.Infof("[INTERNAL_KILL] Container %s stopped (reason: %s)", c.id, reason)
}
