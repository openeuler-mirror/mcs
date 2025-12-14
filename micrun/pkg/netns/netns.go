package netns

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	log "micrun/logger"

	"golang.org/x/sys/unix"
)

type holder struct {
	cmd  *exec.Cmd
	pid  int
	done chan error
}

var (
	holdersMu sync.Mutex
	holders   = make(map[string]*holder)
)

// Create ensures a network namespace holder exists for the given sandbox ID.
// It returns the holder's PID and the /proc/<pid>/ns/net path.
func Create(id string) (int, string, error) {
	if id == "" {
		return 0, "", fmt.Errorf("netns: empty holder id")
	}

	holdersMu.Lock()
	if h, ok := holders[id]; ok && h != nil {
		pid := h.pid
		holdersMu.Unlock()
		if pid <= 0 {
			return 0, "", fmt.Errorf("netns: holder for %s has invalid pid", id)
		}
		return pid, pathFor(pid), nil
	}
	holdersMu.Unlock()

	cmd, err := startHolderCmd()
	if err != nil {
		return 0, "", err
	}

	if err := cmd.Start(); err != nil {
		return 0, "", fmt.Errorf("netns: failed to start holder for %s: %w", id, err)
	}

	pid := cmd.Process.Pid
	h := &holder{
		cmd:  cmd,
		pid:  pid,
		done: make(chan error, 1),
	}

	holdersMu.Lock()
	holders[id] = h
	holdersMu.Unlock()

	go func() {
		err := cmd.Wait()
		// fill done channel when cmd is done (whatever error)
		h.done <- err
		close(h.done)

		holdersMu.Lock()
		// Ensure we only clear if the current holder matches this instance.
		if cur, ok := holders[id]; ok && cur == h {
			delete(holders, id)
		}
		holdersMu.Unlock()

		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			log.Debugf("netns holder %s exited with error: %v", id, err)
		} else {
			log.Debugf("netns holder %s exited", id)
		}
	}()

	return pid, pathFor(pid), nil
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

	holdersMu.Lock()
	holders[id] = &holder{pid: pid}
	holdersMu.Unlock()
	return path, nil
}

// Cleanup tears down the holder identified by id. If the holder is not known,
// pidHint is used as a fallback to attempt termination.
func Cleanup(id string, pidHint int) error {
	if id == "" && pidHint <= 0 {
		return nil
	}

	holdersMu.Lock()
	h, ok := holders[id]
	if ok {
		delete(holders, id)
	}
	holdersMu.Unlock()

	if !ok {
		if pidHint <= 0 {
			return nil
		}
		return terminateByPID(pidHint)
	}

	if h.cmd == nil {
		return terminateByPID(firstNonZero(h.pid, pidHint))
	}

	// Attempt graceful termination first.
	// be slient for "Process Dead || Process Not Found" => which is fine
	if err := h.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		log.Debugf("netns holder %s SIGTERM failed: %v", id, err)
	}

	select {
	case <-h.done:
		return nil
	case <-time.After(2 * time.Second):
		if err := h.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
			log.Debugf("netns holder %s SIGKILL failed: %v", id, err)
		}
		// ensure done channel received value when process is down, and then drop it.
		<-h.done
		return nil
	}
}

// PID returns the holder PID recorded for the given id.
func PID(id string) (int, bool) {
	holdersMu.Lock()
	defer holdersMu.Unlock()
	h, ok := holders[id]
	if !ok || h == nil || h.pid <= 0 {
		return 0, false
	}
	return h.pid, true
}

// TODO: need a proper holdercmd, current startHolderCmd is a workaround for debug
// we need:
// without running pause image, micrun serve as a placeholder for the netns holder process
// 1. not `sleep`, syscall_pause directly
// 2. handle SIGTERM, SIGKILL, ..., make sure hodler proc in the control of a normal container management flow
// 3. handle
func startHolderCmd() (*exec.Cmd, error) {
	var candidates = [][]string{
		{"sleep", "infinity"},
		{"tail", "-f", "/dev/null"},
	}

	var lastErr error
	for _, candidate := range candidates {
		bin, err := exec.LookPath(candidate[0])
		if err != nil {
			lastErr = err
			continue
		}

		cmd := exec.Command(bin, candidate[1:]...)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags: unix.CLONE_NEWNET,
			Setsid:     true,
		}
		return cmd, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no suitable holder command found")
	}
	return nil, fmt.Errorf("netns: %w", lastErr)
}

func pathFor(pid int) string {
	return fmt.Sprintf("/proc/%d/ns/net", pid)
}

func terminateByPID(pid int) error {
	if pid <= 0 {
		return nil
	}

	// Best-effort termination; process might already be gone.
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		log.Debugf("netns holder pid %d SIGTERM failed: %v", pid, err)
	}

	const retryWindow = 500 * time.Millisecond
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		time.Sleep(retryWindow)
	}

	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		log.Debugf("netns holder pid %d SIGKILL failed: %v", pid, err)
		return err
	}
	return nil
}

func firstNonZero(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}
