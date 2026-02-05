// Package shim provides IO mode classification for container runtime.
// It centralizes the logic for handling different IO scenarios (TTY/non-TTY, foreground/background).
package shim

import (
	"fmt"
	"path/filepath"

	micrunio "micrun/pkg/io"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
)

// IOMode describes the IO session mode for a container.
type IOMode struct {
	// IsTTY indicates whether this is TTY mode.
	// Affects: terminal configuration, data processing method.
	IsTTY bool

	// IsForeground indicates whether this is foreground mode.
	// Foreground: shim exits when client disconnects.
	// Background: shim continues running, supports attach.
	IsForeground bool

	// HasStdin indicates whether interactive input is available.
	// Affects: whether to create stdin FIFO.
	HasStdin bool

	// SupportsDetach indicates whether detach is supported (nerdctl Ctrl+P Ctrl+Q).
	SupportsDetach bool

	// SupportsAttach indicates whether reattach is supported.
	SupportsAttach bool
}

// IO mode constants (for logging and debugging).
const (
	IOModeTTYForeground    = "TTY-foreground"     // -it
	IOModeTTYBackground    = "TTY-background"     // -it -d
	IOModeTTYReadOnly      = "TTY-read-only"      // -t (without -i)
	IOModeNonTTYForeground = "non-TTY-foreground" // (without -t, without -d)
	IOModeNonTTYBackground = "non-TTY-background" // -d
)

// String returns a human-readable representation of the IO mode.
func (m IOMode) String() string {
	if m.IsTTY {
		if m.IsForeground {
			return IOModeTTYForeground
		}
		if m.HasStdin {
			return IOModeTTYBackground
		}
		return IOModeTTYReadOnly
	}
	if m.IsForeground {
		return IOModeNonTTYForeground
	}
	return IOModeNonTTYBackground
}

// DetermineIOMode determines the IO mode from a CreateTaskRequest.
func DetermineIOMode(r *taskAPI.CreateTaskRequest) IOMode {
	mode := IOMode{
		IsTTY: r.Terminal,
	}

	// Determine foreground: has stdin FIFO = foreground
	mode.IsForeground = (r.Stdin != "")

	// Determine has interactive input: all modes support input (for ctr compatibility)
	// - With -i: use nerdctl-provided stdin (r.Stdin != "")
	// - Without -i: generate standard stdin FIFO for ctr compatibility
	mode.HasStdin = true

	// TTY mode supports detach (nerdctl Ctrl+P Ctrl+Q, or "back" command)
	mode.SupportsDetach = r.Terminal

	// Determine supports attach:
	// - TTY mode supports multiple attach (detach with "back" command, then reattach)
	// - Background mode supports attach
	mode.SupportsAttach = !mode.IsForeground || mode.IsTTY

	return mode
}

// GenerateFIFOPaths generates FIFO paths for all IO modes.
// This function centralizes the FIFO path generation logic using switch-case
// to handle different scenarios explicitly.
func GenerateFIFOPaths(r *taskAPI.CreateTaskRequest, namespace string) (stdin, stdout, stderr string) {
	ioMode := DetermineIOMode(r)

	switch ioMode.String() {
	case IOModeTTYForeground:
		// TTY foreground (-it): use nerdctl-provided FIFO paths
		stdin = r.Stdin
		stdout = r.Stdout
		stderr = r.Stderr

	case IOModeTTYBackground:
		// TTY background (-it -d): nerdctl provides binary://, convert to FIFO paths
		stdin = micrunio.GenerateStandardFIFOPath(namespace, r.ID, "stdin")
		stdout = convertOrGenerateFIFOPath(r.Stdout, namespace, r.ID, "stdout")
		stderr = convertOrGenerateFIFOPath(r.Stderr, namespace, r.ID, "stderr")

	case IOModeTTYReadOnly:
		// TTY read-only (-t without -i): generate stdin FIFO for ctr compatibility
		// Note: -i option is only available in nerdctl, not ctr
		stdin = micrunio.GenerateStandardFIFOPath(namespace, r.ID, "stdin")
		stdout = convertOrGenerateFIFOPath(r.Stdout, namespace, r.ID, "stdout")
		stderr = "" // TTY mode: stderr merged into stdout

	case IOModeNonTTYForeground:
		// non-TTY foreground: use nerdctl-provided FIFO paths or generate for ctr
		if r.Stdin != "" {
			stdin = r.Stdin // nerdctl provides stdin
		} else {
			stdin = micrunio.GenerateStandardFIFOPath(namespace, r.ID, "stdin") // ctr compatibility
		}
		stdout = r.Stdout
		stderr = r.Stderr

	case IOModeNonTTYBackground:
		// non-TTY background (-d): generate FIFO paths to support attach
		stdin = micrunio.GenerateStandardFIFOPath(namespace, r.ID, "stdin")
		stdout = convertOrGenerateFIFOPath(r.Stdout, namespace, r.ID, "stdout")
		stderr = convertOrGenerateFIFOPath(r.Stderr, namespace, r.ID, "stderr")

	default:
		// Should never reach here
		panic(fmt.Sprintf("unknown IO mode: %s", ioMode.String()))
	}

	return
}

// convertOrGenerateFIFOPath converts a binary:// URL to a FIFO path or generates a new one.
func convertOrGenerateFIFOPath(path, namespace, containerID, streamType string) string {
	if path != "" && micrunio.IsValidFIFOPath(path) {
		return path
	}
	return micrunio.GenerateStandardFIFOPath(namespace, containerID, streamType)
}

// GenerateStandardFIFOPath generates the standard containerd FIFO path for a container.
// This is a wrapper around micrunio.GenerateStandardFIFOPath for use in shim package.
// Deprecated: Use micrunio.GenerateStandardFIFOPath directly.
func GenerateStandardFIFOPath(namespace, containerID, stream string) string {
	return filepath.Join("/run/containerd/io.containerd.runtime.v2.task", namespace, containerID, stream)
}

// IsValidFIFOPath checks if a path is a valid FIFO path (not a URL or empty string).
// Deprecated: Use micrunio.IsValidFIFOPath directly.
func IsValidFIFOPath(path string) bool {
	return micrunio.IsValidFIFOPath(path)
}
