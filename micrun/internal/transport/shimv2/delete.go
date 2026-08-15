package shim

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/mount"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"
)

// deleteContainer handles the deletion of a container, including stopping it if necessary and unmounting its rootfs.
func deleteContainer(ctx context.Context, s *shimService, c *shimContainer) error {
	if c == nil {
		return nil
	}
	// Serialize the stop+delete sequence so concurrent Delete RPCs for the
	// same container do not double-stop the guest domain (the loser would
	// fail with a spurious Remove error).
	c.deleteMu.Lock()
	defer c.deleteMu.Unlock()

	// Forcibly delete pod containers.
	if !c.cType.CanBeSandbox() {
		sandbox, hasSandbox := s.currentSandbox()
		// Check if sandbox still exists before trying to stop/delete container
		if !hasSandbox {
			log.Debugf("Sandbox already deleted, skipping StopContainer/DeleteContainer for %s", c.id)
		} else {
			// Always attempt StopContainer: the domain layer's c.stop checks
			// the real guest state and is a no-op if the container is already
			// stopped. Relying on the in-memory shim task status here is
			// unsafe because forceStopIfActive may have already marked the
			// task STOPPED without actually stopping the guest domain,
			// which would skip this call and orphan the domain.
			// Detach from RPC cancellation: every sibling teardown path
			// (reconcileSandbox, teardownSandbox, killPodContainer) does the
			// same — a client ttrpc deadline firing mid Stop/Delete would
			// abort the guest calls, orphan the domain, and surface a spurious
			// Delete failure (retry converges, but the partial stop is wrong).
			teardownCtx := context.WithoutCancel(ctx)
			if _, err := sandbox.StopContainer(teardownCtx, c.id, false); err != nil && errors.Is(err, er.ContainerNotFound) {
				log.Debugf("Container %s not found in real sandbox, already deleted.", c.id)
			} else if err != nil {
				return err
			}
			if _, err := sandbox.DeleteContainer(teardownCtx, c.id); err != nil && errors.Is(err, er.ContainerNotFound) {
				log.Debugf("Container %s not found in real sandbox, already deleted.", c.id)
			} else if err != nil {
				return err
			}
		}
	}

	// Stop the IO session (copier goroutines, epoll/fifo/tty fds) BEFORE the
	// unmount: IO teardown does not depend on the rootfs, but an unmount
	// failure (busy rootfs, permission error) used to return early and leak
	// every copier goroutine + epoll/fifo/tty fd on each failed Delete retry.
	// Use the IOManager() accessor so the ioManager field read is serialized
	// against concurrent SetIOManager calls from attach/IO-event goroutines.
	if mgr := c.IOManager(); !validation.IsNil(mgr) {
		mgr.Stop()
	}

	if c.mounted {
		innerRootfs := filepath.Join(c.bundle, "rootfs")
		if err := mount.UnmountAll(innerRootfs, 0); err != nil {
			return err
		}
		c.mounted = false
	} else if c.recovered {
		// A shim crash + recovery loses the mounted/bundle bookkeeping, but
		// the Create-time rootfs mount may still be alive under the
		// conventional bundle path (the daemon shim runs with cwd = sandbox
		// bundle; pod containers live in sibling bundle dirs). Best-effort
		// unmount: UnmountAll is a no-op for a non-mountpoint (EINVAL) or a
		// missing path, so this is safe when nothing was mounted.
		if recoveredRootfs := recoveredTaskRootfs(c); recoveredRootfs != "" {
			if err := mount.UnmountAll(recoveredRootfs, 0); err != nil {
				log.Warnf("failed to unmount recovered task rootfs %s: %v", recoveredRootfs, err)
			}
		}
	}

	return nil
}

// recoveredTaskRootfs reconstructs the conventional bundle rootfs path for a
// recovered task: the daemon shim runs with cwd = the sandbox task's bundle,
// and a pod container's bundle is its sibling under the namespace directory.
func recoveredTaskRootfs(c *shimContainer) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	bundle := cwd
	if !c.cType.CanBeSandbox() {
		bundle = filepath.Join(filepath.Dir(cwd), c.id)
	}
	return filepath.Join(bundle, "rootfs")
}
