package netns

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	log "micrun/internal/support/logger"
)

type pidSignalFunc func(int, syscall.Signal) error

type pidGoneFunc func(int) bool

type pidExitWaitFunc func(int, time.Duration, time.Duration, pidGoneFunc) bool

type pidTerminator struct {
	signal      pidSignalFunc
	gone        pidGoneFunc
	waitForExit pidExitWaitFunc
	// verifyIdentity re-checks that pid still refers to the same process
	// before the SIGKILL escalation, guarding against PID recycling during
	// the grace-period poll. nil means skip the check.
	verifyIdentity func(pid int) error
	gracePeriod    time.Duration
	pollInterval   time.Duration
}

const (
	pidTerminationGracePeriod  = 2 * time.Second
	pidTerminationPollInterval = 100 * time.Millisecond
)

var defaultPIDTerminator = pidTerminator{
	signal:         syscall.Kill,
	gone:           processGone,
	waitForExit:    waitForPIDExit,
	verifyIdentity: verifyHolderProcess,
	gracePeriod:    pidTerminationGracePeriod,
	pollInterval:   pidTerminationPollInterval,
}

func terminateByPID(pid int) error {
	return terminateByPIDWith(pid, defaultPIDTerminator)
}

func terminateByPIDWith(pid int, terminator pidTerminator) error {
	if pid <= 0 {
		return nil
	}
	terminator = normalizePIDTerminator(terminator)

	// Best-effort termination; process might already be gone.
	if err := terminator.signal(pid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		log.Debugf("netns holder pid %d SIGTERM failed: %v", pid, err)
	}

	if terminator.waitForExit(pid, terminator.gracePeriod, terminator.pollInterval, terminator.gone) {
		return nil
	}

	// Re-validate PID identity before SIGKILL: the holder may have exited
	// and the kernel recycled the PID to an unrelated process during the
	// grace-period poll. If the PID now belongs to a different process,
	// skip the kill rather than risk terminating an innocent recycled PID.
	if !terminator.gone(pid) && terminator.verifyIdentity != nil {
		if verifyErr := terminator.verifyIdentity(pid); verifyErr != nil {
			log.Debugf("netns holder pid %d identity changed during grace period (%v), skipping SIGKILL", pid, verifyErr)
			return nil
		}
	}

	if err := terminator.signal(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		log.Debugf("netns holder pid %d SIGKILL failed: %v", pid, err)
		return err
	}
	return nil
}

func normalizePIDTerminator(terminator pidTerminator) pidTerminator {
	if terminator.signal == nil {
		terminator.signal = syscall.Kill
	}
	if terminator.gone == nil {
		terminator.gone = processGone
	}
	if terminator.waitForExit == nil {
		terminator.waitForExit = waitForPIDExit
	}
	if terminator.gracePeriod <= 0 {
		terminator.gracePeriod = pidTerminationGracePeriod
	}
	if terminator.pollInterval <= 0 {
		terminator.pollInterval = pidTerminationPollInterval
	}
	return terminator
}

func processGone(pid int) bool {
	// Use /proc check instead of Kill(pid, 0). This is lighter than signal
	// handling and does not require permission to signal the target process.
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return os.IsNotExist(err)
}

func waitForPIDExit(pid int, timeout, pollInterval time.Duration, gone pidGoneFunc) bool {
	if gone(pid) {
		return true
	}
	if timeout <= 0 {
		return gone(pid)
	}
	if pollInterval <= 0 {
		pollInterval = timeout
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if gone(pid) {
				return true
			}
		case <-timer.C:
			return gone(pid)
		}
	}
}
