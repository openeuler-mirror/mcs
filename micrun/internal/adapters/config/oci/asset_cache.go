package oci

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	fsutil "micrun/internal/support/fs"
	log "micrun/internal/support/logger"
)

// cacheRegularFile copies a regular source file to cacheDir and returns the
// cached path. Missing, empty, or non-regular paths are passed through so later
// validation can report errors with the original user-facing path.
func cacheRegularFile(cacheDir, sourcePath string) (string, error) {
	if sourcePath == "" {
		return "", nil
	}
	stat, err := os.Stat(sourcePath)
	if err != nil {
		return sourcePath, nil
	}
	if !stat.Mode().IsRegular() {
		return sourcePath, nil
	}

	destPath, cached, err := cacheRegularFileDestination(cacheDir, sourcePath, stat)
	if err != nil {
		return "", err
	}
	if cached {
		return destPath, nil
	}

	if err := copyRegularFileAtomically(cacheDir, sourcePath, destPath, stat.Mode().Perm()); err != nil {
		return "", err
	}
	log.Debugf("copied %s to safe location %s", sourcePath, destPath)
	return destPath, nil
}

func copyRegularFileAtomically(cacheDir, sourcePath, destPath string, mode os.FileMode) (err error) {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", sourcePath, err)
	}
	defer sourceFile.Close()

	tempFile, err := os.CreateTemp(cacheDir, "."+filepath.Base(destPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary cache file for %s: %w", destPath, err)
	}
	tempPath := tempFile.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err = io.Copy(tempFile, sourceFile); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("failed to copy from %s to temporary cache file %s: %w", sourcePath, tempPath, err)
	}
	if err = tempFile.Chmod(mode); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("failed to chmod temporary cache file %s: %w", tempPath, err)
	}
	if err = tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("failed to sync temporary cache file %s: %w", tempPath, err)
	}
	if err = tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary cache file %s: %w", tempPath, err)
	}
	if err = os.Rename(tempPath, destPath); err != nil {
		return fmt.Errorf("failed to promote temporary cache file %s to %s: %w", tempPath, destPath, err)
	}
	return fsutil.SyncDir(cacheDir)
}

func cacheRegularFileDestination(cacheDir, sourcePath string, sourceInfo os.FileInfo) (string, bool, error) {
	destPath := filepath.Join(cacheDir, filepath.Base(sourcePath))
	destInfo, err := os.Stat(destPath)
	if err != nil {
		if os.IsNotExist(err) {
			return destPath, false, nil
		}
		return "", false, fmt.Errorf("failed to inspect destination file %s: %w", destPath, err)
	}

	if os.SameFile(sourceInfo, destInfo) {
		return destPath, true, nil
	}
	if destInfo.Mode().IsRegular() {
		sameContent, err := sameFileContent(sourcePath, destPath)
		if err != nil {
			return "", false, err
		}
		if sameContent {
			return destPath, true, nil
		}
	}

	return disambiguatedCachePath(cacheDir, sourcePath), false, nil
}

func sameFileContent(left, right string) (bool, error) {
	leftDigest, err := sha256FileDigest(left)
	if err != nil {
		return false, fmt.Errorf("failed to hash source file %s: %w", left, err)
	}
	rightDigest, err := sha256FileDigest(right)
	if err != nil {
		return false, fmt.Errorf("failed to hash destination file %s: %w", right, err)
	}
	return leftDigest == rightDigest, nil
}

func sha256FileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func disambiguatedCachePath(cacheDir, sourcePath string) string {
	base := filepath.Base(sourcePath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem = "asset"
	}

	hashSource, err := filepath.Abs(sourcePath)
	if err != nil {
		hashSource = sourcePath
	}
	sum := sha256.Sum256([]byte(hashSource))
	return filepath.Join(cacheDir, fmt.Sprintf("%s-%s%s", stem, hex.EncodeToString(sum[:])[:12], ext))
}
