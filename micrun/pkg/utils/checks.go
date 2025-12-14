package utils

import (
	"errors"
	"fmt"
	log "micrun/logger"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const maxClientIDLength = 66

var cidPattern = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9_.-]+$")

func ValidContainerID(id string) error {
	if id == "" {
		return fmt.Errorf("container ID cannot be empty")
	}

	if len(id) > maxClientIDLength {
		return fmt.Errorf("container/sandbox ID %q exceeds mica limit (%d characters)", id, maxClientIDLength)
	}

	if !cidPattern.MatchString(id) {
		return fmt.Errorf("invalid container/sandbox ID: %s", id)
	}
	return nil
}

func FileExist(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}

// EnsureDir check if a directory exist, if not then create it
func EnsureDir(path string, mode os.FileMode) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("not an absolute path: %s", path)
	}

	if fi, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			if err = os.MkdirAll(path, mode); err != nil {
				return err
			}
		} else {
			return err
		}
	} else if !fi.IsDir() {
		return fmt.Errorf("not a directory: %s", path)
	}

	return nil
}

func InList(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}

// LsofSocket returns a slice of PIDs using the given socket path
// It runs lsof to check which processes are using the socket
func LsofSocket(socketPath string, command string) []int {
	var pids []int
	cmd := exec.Command("lsof", socketPath)
	output, err := cmd.Output()
	if err != nil {
		log.Debugf("Failed to run lsof on %s: %v", socketPath, err)
		return pids
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "COMMAND") || line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if fields[0] != command {
				continue
			}
			if pid, err := strconv.Atoi(fields[1]); err == nil {
				pids = append(pids, pid)
			}
		}
	}
	return pids
}


// Validate the bundle and rootfs.
func ValidBundle(containerID, bundlePath string) (string, error) {
	if containerID == "" {
		return "", fmt.Errorf("container ID is empty")
	}

	if bundlePath == "" {
		return "", fmt.Errorf("bundle path is required")
	}

	// resolve path first to handle symlinks before other checks
	resolved, err := ResolvePath(bundlePath)
	if err != nil {
		return "", err
	}

	stat, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("invalid resolved bundle path '%s': %w", resolved, err)
	}
	if !stat.IsDir() {
		return "", fmt.Errorf("invalid resolved bundle path '%s', it should be a directory", resolved)
	}

	return resolved, nil
}
