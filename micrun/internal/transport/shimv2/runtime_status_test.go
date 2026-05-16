package shim

import (
	"testing"

	cntr "micrun/internal/domain/container"

	"github.com/containerd/containerd/api/types/task"
)

func TestTaskStatusFromContainerState(t *testing.T) {
	tests := map[cntr.StateString]task.Status{
		cntr.StateReady:    task.Status_CREATED,
		cntr.StateRunning:  task.Status_RUNNING,
		cntr.StatePaused:   task.Status_PAUSED,
		cntr.StateStopped:  task.Status_STOPPED,
		cntr.StateCreating: task.Status_UNKNOWN,
		cntr.StateDown:     task.Status_UNKNOWN,
		"future-state":     task.Status_UNKNOWN,
	}

	for state, want := range tests {
		if got := taskStatusFromContainerState(state); got != want {
			t.Fatalf("taskStatusFromContainerState(%q) = %s, want %s", state, got, want)
		}
	}
}
