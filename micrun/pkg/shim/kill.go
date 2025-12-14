package shim

import (
	"context"
	"syscall"
	"time"

	log "micrun/logger"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
)

// requestContainerKill forwards a best-effort Kill request to the shim service.
// timeout = 5, hard coded.
func requestContainerKill(ctx context.Context, s *shimService, c *shimContainer, sig syscall.Signal, reason string) {
	if s == nil || c == nil {
		return
	}

	killCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if _, err := s.Kill(killCtx, &taskAPI.KillRequest{
		ID:     c.id,
		Signal: uint32(sig),
	}); err != nil {
		log.Warnf("container %s kill (%s) failed: %v", c.id, reason, err)
		c.ioExit()
	}
}
