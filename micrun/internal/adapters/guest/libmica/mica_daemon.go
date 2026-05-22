package libmica

import (
	"fmt"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	defs "micrun/internal/support/definitions"
)

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
	return exec.Command(name, args...).Run()
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
	log.Info("DaemonState() called")
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
