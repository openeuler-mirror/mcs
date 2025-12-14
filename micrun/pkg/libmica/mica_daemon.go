package libmica

import (
	"fmt"
	er "micrun/errors"
	log "micrun/logger"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	defs "micrun/definitions"
)

// Constants
const MICAD_PIDFILE = defs.DaemonRoot + "/micad.pid"
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

// TODO: when to check?
// return nil => failed to setup, no need to run micran
// return state => daemon state
func DaemonState() (*MicaDaemonState, error) {
	log.Info("DaemonState() called")
	state := MicaDaemonState{}

	pid, err := micadDetect()
	if err != nil {
		if setupErr := setupMicad(); setupErr != nil {
			return nil, fmt.Errorf("failed to setup micad daemon: %w", setupErr)
		}
		pid, err = micadDetect()
		if err != nil {
			state.Listening = false
			state.State = DaemonStopped
			state.Pid = 0
			return &state, er.MicadNotRunning
		}
	}

	state.Pid = pid
	state.State = DaemonRunning
	state.Listening = validSocketPath(defs.MicaCreatSocketPath)

	return &state, nil
}

func (m *MicaDaemonState) Active() bool {
	if m == nil {
		return false
	}
	return m.State == DaemonRunning
}

// setupMicad attempts to start micad if it's not already running
// TODO: systemctl binary call is not good
func setupMicad() error {
	if pid, err := micadDetect(); pid != 0 && err == nil {
		log.Debugf("got micad pid= %d, err : %v", pid, err)
		return nil
	}

	if _, err := exec.LookPath("systemctl"); err == nil {
		cmd := exec.Command("systemctl", "start", "micad")
		if err := cmd.Run(); err != nil {
			log.Warnf("failed to start micad with systemctl: %v", err)
		} else {
			log.Info("micad started via systemctl")
			return nil
		}
	}

	if _, err := exec.LookPath("service"); err == nil {
		cmd := exec.Command("service", "micad", "start")
		if err := cmd.Run(); err != nil {
			log.Infof("failed to start micad with service: %v", err)
		} else {
			log.Info("micad started via service")
			return nil
		}
	}

	return fmt.Errorf("mica daemon service not found or could not be started")
}
