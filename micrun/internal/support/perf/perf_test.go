package perf

import (
	"testing"
)

// TestTimerDisabledByDefault pins the opt-in contract: without MICRUN_PERF
// the Timer is inert (no panics, no output), so production paths pay one
// bool check.
func TestTimerDisabledByDefault(t *testing.T) {
	tm := Start(nil, "create", "c1")
	tm.Stage("guest_create")
	tm.Total()
	if tm.op != "" {
		t.Fatalf("expected inert timer, got op=%q", tm.op)
	}
}
