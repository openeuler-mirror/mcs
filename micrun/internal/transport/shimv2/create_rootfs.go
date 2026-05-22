package shim

import (
	"fmt"

	cntr "micrun/internal/domain/container"
	"micrun/internal/support/fs"
	log "micrun/internal/support/logger"

	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/mount"
)

func withMountedRootfs(rootfsPath string, mounts []*types.Mount, rootfs *cntr.RootFs, run func() error) (retErr error) {
	if rootfs == nil {
		return fmt.Errorf("rootfs state is required")
	}
	mounted, err := mountRootfs(rootfsPath, mounts)
	if err != nil {
		return err
	}
	rootfs.Mounted = mounted

	defer func() {
		if retErr == nil || !rootfs.Mounted {
			return
		}
		if err := mount.UnmountAll(rootfsPath, 0); err != nil {
			log.Warnf("failed to clean up rootfs mount: %v", err)
		}
	}()

	return run()
}

// mountRootfs mounts the container's root filesystem.
func mountRootfs(rootfsPath string, rootfs []*types.Mount) (bool, error) {
	// NOTICE: Only one rootfs is supported.
	if len(rootfs) != 1 {
		log.Warnf("Only support one rootfs in bundle.")
	}
	if len(rootfs) == 0 {
		return false, nil
	}

	if err := fs.MountDirs(rootfs, rootfsPath); err != nil {
		return false, err
	}
	return true, nil
}
