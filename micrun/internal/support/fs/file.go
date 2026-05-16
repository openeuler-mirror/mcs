package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cdtypes "github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/mount"

	defs "micrun/internal/support/definitions"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"
)

func IsRegular(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return stat.Mode().IsRegular()
}

func ResolvePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path must be specified")
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file does not exist: %s", absolute)
		}
		return "", err
	}

	return resolved, nil
}

func EnsureRegularFilePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path must be specified")
	}

	resolved, err := ResolvePath(path)
	if err != nil {
		return "", err
	}

	if !IsRegular(resolved) {
		return "", fmt.Errorf("expected regular file: %s", resolved)
	}

	return resolved, nil
}

func FileExist(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func CleanAbsolutePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	if trimmed != path {
		return "", fmt.Errorf("path cannot contain surrounding whitespace")
	}
	if strings.ContainsRune(path, '\x00') {
		return "", fmt.Errorf("path cannot contain NUL byte")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be absolute: %s", path)
	}
	return filepath.Clean(path), nil
}

func SyncDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func EnsureDir(path string, mode os.FileMode) error {
	cleanPath, err := CleanAbsolutePath(path)
	if err != nil {
		return err
	}

	if fi, err := os.Stat(cleanPath); err != nil {
		if os.IsNotExist(err) {
			if err = os.MkdirAll(cleanPath, mode); err != nil {
				return err
			}
		} else {
			return err
		}
	} else if !fi.IsDir() {
		return fmt.Errorf("not a directory: %s", cleanPath)
	}

	return nil
}

func SetReadonly(path string) error {
	return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0444)
		if info.IsDir() {
			mode = os.FileMode(0555)
		}
		return os.Chmod(path, mode)
	})
}

func RemoveContainerCacheDir(id string) error {
	return RemoveContainerCacheDirAt(defs.DefaultMicaContainersRoot, id)
}

func RemoveContainerCacheDirAt(containerRoot, id string) error {
	root, err := CleanAbsolutePath(containerRoot)
	if err != nil {
		return fmt.Errorf("container root is invalid: %w", err)
	}

	cleanID, err := validation.NormalizeSinglePathSegment(id)
	if err != nil {
		return fmt.Errorf("container id is invalid: %w", err)
	}
	return os.RemoveAll(filepath.Join(root, cleanID))
}

func MountDirs(mounts []*cdtypes.Mount, dest string) error {
	if len(mounts) == 0 {
		return nil
	}

	cleanDest, err := CleanAbsolutePath(dest)
	if err != nil {
		return fmt.Errorf("mount destination is invalid: %w", err)
	}
	for idx, rm := range mounts {
		if rm == nil {
			return fmt.Errorf("mount %d is nil", idx)
		}
	}

	if err := EnsureDir(cleanDest, 0o711); err != nil {
		return fmt.Errorf("mount destination is invalid: %w", err)
	}
	for _, rm := range mounts {
		m := &mount.Mount{
			Type:    rm.Type,
			Source:  rm.Source,
			Options: rm.Options,
		}

		if err := m.Mount(cleanDest); err != nil {
			return fmt.Errorf("failed to mount to %s: %w", cleanDest, err)
		}
	}
	return nil
}

func TravelDir(root string) error {
	tree, err := DirectoryTree(root)
	if err != nil {
		return err
	}
	log.Debugf("%s", tree)
	return nil
}

func DirectoryTree(root string) (string, error) {
	var treeBuilder strings.Builder
	treeBuilder.WriteString("\n")
	treeBuilder.WriteString(root)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			treeBuilder.WriteString("\n")
			return nil
		}

		parts := strings.Split(relPath, string(os.PathSeparator))
		depth := len(parts) - 1

		prefix := treeEntryPrefix(depth)

		if info.IsDir() {
			treeBuilder.WriteString(prefix)
			treeBuilder.WriteString(info.Name())
			treeBuilder.WriteString("/\n")
		} else {
			treeBuilder.WriteString(prefix)
			treeBuilder.WriteString(info.Name())
			treeBuilder.WriteString("\n")
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	return treeBuilder.String(), nil
}

func treeEntryPrefix(depth int) string {
	var prefixBuilder strings.Builder
	for range depth {
		prefixBuilder.WriteString("│   ")
	}
	prefixBuilder.WriteString("├── ")
	return prefixBuilder.String()
}
