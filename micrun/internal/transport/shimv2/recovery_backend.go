package shim

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	oci "micrun/internal/adapters/config/oci"
	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	defs "micrun/internal/support/definitions"
	"micrun/internal/support/fs"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"

	ctrannotations "github.com/containerd/containerd/pkg/cri/annotations"
)

type shimRecoveryBackend struct {
	guestControl  ports.GuestControl
	containerDeps *cntr.Dependencies
	containersDir string
	taskDirRoot   string
}

var _ ports.RecoveryBackend = (*shimRecoveryBackend)(nil)

func (s *shimService) recoveryBackend() shimRecoveryBackend {
	backend := shimRecoveryBackend{
		guestControl:  s.runtimeDeps.guestControl,
		containerDeps: s.runtimeDeps.containerDeps,
	}
	// Read s.config under the service lock: it is written by
	// loadRuntimeConfig during Create RPCs, which may run concurrently
	// with recovery.
	s.Lock()
	cfg := s.config
	s.Unlock()
	if cfg != nil {
		backend.containersDir = oci.ContainerCacheRoot(cfg.StateDir)
	}
	return backend
}

func (b shimRecoveryBackend) CleanupOrphans(ctx context.Context, namespace string) error {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	namespace, err := normalizeShimNamespace(namespace)
	if err != nil {
		return err
	}

	paths, err := b.cleanupPaths()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(paths.containersDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	var cleanupErr error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() {
			continue
		}

		containerID, err := normalizeRecoveryContainerID(entry.Name())
		if err != nil {
			err = fmt.Errorf("unsafe recovery container directory name %q: %w", entry.Name(), err)
			log.Errorf("%v", err)
			cleanupErr = errors.Join(cleanupErr, err)
			continue
		}

		if err := b.cleanupOrphan(paths.containersDir, paths.taskDirRoot, namespace, containerID); err != nil {
			log.Errorf("%v", err)
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}

	return cleanupErr
}

func (b shimRecoveryBackend) cleanupOrphan(containersDir, taskDirRoot, namespace, containerID string) error {
	taskDir := filepath.Join(taskDirRoot, namespace, containerID)
	if _, err := os.Stat(taskDir); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat task directory %s: %w", taskDir, err)
	}

	orphanPath := filepath.Join(containersDir, containerID)
	log.Infof("[CLEANUP] Removing orphaned container directory: %s", orphanPath)
	if err := os.RemoveAll(orphanPath); err != nil {
		return fmt.Errorf("failed to remove orphaned directory %s: %w", orphanPath, err)
	}
	return nil
}

type recoveryCleanupPaths struct {
	containersDir string
	taskDirRoot   string
}

func (b shimRecoveryBackend) cleanupPaths() (recoveryCleanupPaths, error) {
	containersDir := b.containersDir
	if containersDir == "" {
		containersDir = defs.DefaultMicaContainersRoot
	}
	taskDirRoot := b.taskDirRoot
	if taskDirRoot == "" {
		taskDirRoot = defs.ContainerdTaskDir
	}
	cleanContainersDir, err := fs.CleanAbsolutePath(containersDir)
	if err != nil {
		return recoveryCleanupPaths{}, fmt.Errorf("recovery containers directory is invalid: %w", err)
	}
	cleanTaskDirRoot, err := fs.CleanAbsolutePath(taskDirRoot)
	if err != nil {
		return recoveryCleanupPaths{}, fmt.Errorf("recovery task directory root is invalid: %w", err)
	}
	return recoveryCleanupPaths{containersDir: cleanContainersDir, taskDirRoot: cleanTaskDirRoot}, nil
}

func normalizeRecoveryContainerID(containerID string) (string, error) {
	normalized, err := validation.NormalizeSinglePathSegment(containerID)
	if err != nil {
		return "", fmt.Errorf("container id is invalid: %w", err)
	}
	return normalized, nil
}

