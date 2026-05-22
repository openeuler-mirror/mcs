package netns

import (
	"fmt"
	"os/exec"
	"sync"

	"micrun/internal/support/lockutil"
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
		existing *holder
		created  bool
	)

	lockutil.WithLock(&holdersMu, func() {
		existing = holders[id]
	})

	if existing != nil {
		return existing, false, nil
	}

	proposed, createErr := create()
	if createErr != nil {
		return nil, false, createErr
	}
	if proposed == nil {
		return nil, false, fmt.Errorf("netns: holder factory returned nil")
	}

	lockutil.WithLock(&holdersMu, func() {
		existing = holders[id]
		if existing != nil {
			return
		}
		holders[id] = proposed
		created = true
	})
	if existing != nil {
		releaseHolder(proposed)
		return existing, false, nil
	}
	return proposed, created, nil
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

func holderPID(id string) (int, bool) {
	var (
		pid int
		ok  bool
	)
	lockutil.WithLock(&holdersMu, func() {
		var h *holder
		h, ok = holders[id]
		if !ok || h == nil || h.pid <= 0 {
			return
		}
		pid = h.pid
		ok = true
	})
	return pid, ok
}
