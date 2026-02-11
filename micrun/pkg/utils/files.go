package utils

import (
	"debug/elf"
	"encoding/json"
	"fmt"
	"io"
	defs "micrun/definitions"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	cdtypes "github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/mount"

	log "micrun/logger"
)

func IsRegular(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return stat.Mode().IsRegular()
}

func isFifo(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeNamedPipe != 0
}

func isSymlink(path string) bool {
	stat, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeSymlink != 0
}

// return absolute and non-link path
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

// EnsureRegularFilePath validates that path exists and points to a regular file.
// It returns the resolved absolute path for downstream use.
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

// getAllParentPaths returns all the parent directories of a path, including itself but excluding root directory "/".
// For example, "/foo/bar/biz" returns {"/foo", "/foo/bar", "/foo/bar/biz"}
func getAllParentPaths(path string) []string {
	if path == "/" || path == "." {
		return []string{}
	}

	paths := []string{filepath.Clean(path)}
	cur := path
	var parent string
	for cur != "/" && cur != "." {
		parent = filepath.Dir(cur)
		paths = append([]string{parent}, paths...)
		cur = parent
	}
	// remove the "/" or "." from the return result
	return paths[1:]
}

// mkdirAllWithInheritedOwner creates a directory named path, along with any necessary parents.
// It creates the missing directories with the ownership of the last existing parent.
// The path needs to be absolute and the method doesn't handle symlink.
func mkdirAllWithInheritedOwner(path string, perm os.FileMode) error {
	if len(path) == 0 {
		return fmt.Errorf("path cannot be empty")
	}

	// By default, use the uid and gid of the calling process.
	var uid = os.Getuid()
	var gid = os.Getgid()

	paths := getAllParentPaths(path)
	for _, curPath := range paths {
		info, err := os.Stat(curPath)

		if err != nil {
			if err = os.MkdirAll(curPath, perm); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			if err = syscall.Chown(curPath, uid, gid); err != nil {
				return fmt.Errorf("failed to change ownership: %w", err)
			}
			continue
		}

		if !info.IsDir() {
			return &os.PathError{Op: "mkdir", Path: curPath, Err: syscall.ENOTDIR}
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			uid = int(stat.Uid)
			gid = int(stat.Gid)
		} else {
			return fmt.Errorf("failed to retrieve UID and GID for path: %s", curPath)
		}
	}
	return nil
}

func RestoreStructFromJSON(file string) (any, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	var value any
	err = json.Unmarshal(content, &value)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	return value, nil
}

func SaveStructToJSON(file string, state any) error {
	structBytes, err := json.Marshal(state)
	if err != nil {
		log.Pretty("err: %v, state: %v", err, state)
		return fmt.Errorf("failed to serialize struct: %w", err)
	}
	log.Infof("[SaveStructToJSON] Serialized %d bytes for %s", len(structBytes), file)

	// Create directory if needed
	if err := os.MkdirAll(filepath.Dir(file), defs.DirMode); err != nil {
		log.Errorf("SaveStructToJSON: failed to create directory for %s: %v", file, err)
		return err
	}
	log.Infof("[SaveStructToJSON] Created directory for %s", filepath.Dir(file))

	// Open file with write flags
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, defs.FileMode)
	if err != nil {
		log.Errorf("SaveStructToJSON: failed to open %s: %v", file, err)
		return err
	}
	log.Infof("[SaveStructToJSON] Opened file %s", file)

	// Write data
	if _, err := f.Write(structBytes); err != nil {
		f.Close()
		log.Errorf("SaveStructToJSON: failed to write to %s: %v", file, err)
		return err
	}
	log.Infof("[SaveStructToJSON] Wrote %d bytes to %s", len(structBytes), file)

	// Sync to disk - ensures data is flushed to filesystem storage
	// This is critical for state persistence before process exit
	if err := f.Sync(); err != nil {
		f.Close()
		log.Errorf("SaveStructToJSON: failed to sync %s: %v", file, err)
		return err
	}

	if err := f.Close(); err != nil {
		log.Errorf("SaveStructToJSON: failed to close %s: %v", file, err)
		return err
	}

	log.Infof("[SaveStructToJSON] Successfully wrote and synced %s", file)
	return nil
}

