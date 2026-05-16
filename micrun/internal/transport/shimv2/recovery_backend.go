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
	podmanannotations "github.com/containers/podman/v4/pkg/annotations"
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
	if s != nil && s.config != nil {
		backend.containersDir = oci.ContainerCacheRoot(s.config.StateDir)
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
	isRunning := sandboxState == cntr.StateRunning
	if isRunning {
		log.Infof("[RESTORE] Sandbox %s is RUNNING, containers will be marked as RUNNING", id)
	} else {
		log.Infof("[RESTORE] Sandbox %s is %s, containers will be marked as CREATED", id, sandboxState)
	}

	containers := sandbox.GetAllContainers()
	restored, err := recoveredTasksFromContainers(containers, isRunning)
	if err != nil {
		return nil, nil, err
	}

	return runtimeSandbox{SandboxTraits: sandbox}, restored, nil
}

func recoveredTasksFromContainers(containers []cntr.ContainerTraits, isRunning bool) ([]ports.RecoveredTask, error) {
	restored := make([]ports.RecoveredTask, 0, len(containers))
	for i, c := range containers {
		task, err := recoveredTaskFromContainer(c, isRunning)
		if err != nil {
			return nil, fmt.Errorf("recovered container[%d]: %w", i, err)
		}
		restored = append(restored, task)
	}
	return restored, nil
}

func recoveredTaskFromContainer(container cntr.ContainerTraits, isRunning bool) (ports.RecoveredTask, error) {
	if container == nil {
		return ports.RecoveredTask{}, fmt.Errorf("recovered container is nil")
	}
	if container.ID() == "" {
		return ports.RecoveredTask{}, fmt.Errorf("recovered container id is empty")
	}
	canSandbox, isSandbox := recoveredTaskSandboxRole(container.GetAnnotations())
	return ports.RecoveredTask{
		ID:         container.ID(),
		CanSandbox: canSandbox,
		IsSandbox:  isSandbox,
		IsRunning:  isRunning,
	}, nil
}

func recoveredTaskSandboxRole(annotations map[string]string) (canSandbox bool, isSandbox bool) {
	if hasRecoveredTaskAnnotation(annotations, oci.CRISandboxNameKeyList) {
		return false, false
	}
	if isRecoveredCRISandbox(annotations) {
		return true, true
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
	return containerType == ctrannotations.ContainerTypeSandbox ||
		containerType == podmanannotations.ContainerTypeSandbox
}

func hasRecoveredTaskAnnotation(annotations map[string]string, keys []string) bool {
	for _, key := range keys {
		if _, ok := annotations[key]; ok {
			return true
		}
	}
	return false
}
