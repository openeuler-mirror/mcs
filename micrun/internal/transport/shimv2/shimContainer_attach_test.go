package shim

import (
	"testing"
	"time"
)

// TestApplyAttachEventDropsStaleReordering pins the guard against fan-in
// reordering: an older-published event delivered after a newer one must not
// flip the final attach state (stuck-true suspends auto-close forever;
// stuck-false kills a live session after the grace window).
func TestApplyAttachEventDropsStaleReordering(t *testing.T) {
	c := &shimContainer{id: "t-attach-guard", attachChanged: make(chan struct{}, 1)}

	t0 := time.Now()
	t1 := t0.Add(time.Millisecond)
	t2 := t1.Add(time.Millisecond)

	// Normal order applies.
	if !c.ApplyAttachEvent(true, t1) {
		t.Fatal("first attach event must apply")
	}
	if !c.IsAttached() {
		t.Fatal("attached expected true after ClientAttached")
	}
	// Newer detach applies.
	if !c.ApplyAttachEvent(false, t2) {
		t.Fatal("newer detach must apply")
	}
	if c.IsAttached() {
		t.Fatal("attached expected false after ClientDetached")
	}
	// Late-delivered older attach (published before the detach) must be
	// dropped, not resurrect the session.
	if c.ApplyAttachEvent(true, t1) {
		t.Fatal("stale attach event must be dropped")
	}
	if c.IsAttached() {
		t.Fatal("stale attach must not flip the final state")
	}
	// Zero timestamp (manual/test events) applies unconditionally.
	if !c.ApplyAttachEvent(true, time.Time{}) {
		t.Fatal("zero-timestamp event must apply")
	}
	if !c.IsAttached() {
		t.Fatal("attached expected true after zero-timestamp attach")
	}
}
