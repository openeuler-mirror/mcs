package shim

import (
	"testing"

	"github.com/containerd/containerd/api/types/task"
)

// The handle is the single choke point for status writes: illegal lifecycle
// transitions must be rejected regardless of which service-layer path issues
// them, so a guard that slipped in a future refactor cannot resurrect a
// STOPPED task or un-start a RUNNING one.
func TestShimContainerSetStatusEnforcesStateMachine(t *testing.T) {
	cases := []struct {
		name string
		from task.Status
		to   task.Status
		want task.Status
	}{
		{"stopped is absorbing", task.Status_STOPPED, task.Status_RUNNING, task.Status_STOPPED},
		{"no un-start", task.Status_RUNNING, task.Status_CREATED, task.Status_RUNNING},
		{"start", task.Status_CREATED, task.Status_RUNNING, task.Status_RUNNING},
		{"pause commit", task.Status_PAUSING, task.Status_PAUSED, task.Status_PAUSED},
		{"pause rollback", task.Status_PAUSING, task.Status_RUNNING, task.Status_RUNNING},
		{"kill from paused", task.Status_PAUSED, task.Status_STOPPED, task.Status_STOPPED},
		{"recovery seed", task.Status_UNKNOWN, task.Status_PAUSED, task.Status_PAUSED},
		{"no unknown demotion", task.Status_RUNNING, task.Status_UNKNOWN, task.Status_RUNNING},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sc := &shimContainer{id: "t", status: c.from}
			sc.SetStatus(c.to)
			if sc.Status() != c.want {
				t.Fatalf("status after %s -> %s = %s, want %s", c.from, c.to, sc.Status(), c.want)
			}
		})
	}
}
