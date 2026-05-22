package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	"micrun/internal/support/fs"
	"micrun/internal/support/statekey"
)

// Store is a simple file-backed runtime state store.
// It is intentionally generic so existing sandbox/container state formats can
// be migrated incrementally without changing callers all at once.
type Store struct {
	root string
}

func New(root string) *Store {
	return &Store{root: strings.TrimSpace(root)}
}

var _ ports.StateStore = (*Store)(nil)

var ErrInvalidStateKey = statekey.ErrInvalid
var ErrInvalidStateRoot = errors.New("invalid state store root")

func (s *Store) Load(ctx context.Context, namespace, taskID string) (*ports.RuntimeSnapshot, error) {
	location, err := s.snapshotLocationFor(ctx, namespace, taskID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(location.path)
	if err != nil {
		return nil, err
	}
	return &ports.RuntimeSnapshot{
		Namespace: location.namespace,
		TaskID:    location.taskID,
		Data:      data,
	}, nil
}

func (s *Store) Save(ctx context.Context, snapshot *ports.RuntimeSnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("nil snapshot")
	}
	location, err := s.snapshotLocationFor(ctx, snapshot.Namespace, snapshot.TaskID)
	if err != nil {
		return err
	}
	path := location.path
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileAtomically(path, snapshot.Data, 0o644)
}

func (s *Store) Delete(ctx context.Context, namespace, taskID string) error {
	location, err := s.snapshotLocationFor(ctx, namespace, taskID)
	if err != nil {
		return err
	}
	snapshotPath := location.path
	taskDir := filepath.Dir(snapshotPath)
	if err := os.Remove(snapshotPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return removeEmptyParents(location.root, taskDir)
}

func (s *Store) snapshotPath(namespace, taskID string) string {
	return filepath.Join(s.snapshotDir(namespace, taskID), "runtime.json")
}

func (s *Store) snapshotDir(namespace, taskID string) string {
	return filepath.Join(s.root, namespace, taskID)
}

func stateStoreContextErr(ctx context.Context) error {
	return contextx.OrBackground(ctx).Err()
}

type snapshotLocation struct {
	root      string
	path      string
	namespace string
	taskID    string
}

func (s *Store) snapshotLocationFor(ctx context.Context, namespace, taskID string) (snapshotLocation, error) {
	if err := stateStoreContextErr(ctx); err != nil {
		return snapshotLocation{}, err
	}

	root, err := s.rootDir()
	if err != nil {
		return snapshotLocation{}, err
	}
	normNamespace, err := statekey.Normalize(namespace)
	if err != nil {
		return snapshotLocation{}, err
	}
	normTaskID, err := statekey.Normalize(taskID)
	if err != nil {
		return snapshotLocation{}, err
	}
	return snapshotLocation{
		root:      root,
		path:      filepath.Join(root, normNamespace, normTaskID, "runtime.json"),
		namespace: normNamespace,
		taskID:    normTaskID,
	}, nil
}

func (s *Store) rootDir() (string, error) {
	if s == nil {
		return "", fmt.Errorf("%w: nil store", ErrInvalidStateRoot)
	}
	root, err := fs.CleanAbsolutePath(s.root)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidStateRoot, err)
	}
	return root, nil
}

func removeEmptyParents(root, start string) error {
	root = filepath.Clean(root)
	dir := filepath.Clean(start)

	for dir != root {
		if !pathWithinRoot(root, dir) {
			return fmt.Errorf("state cleanup path %s is outside root %s", dir, root)
		}

		if err := os.Remove(dir); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				dir = filepath.Dir(dir)
				continue
			}
			if isDirectoryNotEmpty(err) {
				return nil
			}
			return err
		}
		dir = filepath.Dir(dir)
	}
	return nil
}

func pathWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func isDirectoryNotEmpty(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST)
}

func writeFileAtomically(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".runtime-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	cleanupTemp := func() {
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanupTemp()
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		cleanupTemp()
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanupTemp()
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanupTemp()
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		cleanupTemp()
		return err
	}

	return fs.SyncDir(filepath.Dir(path))
}
