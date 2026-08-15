package netns

import (
	"fmt"
	"os/exec"
	"sync"

	"golang.org/x/sys/unix"
	"micrun/internal/support/lockutil"
	"micrun/internal/support/logger"
)

type holder struct {
	cmd     *exec.Cmd
	pid     int
	done    chan error
	release func()
}

var (
	holdersMu sync.Mutex
	holders   = make(map[string]*holder)
)

func putHolder(id string, h *holder) {
	lockutil.WithLock(&holdersMu, func() {
		holders[id] = h
	})
}

func replaceHolder(id string, h *holder) (*holder, bool) {
	var (
		previous *holder
		replaced bool
	)
	lockutil.WithLock(&holdersMu, func() {
		previous = holders[id]
		if sameHolderPID(previous, h) {
			return
		}
		holders[id] = h
		replaced = previous != nil
	})
	if replaced {
		releaseHolder(previous)
	}
	return previous, replaced
}

func createHolderIfAbsent(id string, create func() (*holder, error)) (*holder, bool, error) {
	var (
		existing  *holder
		proposed  *holder
		created   bool
		reused    bool
		createErr error
	)

	lockutil.WithLock(&holdersMu, func() {
		existing = holders[id]
		if existing != nil {
			reusable := holderAlive(existing.pid)
			if reusable && existing.cmd == nil {
				// A registered (recovered) holder has no watcher, so a dead
				// entry can linger until its PID is recycled by an unrelated
				// live process. Verify the identity before reusing it; on a
				// mismatch drop the entry so the caller spawns a fresh holder
				// instead of handing out a foreign process's netns.
				if err := verifyHolderProcess(existing.pid); err != nil {
					log.Warnf("netns: refusing to reuse registered holder for %s pid %d: %v", id, existing.pid, err)
					reusable = false
				}
			}
			if reusable {
				reused = true
				return
			}
			// The holder process died (OOM killer, admin kill) or failed the
			// identity check: drop the stale entry so the caller can spawn a
			// fresh holder. Do not call release on a dead entry —
			// terminateByPID uses a bare PID, and after reaping the PID may
			// already be reused by the new holder (or an unrelated process).
			log.Infof("netns holder %s pid %d is not reusable, replacing", id, existing.pid)
			delete(holders, id)
			existing = nil
		}
		// Spawn the process inside the lock. Spawning outside would leave a
		// window between the map check and the insertion in which a
		// concurrent Cleanup sees no holder, tears down "nothing", and the
		// holder spawned in that window gets registered afterwards — an
		// orphan process and a permanently leaked netns.
		proposed, createErr = create()
		if createErr != nil || proposed == nil {
			return
		}
		holders[id] = proposed
		created = true
	})
	if reused {
		return existing, false, nil
	}
	if createErr != nil {
		return nil, false, createErr
	}
	if proposed == nil {
		return nil, false, fmt.Errorf("netns: holder factory returned nil")
	}
	return proposed, created, nil
}

// holderAlive reports whether the process is still present (signal 0).
func holderAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := unix.Kill(pid, 0)
	return err == nil || err == unix.EPERM
}

func releaseHolder(h *holder) {
	if h == nil || h.release == nil {
		return
	}
	h.release()
}

func sameHolderPID(a, b *holder) bool {
	return a != nil && b != nil && a.pid > 0 && a.pid == b.pid
}

func takeHolder(id string) (*holder, bool) {
	var (
		existing *holder
		ok       bool
	)
	lockutil.WithLock(&holdersMu, func() {
		var h *holder
		h, ok = holders[id]
		if ok {
			delete(holders, id)
		}
		existing = h
	})
	return existing, ok
}

func deleteHolderIfCurrent(id string, h *holder) {
	lockutil.WithLock(&holdersMu, func() {
		if cur, ok := holders[id]; ok && cur == h {
			delete(holders, id)
		}
	})
}

// takeHolderIfCurrent removes the holder for id ONLY if it is still the same
// pointer as expected. Returns the holder and true if removed; nil and false
// otherwise. This prevents a concurrent Create/replaceHolder from swapping
// in a new holder that we would then incorrectly release.
func takeHolderIfCurrent(id string, expected *holder) (*holder, bool) {
	var existing *holder
	var ok bool
	lockutil.WithLock(&holdersMu, func() {
		if cur, found := holders[id]; found && cur == expected {
			delete(holders, id)
			existing = cur
			ok = true
		}
	})
	return existing, ok
}

func holderPID(id string) (int, bool) {
	var (
		pid int
		ok  bool
	)
	lockutil.WithLock(&holdersMu, func() {
		var h *holder
		h, ok = holders[id]
		if !ok || h == nil || h.pid <= 0 {
			ok = false
			return
		}
		pid = h.pid
		ok = true
	})
	return pid, ok
}
