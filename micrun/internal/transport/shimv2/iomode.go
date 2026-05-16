// Package shim provides IO mode classification for container runtime.
// It centralizes the logic for handling different IO scenarios (TTY/non-TTY, foreground/background).
package shim

import (
	"strings"

	micrunio "micrun/internal/adapters/io"
	"micrun/internal/ports"
	log "micrun/internal/support/logger"
)

// IOMode describes the IO session mode for a container.
type IOMode struct {
	// IsTTY indicates whether this is TTY mode.
	// Affects: terminal configuration, data processing method.
	IsTTY bool

	// IsForeground indicates whether the create request carries at least one
	// valid foreground containerd FIFO path.
	IsForeground bool

	// HasStdin indicates whether stdin was requested, including placeholder
	// paths such as binary:// that must be converted to a generated FIFO.
	HasStdin bool

	// SupportsDetach indicates whether terminal input can detach the client.
	SupportsDetach bool

	// SupportsAttach indicates whether reattach is supported.
	SupportsAttach bool
}

// IOModeKind is the stable classification used for IO control flow.
type IOModeKind string

// IO mode constants.
const (
	IOModeTTYForeground    IOModeKind = "TTY-foreground"     // -it
	IOModeTTYBackground    IOModeKind = "TTY-background"     // -it -d
	IOModeTTYReadOnly      IOModeKind = "TTY-read-only"      // -t (without -i)
	IOModeNonTTYForeground IOModeKind = "non-TTY-foreground" // (without -t, without -d)
	IOModeNonTTYBackground IOModeKind = "non-TTY-background" // -d
)

func (k IOModeKind) String() string {
	return string(k)
}

// String returns a human-readable representation of the IO mode.
func (m IOMode) String() string {
	return m.Kind().String()
}

func (m IOMode) Kind() IOModeKind {
	if m.IsTTY {
		if !m.HasStdin {
			return IOModeTTYReadOnly
		}
		if m.IsForeground {
			return IOModeTTYForeground
		}
		return IOModeTTYBackground
	}
	if m.IsForeground {
		return IOModeNonTTYForeground
	}
	return IOModeNonTTYBackground
}

// DetermineIOMode determines the IO mode from an internal task create request.
func DetermineIOMode(r ports.TaskCreateRequest) IOMode {
	mode := IOMode{
		IsTTY: r.Terminal,
	}

	// Determine foreground: a valid containerd FIFO means a client is attached.
	mode.IsForeground = micrunio.IsValidFIFOPath(r.Stdin) ||
		micrunio.IsValidFIFOPath(r.Stdout) ||
		micrunio.IsValidFIFOPath(r.Stderr)

	// Determine whether stdin was requested. Invalid placeholders such as
	// binary:// still count as a stdin request, but not as a foreground FIFO.
	mode.HasStdin = hasRequestedStdin(r.Stdin)

	// TTY mode supports detach (nerdctl Ctrl+P Ctrl+Q, or "back" command)
	mode.SupportsDetach = r.Terminal && mode.HasStdin

	// Determine supports attach:
	// - TTY mode supports multiple attach (detach with "back" command, then reattach)
	// - Background mode supports attach
	mode.SupportsAttach = !mode.IsForeground || mode.IsTTY

	return mode
}

func hasRequestedStdin(path string) bool {
	return strings.TrimSpace(path) != ""
}

// GenerateFIFOPaths generates FIFO paths for all IO modes.
// This function centralizes the FIFO path generation logic using switch-case
// to handle different scenarios explicitly.
func GenerateFIFOPaths(r ports.TaskCreateRequest, namespace string) (stdin, stdout, stderr string) {
	ioMode := DetermineIOMode(r)
	paths := newFIFOPathGenerator(namespace, r.ID)

	switch ioMode.Kind() {
	case IOModeTTYForeground:
		// TTY foreground (-it): use nerdctl-provided FIFO paths, but still
		// normalize placeholder stream values from mixed clients.
		stdin = paths.providedOrStandard(r.Stdin, "stdin")
		stdout = paths.providedOrStandard(r.Stdout, "stdout")
		stderr = paths.providedOrStandard(r.Stderr, "stderr")

	case IOModeTTYBackground:
		// TTY background (-it -d): nerdctl provides binary://, convert to FIFO paths
		stdin = paths.standard("stdin")
		stdout = paths.providedOrStandard(r.Stdout, "stdout")
		stderr = paths.providedOrStandard(r.Stderr, "stderr")

	case IOModeTTYReadOnly:
		// TTY read-only (-t without -i): generate stdin FIFO for ctr compatibility
		// Note: -i option is only available in nerdctl, not ctr
		stdin = paths.standard("stdin")
		stdout = paths.providedOrStandard(r.Stdout, "stdout")
		stderr = "" // TTY mode: stderr merged into stdout

	case IOModeNonTTYForeground:
		// non-TTY foreground: use nerdctl-provided FIFO paths or generate for ctr
		stdin = paths.providedOrStandard(r.Stdin, "stdin")
		stdout = r.Stdout
		stderr = r.Stderr

	case IOModeNonTTYBackground:
		// non-TTY background (-d): generate FIFO paths to support attach
		stdin = paths.standard("stdin")
		stdout = paths.providedOrStandard(r.Stdout, "stdout")
		stderr = paths.providedOrStandard(r.Stderr, "stderr")

	default:
		log.Warnf("unknown IO mode: %s, falling back to empty paths", ioMode.String())
	}

	return
}

type fifoPathGenerator struct {
	namespace   string
	containerID string
}

func newFIFOPathGenerator(namespace, containerID string) fifoPathGenerator {
	return fifoPathGenerator{namespace: namespace, containerID: containerID}
}

func (g fifoPathGenerator) standard(streamType string) string {
	return micrunio.GenerateStandardFIFOPath(g.namespace, g.containerID, streamType)
}

func (g fifoPathGenerator) providedOrStandard(path, streamType string) string {
	if micrunio.IsValidFIFOPath(path) {
		return path
	}
	return g.standard(streamType)
}