func (b shimRecoveryBackend) Restore(ctx context.Context, id string) (ports.Sandbox, []ports.RecoveredTask, error) {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if b.guestControl == nil {
		return nil, nil, fmt.Errorf("guest control is required")
	}

	sandbox, err := cntr.LoadSandboxWithDependencies(ctx, id, b.guestControl, b.containerDeps)
	if err != nil {
		return nil, nil, err
	}

	sandboxState := sandbox.GetState()
	log.Infof("[RESTORE] Sandbox %s persisted state is %s; each container is recovered from its own persisted state", id, sandboxState)

	containers := sandbox.GetAllContainers()
	restored, err := recoveredTasksFromContainers(containers)
	if err != nil {
		return nil, nil, err
	}

	return runtimeSandbox{SandboxTraits: sandbox}, restored, nil
}

func recoveredTasksFromContainers(containers []cntr.ContainerTraits) ([]ports.RecoveredTask, error) {
	restored := make([]ports.RecoveredTask, 0, len(containers))
	for i, c := range containers {
		task, err := recoveredTaskFromContainer(c)
		if err != nil {
			return nil, fmt.Errorf("recovered container[%d]: %w", i, err)
		}
		restored = append(restored, task)
	}
	return restored, nil
}

func recoveredTaskFromContainer(container cntr.ContainerTraits) (ports.RecoveredTask, error) {
	if container == nil {
		return ports.RecoveredTask{}, fmt.Errorf("recovered container is nil")
	}
	if container.ID() == "" {
		return ports.RecoveredTask{}, fmt.Errorf("recovered container id is empty")
	}
	canSandbox, isSandbox := recoveredTaskSandboxRole(container.GetAnnotations())
	// Use the CONTAINER's own status, not the sandbox-level isRunning flag:
	// a container that was created but not yet started (Ready) must recover
	// as CREATED — marking it RUNNING would make the later Start RPC fail
	// with AlreadyExists and wedge the container forever. Stopped/Down
	// surfaces as STOPPED so Wait does not block on a channel nothing will
	// ever close. Paused keeps task.Status_PAUSED so Resume is not skipped
	// as already-running, while NeedsExitWatcher still covers the live domain.
	status := container.Status()
	isStopped := status == cntr.StateStopped || status == cntr.StateDown
	isPaused := status == cntr.StatePaused
	containerRunning := status == cntr.StateRunning
	return ports.RecoveredTask{
		ID:         container.ID(),
		CanSandbox: canSandbox,
		IsSandbox:  isSandbox,
		IsRunning:  containerRunning,
		IsPaused:   isPaused,
		IsStopped:  isStopped,
	}, nil
}

func recoveredTaskSandboxRole(annotations map[string]string) (canSandbox bool, isSandbox bool) {
	// container-type is authoritative and must be checked first: a real
	// containerd CRI sandbox spec carries BOTH container-type=sandbox AND
	// sandbox-id (DefaultCRIAnnotations writes sandbox-id unconditionally —
	// for a sandbox it holds the sandbox's own id). Checking sandbox-id first
	// misclassifies every recovered CRI pod sandbox as a pod container, whose
	// Kill/Delete then only converges the infra container and leaks the whole
	// guest domain as an orphan.
	if isRecoveredCRISandbox(annotations) {
		return true, true
	}
	if hasRecoveredTaskAnnotation(annotations, oci.CRISandboxNameKeyList) {
		return false, false
	}
	return true, false
}

func isRecoveredCRISandbox(annotations map[string]string) bool {
	for _, key := range oci.CRIContainerTypeKeyList {
		if isRecoveredCRISandboxType(annotations[key]) {
			return true
		}
	}
	return false
}

func isRecoveredCRISandboxType(containerType string) bool {
	// Match both containerd CRI ("sandbox") and CRI-O/podman ("sandbox")
	// container-type annotation values without importing the podman module.
	return containerType == ctrannotations.ContainerTypeSandbox ||
		containerType == "sandbox"
}

func hasRecoveredTaskAnnotation(annotations map[string]string, keys []string) bool {
	for _, key := range keys {
		if _, ok := annotations[key]; ok {
			return true
		}
	}
	return false
}
