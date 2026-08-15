package netns

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	log "micrun/internal/support/logger"
	"micrun/internal/support/panicsafe"
)

const holderShutdownTimeout = 2 * time.Second

// Create ensures a network namespace holder exists for the given sandbox ID.
// It returns the holder's PID and the /proc/<pid>/ns/net path.
func Create(id string) (int, string, error) {
	if id == "" {
		return 0, "", fmt.Errorf("netns: empty holder id")
	}

	h, _, err := createHolderIfAbsent(id, func() (*holder, error) {
		cmd, err := startHolderCmd()
		if err != nil {
			return nil, err
		}

		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("netns: failed to start holder for %s: %w", id, err)
		}

		ph := newStartedHolder(cmd)
		// Start the watcher inside the create callback so it is ready
		// before the holder becomes visible to concurrent Cleanup. Without
		// this, Cleanup could SIGTERM the holder between map insertion and
		// watchHolder startup, leaving `done` unfilled and forcing Cleanup
		// to wait the full shutdown timeout (~4s) before reaping.
		panicsafe.Go("netns holder watcher", func() { watchHolder(id, ph) })
		return ph, nil
	})
	if err != nil {
		return 0, "", err
	}

	pid := h.pid
	if pid <= 0 {
		return 0, "", fmt.Errorf("netns: holder for %s has invalid pid", id)
	}

	// Validate liveness before returning success. A holder that exits
	// immediately (bad sleep binary, busybox without "infinity") can race
	// watchHolder → deleteHolderIfCurrent so the map entry is already gone
	// while Create would still hand back a dead pid/path.
	//
	// Do NOT call Cleanup(id, pid) here: if watchHolder already reaped the
	// process and removed it from the map, Cleanup falls through to
	// terminateByPID(pid) which may kill a recycled PID. Instead, try to
	// take the holder from the map and release it through its done channel;
	// if it's already gone, watchHolder has reaped it and nothing more is
	// needed.
	path := pathFor(pid)
	if !holderAlive(pid) {
		if h, ok := takeHolderIfCurrent(id, h); ok {
			releaseHolder(h)
		}
		return 0, "", fmt.Errorf("netns: holder for %s pid %d exited immediately after start", id, pid)
	}
	if _, err := os.Stat(path); err != nil {
		if h, ok := takeHolderIfCurrent(id, h); ok {
			releaseHolder(h)
		}
		return 0, "", fmt.Errorf("netns: holder for %s pid %d netns path invalid: %w", id, pid, err)
	}

	return pid, path, nil
}

func newStartedHolder(cmd *exec.Cmd) *holder {
	h := &holder{
		cmd:  cmd,
		pid:  cmd.Process.Pid,
		done: make(chan error, 1),
	}
	h.release = func() {
		releaseStartedHolder(h)
	}
	return h
}

// releaseStartedHolder gracefully terminates a started holder. It reuses the
// holder's done channel (filled by watchHolder) instead of spawning a second
// cmd.Wait goroutine: os/exec forbids concurrent Wait calls on the same Cmd.
func releaseStartedHolder(h *holder) {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return
	}
	if err := h.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		log.Debugf("netns unused holder SIGTERM failed: %v", err)
	}

	if waitHolderDone(h.done, holderShutdownTimeout) {
		return
	}
	if err := h.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		log.Debugf("netns unused holder SIGKILL failed: %v", err)
	}
	waitHolderDone(h.done, holderShutdownTimeout)
}

func watchHolder(id string, h *holder) {
	err := h.cmd.Wait()
	notifyHolderDone(h.done, err)

	deleteHolderIfCurrent(id, h)

	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		log.Debugf("netns holder %s exited with error: %v", id, err)
	} else {
		log.Debugf("netns holder %s exited", id)
	}
}

// RegisterExisting registers an already running holder process (e.g. after shim restore).
func RegisterExisting(id string, pid int) (string, error) {
	if id == "" {
		return "", fmt.Errorf("netns: empty holder id")
	}
	if pid <= 0 {
		return "", fmt.Errorf("netns: invalid pid for holder %s", id)
	}
	path := pathFor(pid)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("netns: holder %s pid %d not valid: %w", id, pid, err)
	}
	if err := verifyHolderProcess(pid); err != nil {
		return "", err
	}

	replaceHolder(id, &holder{
		pid: pid,
		release: func() {
			terminateRegisteredHolder(id, pid)
		},
	})
	return path, nil
}

// terminateRegisteredHolder verifies the process identity before terminating a
// RegisterExisting holder. Registered holders have no watchHolder, so a stale
// map entry may point at a PID that has been recycled to an unrelated process
// after the original holder died. verifyHolderProcess reads /proc/<pid>/cmdline
// and confirms the process still matches a holder command shape; if verification
// fails, termination is skipped to avoid killing an innocent process.
func terminateRegisteredHolder(id string, pid int) {
	if pid <= 0 {
		return
	}
	if err := verifyHolderProcess(pid); err != nil {
		log.Debugf("netns registered holder %s pid %d is no longer a holder, skipping termination: %v", id, pid, err)
		return
	}
	if err := terminateByPID(pid); err != nil {
		log.Debugf("netns registered holder %s pid %d release failed: %v", id, pid, err)
	}
}

// Cleanup tears down the holder identified by id. If the holder is not known,
// pidHint is used as a fallback to attempt termination.
func Cleanup(id string, pidHint int) error {
	if id == "" && pidHint <= 0 {
		return nil
	}

	h, ok := takeHolder(id)

	if !ok {
		// Holder is not in the map: either watchHolder already reaped it
		// (started holder died → cmd.Wait returned → deleteHolderIfCurrent
		// removed it), or a prior Cleanup already removed it. In both cases
		// the process is gone and terminateByPID(pidHint) is unnecessary
		// and dangerous — the PID may have been recycled to an unrelated
		// process. (Same rationale as the R11 fix in Create.)
		return nil
	}

	if h.cmd == nil {
		// RegisterExisting holder: no watchHolder guards it, so a stale entry
		// may point at a PID recycled to an unrelated process. Re-validate the
		// process identity before terminating to avoid killing an innocent process.
		terminateRegisteredHolder(id, firstNonZero(h.pid, pidHint))
		return nil
	}

	// Attempt graceful termination first. Missing or already-dead processes are fine.
	if err := h.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		log.Debugf("netns holder %s SIGTERM failed: %v", id, err)
	}

	if waitHolderDone(h.done, holderShutdownTimeout) {
		return nil
	}

	if err := h.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		log.Debugf("netns holder %s SIGKILL failed: %v", id, err)
	}
	waitHolderDone(h.done, holderShutdownTimeout)
	return nil
}

// PID returns the holder PID recorded for the given id.
func PID(id string) (int, bool) {
	return holderPID(id)
}

func pathFor(pid int) string {
	return fmt.Sprintf("/proc/%d/ns/net", pid)
}

func firstNonZero(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

func waitHolderDone(done <-chan error, timeout time.Duration) bool {
	if done == nil {
		return false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func notifyHolderDone(done chan<- error, err error) {
	if done == nil {
		return
	}
	done <- err
	close(done)
}
