package log

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	// ContainerdLogPathEnv overrides the path used when opening containerd shim log output.
	ContainerdLogPathEnv = "MICRUN_CONTAINERD_LOG_PATH"

	// DefaultContainerdLogPath is the fallback relative path for containerd logs.
	DefaultContainerdLogPath = "log"
)

// openContainerdLog opens the containerd-provided log fifo.
// containerd creates a fifo named "log" in the shim's current working directory.
// For testing and environments that don't provide a fifo at that path,
// MICRUN_CONTAINERD_LOG_PATH can override the target path.
func openContainerdLog() (io.WriteCloser, error) {
	logPath := strings.TrimSpace(os.Getenv(ContainerdLogPathEnv))
	if logPath == "" {
		logPath = DefaultContainerdLogPath
	}

	f, err := os.OpenFile(logPath, os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open containerd log fifo at %s: %w", logPath, err)
	}
	return f, nil
}
