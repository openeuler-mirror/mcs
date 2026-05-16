package shim

import (
	"context"
	"fmt"
	"path/filepath"

	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"

	"github.com/containerd/containerd/mount"
)

func cleanupContainer(ctx context.Context, guestControl ports.GuestControl, deps *cntr.Dependencies, sandboxID, containerID, bundle string) error {
	log.Debugf("cleanup container from sandbox %s, and remove rootfs of container %s", sandboxID, containerID)
	if validation.IsNil(guestControl) {
		return fmt.Errorf("guest control is required")
	}
	if err := cntr.CleanupContainerWithDependencies(ctx, guestControl, sandboxID, containerID, false, deps); err != nil {
		return fmt.Errorf("failed to cleanup container %s: %w", containerID, err)
	}

	rootfs := filepath.Join(bundle, "rootfs")
	if err := mount.UnmountAll(rootfs, 0); err != nil {
		log.Errorf("failed to umount: %s", rootfs)
		return err
	}
	return nil
}
