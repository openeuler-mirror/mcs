// Package perf provides opt-in stage timing for the latency-critical paths
// (create/start/stop/attach). Stages emit one structured log line each
//
//	[PERF] <op>:<stage> <duration> id=<container>
//
// only when MICRUN_PERF=1 is set in the shim's environment (i.e. containerd's
// environment: `systemctl edit containerd` + Environment=MICRUN_PERF=1).
// The lines are the data source for tests/perf/measure.sh and the baselines
// in tests/perf/baseline.json; see docs/internals/testing.md §1.6.
package perf

import (
	"os"
	"sync"
	"time"

	log "micrun/internal/support/logger"
	"micrun/internal/support/timex"
)

var (
	enabledOnce sync.Once
	enabled     bool
)

// Enabled reports whether stage timing is on (MICRUN_PERF=1). Evaluated
// once; toggling requires restarting the shim, which is the natural unit
// anyway.
func Enabled() bool {
	enabledOnce.Do(func() {
		enabled = os.Getenv("MICRUN_PERF") == "1"
	})
	return enabled
}

// Timer tracks one operation's stage breakdown. The zero cost when disabled
// is one bool check per Stage call — no allocation happens before Start.
// Methods take a pointer receiver because Stage advances the segment mark;
// hold the Timer in a variable.
type Timer struct {
	op      string
	id      string
	clock   timex.Clock
	started time.Time
	last    time.Time
}

// Start begins timing operation op for container id. Returns a zero Timer
// (all methods no-op) when stage timing is disabled.
func Start(clock timex.Clock, op, id string) *Timer {
	if !Enabled() {
		return &Timer{}
	}
	now := timex.Now(clock)
	return &Timer{op: op, id: id, clock: clock, started: now, last: now}
}

// Stage records the duration of the segment since the previous mark (or the
// Start for the first call), labeled with the stage name.
func (t *Timer) Stage(stage string) {
	if t.op == "" {
		return
	}
	now := timex.Now(t.clock)
	log.Infof("[PERF] %s:%s %v id=%s", t.op, stage, now.Sub(t.last), t.id)
	t.last = now
}

// Total records the whole-operation duration.
func (t *Timer) Total() {
	if t.op == "" {
		return
	}
	now := timex.Now(t.clock)
	log.Infof("[PERF] %s:total %v id=%s", t.op, now.Sub(t.started), t.id)
}
