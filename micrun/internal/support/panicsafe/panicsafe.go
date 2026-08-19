// Package panicsafe turns panics in background goroutines into error logs
// instead of process crashes. The shim is the sole manager of live Xen/micad
// domains: a panic in any long-lived goroutine (exit watcher, event
// forwarder, IO copier, ...) would kill the whole process and orphan every
// domain it manages, which is strictly worse than losing that one goroutine's
// work. RPC handlers are NOT wrapped — ttrpc already isolates those.
package panicsafe

import (
	"runtime/debug"

	log "micrun/internal/support/logger"
)

// Go runs fn on a new goroutine with a panic guard.
func Go(name string, fn func()) {
	go func() {
		defer Recover(name)
		fn()
	}()
}

// Recover is a deferrable panic guard for goroutines whose function body is
// managed elsewhere (e.g. workers that must run their own cleanup defers):
//
//	defer panicsafe.Recover("io copier stdout")
func Recover(name string) {
	if r := recover(); r != nil {
		log.Errorf("panic in %s (recovered, goroutine lost): %v\n%s", name, r, debug.Stack())
	}
}