func SetReadonly(path string) error {
	// assume path is a valid direntry
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

// removeExternalStatFile removes state file in micrun state directory
func removeExternalStatFile(id string) error {
	// if the file does not exist, return nil
	path := filepath.Join(defs.MicrunStateDir, id, defs.MicrunContainerStateFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(path)
}

func removeStateDir(id string) error {
	return os.RemoveAll(filepath.Join(defs.MicrunStateDir, id))
}

func RemoveContainerCacheDir(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("container id cannot be empty")
	}
	return os.RemoveAll(filepath.Join(defs.DefaultMicaContainersRoot, id))
}

func IsELFForHost(path string) (bool, error) {
	f, err := elf.Open(path)
	if err != nil {
		return false, nil
	}
	defer f.Close()

	switch runtime.GOARCH {
	case "arm64":
		return f.Machine == elf.EM_AARCH64, nil
	case "amd64":
		return f.Machine == elf.EM_X86_64, nil
	case "arm":
		// unsupported yet
		return f.Machine == elf.EM_ARM, nil
	case "riscv64":
		// unsupported yet
		return f.Machine == elf.EM_RISCV, nil
	default:
		// Unknown host arch: no strict check.
		return false, nil
	}
}

func MountDirs(mounts []*cdtypes.Mount, dest string) error {
	if len(mounts) == 0 {
		return nil
	}

	if err := os.Mkdir(dest, 0711); err != nil && !os.IsExist(err) {
		return err
	}
	for _, rm := range mounts {

		m := &mount.Mount{
			Type:    rm.Type,
			Source:  rm.Source,
			Options: rm.Options,
		}

		if err := m.Mount(dest); err != nil {
			return fmt.Errorf("failed to mount to %s: %v", dest, err)
		}
	}
	return nil

}
func backup(srcDir string) error {
	backupDir := filepath.Join(defs.MicrunStateDir, defs.BackupDirName)

	// Test source directory access first
	if stat, err := os.Stat(srcDir); err != nil {
		log.Debugf("Source directory access failed: %s - %v", srcDir, err)
		return fmt.Errorf("failed to access source directory: %w", err)
	} else {
		log.Debugf("Source directory access OK: %s - mode: %v", srcDir, stat.Mode())
	}

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	fileCount := 0
	dirCount := 0
	skipCount := 0

	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Debugf("BACKUP Warning: skipping %s due to error: %v", path, err)
			skipCount++
			return nil
		}

		if !IsRegular(path) && !info.IsDir() {
			log.Debugf("BACKUP Skipping non-regular file: %s", path)
			skipCount++
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			log.Debugf("BACKUP failed to get relative path for %s: %v", path, err)
			skipCount++
			return nil
		}

		destPath := filepath.Join(backupDir, relPath)

		if info.IsDir() {
			dirCount++
			if err := os.MkdirAll(destPath, info.Mode()); err != nil {
				log.Debugf("BACKUP failed to create directory %s: %v", destPath, err)
				return nil
			}
		} else {
			fileCount++
			log.Debugf("BACKUP copying %s to %s", relPath, destPath)
			if err := copyFile(path, destPath, info.Mode()); err != nil {
				log.Debugf("BACKUP failed to copy file %s: %v", path, err)
				return nil
			}
		}

		return nil
	})

	log.Debugf("=== BACKUP: completed ===")
	log.Debugf("BACKUP Summary: %d dirs, %d files copied, %d items skipped", dirCount, fileCount, skipCount)

	// Show what was actually backed up
	if entries, err := os.ReadDir(backupDir); err != nil {
		log.Debugf("BACKUP failed to read backup directory: %v", err)
	} else {
		log.Debugf("BACKUP directory contains %d items:", len(entries))
		for i, entry := range entries {
			if i < 10 {
				if entry.IsDir() {
					log.Debugf("  [DIR]  %s", entry.Name())
				} else {
					log.Debugf("  [FILE] %s", entry.Name())
				}
			}
		}
		if len(entries) > 10 {
			log.Debugf("  ... and %d more items", len(entries)-10)
		}
	}

	return err
}

// copyFile copies a single file from src to dst with the given permissions
func copyFile(src, dst string, mode os.FileMode) error {
	// Create destination directory if needed
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Open source file
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer srcFile.Close()

	// Create destination file
	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", dst, err)
	}
	defer dstFile.Close()

	// Copy file contents
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	// Set file permissions
	if err := os.Chmod(dst, mode); err != nil {
		return fmt.Errorf("failed to set file permissions: %w", err)
	}

	return nil
}

func TravelDir(root string) error {
	var treeBuilder strings.Builder
	treeBuilder.WriteString("\n" + root)

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

		var prefix string
		for i := range depth {
			if i == depth-1 {
				prefix += "├── "
			} else {
				prefix += "│   "
			}
		}

		if info.IsDir() {
			treeBuilder.WriteString(fmt.Sprintf("%s%s/\n", prefix, info.Name()))
		} else {
			treeBuilder.WriteString(fmt.Sprintf("%s%s\n", prefix, info.Name()))
		}

		return nil
	})

	if err != nil {
		return err
	}

	log.Debugf("%s", treeBuilder.String())
	return nil
}
