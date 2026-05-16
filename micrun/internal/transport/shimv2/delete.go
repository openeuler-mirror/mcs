package shim

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/mount"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
)

// deleteContainer handles the deletion of a container, including stopping it if necessary and unmounting its rootfs.
func deleteContainer(ctx context.Context, s *shimService, c *shimContainer) error {
	if c == nil {
		return nil
	}

	// Forcibly delete pod containers.
	if !c.cType.CanBeSandbox() {
		sandbox, hasSandbox := s.currentSandbox()
		// Check if sandbox still exists before trying to stop/delete container
		if !hasSandbox {
			log.Debugf("Sandbox already deleted, skipping StopContainer/DeleteContainer for %s", c.id)
		} else {
			if c.status != task.Status_STOPPED {
				if _, err := sandbox.StopContainer(ctx, c.id, false); err != nil && errors.Is(err, er.ContainerNotFound) {
					log.Debugf("Container %s not found in real sandbox, already deleted.", c.id)
				} else if err != nil {
					return err
				}
			}
			if _, err := sandbox.DeleteContainer(ctx, c.id); err != nil && errors.Is(err, er.ContainerNotFound) {
				log.Debugf("Container %s not found in real sandbox, already deleted.", c.id)
			} else if err != nil {
				return err
			}
		}
	}

	if c.mounted {
		innerRootfs := filepath.Join(c.bundle, "rootfs")
		if err := mount.UnmountAll(innerRootfs, 0); err != nil {
			return err
		}
		c.mounted = false
	}

	s.deleteShimTask(c.id)

	return nil
}
