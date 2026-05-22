package container

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	defs "micrun/internal/support/definitions"
	"micrun/internal/support/fs"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"
)

type legacyStateRepository struct {
	sandboxRoot   string
	containerRoot string
}

func defaultLegacyStateRepository() legacyStateRepository {
	return legacyStateRepositoryWithRoots(legacySandboxStateRootDir, legacyContainerStateRootDir)
}

func legacyStateRepositoryWithRoots(sandboxRoot, containerRoot string) legacyStateRepository {
	return legacyStateRepository{
		sandboxRoot:   sandboxRoot,
		containerRoot: containerRoot,
	}
}

func (r legacyStateRepository) sandboxStatePath(id string) string {
	root, err := cleanLegacyStateRoot(r.sandboxRoot)
	if err != nil {
		return ""
	}
	cleanID, err := validation.NormalizeSinglePathSegment(id)
	if err != nil {
		return ""
	}
	return filepath.Join(root, cleanID, defs.SandboxStateFile)
}

func (r legacyStateRepository) loadSandboxState(id string) (*SandboxStorage, string, error) {
	path := r.sandboxStatePath(id)
	storage, err := loadLegacyJSON[SandboxStorage](path)
	return storage, path, err
}

func (r legacyStateRepository) removeSandboxState(id string) error {
	root, err := cleanLegacyStateRoot(r.sandboxRoot)
	if err != nil {
		return fmt.Errorf("legacy sandbox root is invalid: %w", err)
	}
	cleanID, err := validation.NormalizeSinglePathSegment(id)
	if err != nil {
		return fmt.Errorf("legacy sandbox id is invalid: %w", err)
	}
	return os.RemoveAll(filepath.Join(root, cleanID))
}

func (r legacyStateRepository) containerStatePath(containerPath string) string {
	root, err := cleanLegacyStateRoot(r.containerRoot)
	if err != nil {
		return ""
	}
	clean := normalizedContainerPath(containerPath)
	if clean == "" {
		return ""
	}
	return filepath.Join(root, clean, defs.MicrunContainerStateFile)
}

func (r legacyStateRepository) containerStatePathByID(id string) string {
	root, err := cleanLegacyStateRoot(r.containerRoot)
	if err != nil {
		return ""
	}
	cleanID, err := validation.NormalizeSinglePathSegment(id)
	if err != nil {
		return ""
	}
	return filepath.Join(root, cleanID, defs.MicrunContainerStateFile)
}

func cleanLegacyStateRoot(root string) (string, error) {
	return fs.CleanAbsolutePath(root)
}

func (r legacyStateRepository) containerStatePaths(containerPath, id string, extraPaths []string) []string {
	candidates := []string{
		r.containerStatePath(containerPath),
		r.containerStatePathByID(id),
	}
	candidates = append(candidates, extraPaths...)

	seen := make(map[string]struct{}, len(candidates))
	paths := make([]string, 0, len(candidates))
	for _, path := range candidates {
		path = cleanLegacyContainerStateFilePath(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

func cleanLegacyContainerStateFilePath(path string) string {
	clean, err := fs.CleanAbsolutePath(path)
	if err != nil {
		return ""
	}
	if filepath.Base(clean) != defs.MicrunContainerStateFile {
		return ""
	}
	return clean
}

func (r legacyStateRepository) loadContainerState(paths []string) (*ContainerStorage, string, error) {
	for _, path := range paths {
		storage, err := loadLegacyJSON[ContainerStorage](path)
		if err == nil {
			return storage, path, nil
		}
		if !os.IsNotExist(err) {
			return nil, path, fmt.Errorf("failed to restore legacy container state from %s: %w", path, err)
		}
	}
	return nil, "", os.ErrNotExist
}

func (r legacyStateRepository) removeContainerStateFiles(paths []string) {
	for _, path := range paths {
		r.removeContainerStateFile(path)
	}
}

func (r legacyStateRepository) removeContainerStateFile(path string) {
	clean := cleanLegacyContainerStateFilePath(path)
	if clean == "" {
		return
	}
	if err := os.Remove(clean); err != nil && !os.IsNotExist(err) {
		log.Warnf("failed to remove legacy state file %s: %v", clean, err)
	}
}

func loadLegacyJSON[T any](path string) (*T, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value T
	if err := json.Unmarshal(content, &value); err != nil {
		return nil, fmt.Errorf("failed to unmarshal legacy JSON: %w", err)
	}
	return &value, nil
}
