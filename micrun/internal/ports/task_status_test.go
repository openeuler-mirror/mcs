package ports

import (
	"testing"
	"time"

	"github.com/containerd/containerd/api/types/task"
)

func TestCanTransitTaskStatus(t *testing.T) {
	cases := []struct {
		from, to task.Status
		want     bool
	}{
		// Terminal is absorbing.
		{task.Status_STOPPED, task.Status_RUNNING, false},
		{task.Status_STOPPED, task.Status_CREATED, false},
		{task.Status_STOPPED, task.Status_PAUSED, false},
		{task.Status_STOPPED, task.Status_STOPPED, true}, // idempotent rewrite
		// Forward-only: a started task never returns to CREATED.
		{task.Status_RUNNING, task.Status_CREATED, false},
		{task.Status_PAUSED, task.Status_CREATED, false},
		{task.Status_PAUSING, task.Status_CREATED, false},
		// Pause cycle.
		{task.Status_RUNNING, task.Status_PAUSING, true},
		{task.Status_PAUSING, task.Status_PAUSED, true},
		{task.Status_PAUSING, task.Status_RUNNING, true}, // failed pause rollback
		{task.Status_PAUSED, task.Status_RUNNING, true},
		{task.Status_RUNNING, task.Status_PAUSED, true}, // Suspended convergence
		// Start and stop.
		{task.Status_CREATED, task.Status_RUNNING, true},
		{task.Status_CREATED, task.Status_STOPPED, true},
		{task.Status_RUNNING, task.Status_STOPPED, true},
		{task.Status_PAUSING, task.Status_STOPPED, true},
		{task.Status_PAUSED, task.Status_STOPPED, true},
		// Recovery seeds anything.
		{task.Status_UNKNOWN, task.Status_CREATED, true},
		{task.Status_UNKNOWN, task.Status_RUNNING, true},
		{task.Status_UNKNOWN, task.Status_PAUSED, true},
		{task.Status_UNKNOWN, task.Status_STOPPED, true},
		// Nothing legal moves to UNKNOWN.
		{task.Status_RUNNING, task.Status_UNKNOWN, false},
		{task.Status_CREATED, task.Status_UNKNOWN, false},
	}
	for _, c := range cases {
		if got := CanTransitTaskStatus(c.from, c.to); got != c.want {
			t.Errorf("CanTransitTaskStatus(%s, %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

type finalizeProbe struct {
	Task       // panics on unimplemented methods — finalize must only touch these:
	status     task.Status
	exitStatus uint32
	exitTime   time.Time
	ioExited   bool
}

func (p *finalizeProbe) Status() task.Status { return p.status }
func (p *finalizeProbe) SetStatus(s task.Status) {
	p.status = s
}
func (p *finalizeProbe) ExitTime() time.Time { return p.exitTime }
func (p *finalizeProbe) SetExitInfo(status uint32, at time.Time) {
	p.exitStatus, p.exitTime = status, at
}
func (p *finalizeProbe) IOExit() { p.ioExited = true }

func TestFinalizeTaskStoppedFabricatesWhenNoExitInfo(t *testing.T) {
	now := time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC)
	p := &finalizeProbe{status: task.Status_RUNNING}

	FinalizeTaskStopped(p, 130, now)

	if p.status != task.Status_STOPPED {
		t.Fatalf("status = %s, want STOPPED", p.status)
	}
	if p.exitStatus != 130 || !p.exitTime.Equal(now) {
		t.Fatalf("exit info = (%d, %s), want (130, %s)", p.exitStatus, p.exitTime, now)
	}
	if !p.ioExited {
		t.Fatal("exit channel was not closed")
	}
}

func TestFinalizeTaskStoppedPreservesKillPreWrite(t *testing.T) {
	pre := time.Date(2026, 8, 13, 19, 0, 0, 0, time.UTC)
	p := &finalizeProbe{status: task.Status_RUNNING, exitStatus: 137, exitTime: pre}

	FinalizeTaskStopped(p, 130, time.Now())

	if p.exitStatus != 137 || !p.exitTime.Equal(pre) {
		t.Fatalf("pre-written exit info clobbered: (%d, %s)", p.exitStatus, p.exitTime)
	}
	if p.status != task.Status_STOPPED {
		t.Fatalf("status = %s, want STOPPED", p.status)
	}
}

func TestFinalizeTaskStoppedIdempotentOnStoppedTask(t *testing.T) {
	pre := time.Date(2026, 8, 13, 19, 0, 0, 0, time.UTC)
	p := &finalizeProbe{status: task.Status_STOPPED, exitStatus: 0, exitTime: pre}

	FinalizeTaskStopped(p, 130, time.Now())

	if p.exitStatus != 0 || !p.exitTime.Equal(pre) {
		t.Fatal("finalize on an already-STOPPED task must not touch exit info")
	}
	if !p.ioExited {
		t.Fatal("finalize must still ensure the exit channel is closed")
	}
}
