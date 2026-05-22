package attach

import (
	"context"

	"micrun/internal/ports"
	log "micrun/internal/support/logger"
)

func closeTaskStdin(ctx context.Context, taskHandle ports.Task) error {
	stdinPipe := taskHandle.StdinPipe()
	if stdinPipe == nil {
		return nil
	}
	if err := stdinPipe.Close(); err != nil {
		log.Tracef("stdin pipe close for %s returned: %v", taskHandle.ID(), err)
	}
	stdinCloser := taskHandle.StdinCloser()
	if stdinCloser == nil {
		return nil
	}
	select {
	case <-stdinCloser:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
