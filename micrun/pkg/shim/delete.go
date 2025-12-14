package shim

import (
	"context"
	"path/filepath"

	er "micrun/errors"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/mount"
	log "micrun/logger"
)

// deleteContainer handles the deletion of a container, including stopping it if necessary and unmounting its rootfs.
func deleteContainer(ctx context.Context, s *shimService, c *shimContainer) error {
	if c == nil {
		return nil
	}

	// Forcibly delete pod containers.
	if !c.cType.CanBeSandbox() {
		// Check if sandbox still exists before trying to stop/delete container
		if s.sandbox == nil {
			log.Debugf("Sandbox already deleted, skipping StopContainer/DeleteContainer for %s", c.id)
		} else {
			if c.status != task.Status_STOPPED {
				if _, err := s.sandbox.StopContainer(ctx, c.id, false); err != nil && err == er.ContainerNotFound {
					log.Debugf("Container %s not found in real sandbox, already deleted.", c.id)
				} else if err != nil {
					return err
				}
			}
			if _, err := s.sandbox.DeleteContainer(ctx, c.id); err != nil && err == er.ContainerNotFound {
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

	delete(s.containers, c.id)

	return nil
}
