package netns

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	log "micrun/internal/support/logger"
)

const holderShutdownTimeout = 2 * time.Second

// Create ensures a network namespace holder exists for the given sandbox ID.
// It returns the holder's PID and the /proc/<pid>/ns/net path.
func Create(id string) (int, string, error) {
	if id == "" {
		return 0, "", fmt.Errorf("netns: empty holder id")
	}

	h, created, err := createHolderIfAbsent(id, func() (*holder, error) {
		cmd, err := startHolderCmd()
		if err != nil {
			return nil, err
		}

		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("netns: failed to start holder for %s: %w", id, err)
		}

		return newStartedHolder(cmd), nil
	})
	if err != nil {
		return 0, "", err
	}

	pid := h.pid
	if pid <= 0 {
		return 0, "", fmt.Errorf("netns: holder for %s has invalid pid", id)
	}
	if created {
		go watchHolder(id, h)
	}

	return pid, pathFor(pid), nil
}

func newStartedHolder(cmd *exec.Cmd) *holder {
	h := &holder{
		cmd:  cmd,
		pid:  cmd.Process.Pid,
		done: make(chan error, 1),
	}
	h.release = func() {
		releaseStartedHolder(cmd)
	}
	return h
}

func releaseStartedHolder(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		log.Debugf("netns unused holder SIGTERM failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()
	if waitHolderDone(done, holderShutdownTimeout) {
		return
	}
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		log.Debugf("netns unused holder SIGKILL failed: %v", err)
	}
	waitHolderDone(done, holderShutdownTimeout)
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

	replaceHolder(id, &holder{
		pid: pid,
		release: func() {
			if err := terminateByPID(pid); err != nil {
				log.Debugf("netns registered holder %s pid %d release failed: %v", id, pid, err)
			}
		},
	})
	return path, nil
}

// Cleanup tears down the holder identified by id. If the holder is not known,
// pidHint is used as a fallback to attempt termination.
func Cleanup(id string, pidHint int) error {
	if id == "" && pidHint <= 0 {
		return nil
	}

	h, ok := takeHolder(id)

	if !ok {
		if pidHint <= 0 {
			return nil
		}
		return terminateByPID(pidHint)
	}

	if h.cmd == nil {
		return terminateByPID(firstNonZero(h.pid, pidHint))
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
