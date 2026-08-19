package libmica

import (
	"context"
	"fmt"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	defs "micrun/internal/support/definitions"
)

// serviceStartTimeout bounds a single `systemctl start micad` / `service
// micad start` invocation during shim init.
const serviceStartTimeout = 30 * time.Second

// Constants
const MICAD_PIDFILE = defs.MicadPidFile
const (
	DaemonRunning = "running"
	DaemonStopped = "stopped"
)

// Types
type MicaDaemonState struct {
	Pid       int
	State     string
	Listening bool
}

type micadDetector func() (int, error)
type micadStarter func() error
type micadListener func() bool

type serviceStartCommand struct {
	name string
	args []string
}

type serviceCommandRunner interface {
	LookPath(file string) (string, error)
	Run(name string, args ...string) error
}

type osServiceCommandRunner struct{}

var micadServiceStartCommands = []serviceStartCommand{
	{name: "systemctl", args: []string{"start", "micad"}},
	{name: "service", args: []string{"micad", "start"}},
}

func (osServiceCommandRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (osServiceCommandRunner) Run(name string, args ...string) error {
	// Bounded: `systemctl start micad` runs during shim init / recovery. A
	// hung service manager (unit stuck in activating, dbus wedged) would
	// otherwise block the whole shim startup forever with no RPC serving.
	ctx, cancel := context.WithTimeout(context.Background(), serviceStartTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}

// micadProcessName is the expected /proc/<pid>/comm prefix for the micad
// daemon. The kernel truncates comm to 15 chars; "micad" (5) fits comfortably.
const micadProcessName = "micad"

// procCommReader reads /proc/<pid>/comm for a pid. It is a package-level
// variable so tests can substitute a fake without touching the filesystem.
var procCommReader = func(pid int) (string, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// verifyMicadProcess checks that the live process at pid is actually micad.
// kill(pid, 0) alone cannot tell a live micad apart from a PID that was
// recycled to an unrelated process after micad crashed: the stale pidfile
// would make every caller believe micad is alive, setupMicad would skip the
// start path, and the shim would talk to a dead daemon forever. Read
// /proc/<pid>/comm so a recycled PID is rejected and setupMicad re-starts
// micad for real. This mirrors isLiveShimInstance (sandbox_loader) and
// verifyHolderProcess (netns/holder_command).
func verifyMicadProcess(pid int) error {
	comm, err := procCommReader(pid)
	if err != nil {
		// The process exited between kill(0) and the read, /proc is unavailable,
		// or the pid is a zombie (no comm): treat as "not micad" so the caller
		// re-checks rather than trusting a stale pidfile.
		return fmt.Errorf("micad pid %d identity unreadable: %w", pid, err)
	}
	if !strings.HasPrefix(comm, micadProcessName) {
		return fmt.Errorf("pid %d is not micad (comm=%q), pidfile is stale", pid, comm)
	}
	return nil
}

// micadDetect checks if micad is already running by verifying the PID file
// and process status. Returns (pid, instanceNum, true) if running, (0, 0, false) otherwise.
func micadDetect() (int, error) {
	// believe micad MICAD_PIDFILE can avoid race
	if _, err := os.Stat(MICAD_PIDFILE); err != nil {
		return 0, err
	}

	pidFile, err := os.ReadFile(MICAD_PIDFILE)
	if err != nil {
		return 0, err
	}

	pidFromFile, err := strconv.Atoi(strings.TrimSpace(string(pidFile)))
	if err != nil {
		return 0, err
	}

	// Check if process is running by sending signal 0
	sigProcExistence := syscall.Signal(0)
	if err := syscall.Kill(pidFromFile, sigProcExistence); err != nil {
		return pidFromFile, err
	}

	// kill(pid, 0) confirms only that SOME process owns the pid; it cannot
	// detect PID reuse after micad crashed. Verify the process identity via
	// /proc so a recycled PID (e.g. a worker process that inherited the
	// number) is not mistaken for a live micad — otherwise setupMicad skips
	// the start path and the shim stays wedged against a dead daemon.
	if err := verifyMicadProcess(pidFromFile); err != nil {
		return pidFromFile, err
	}

	return pidFromFile, nil
}

// MicadDetect is a non-blocking version of micad detection.
// It returns the micad PID if micad is running, or 0 if not.
// Unlike DaemonState(), it does NOT attempt to start micad.
func MicadDetect() (int, error) {
	return micadDetect()
}

// DaemonState ensures micad is running and returns the current daemon state.
func DaemonState() (*MicaDaemonState, error) {
	return daemonState(micadDetect, setupMicad, func() bool {
		return validSocketPath(defs.MicaCreateSocketPath)
	})
}

func daemonState(detect micadDetector, start micadStarter, listening micadListener) (*MicaDaemonState, error) {
	state := MicaDaemonState{}

	pid, err := detect()
	if err != nil {
		if setupErr := start(); setupErr != nil {
			return nil, fmt.Errorf("failed to setup micad daemon: %w", setupErr)
		}
		pid, err = detect()
		if err != nil {
			state.Listening = false
			state.State = DaemonStopped
			state.Pid = 0
			return &state, er.MicadNotRunning
		}
	}

	state.Pid = pid
	state.State = DaemonRunning
	state.Listening = listening()

	return &state, nil
}

func (m *MicaDaemonState) Active() bool {
	if m == nil {
		return false
	}
	return m.State == DaemonRunning
}

// setupMicad attempts to start micad if it's not already running.
func setupMicad() error {
	if pid, err := micadDetect(); pid != 0 && err == nil {
		log.Debugf("got micad pid= %d, err : %v", pid, err)
		return nil
	}

	return startMicadService(osServiceCommandRunner{}, micadServiceStartCommands)
}

func startMicadService(runner serviceCommandRunner, commands []serviceStartCommand) error {
	var failures []string
	for _, command := range commands {
		if _, err := runner.LookPath(command.name); err != nil {
			continue
		}
		if err := runner.Run(command.name, command.args...); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", command.name, err))
			log.Warnf("failed to start micad with %s: %v", command.name, err)
			continue
		}
		log.Infof("micad started via %s", command.name)
		return nil
	}

	if len(failures) > 0 {
		return fmt.Errorf("mica daemon could not be started: %s", strings.Join(failures, "; "))
	}
	return fmt.Errorf("mica daemon service not found or could not be started")
}
