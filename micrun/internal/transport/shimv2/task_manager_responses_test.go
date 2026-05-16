package shim

import (
	"testing"
	"time"

	apptask "micrun/internal/application/task"

	tasktypes "github.com/containerd/containerd/api/types/task"
	ptypes "github.com/containerd/containerd/protobuf/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTaskManagerResponseMappersPreserveLifecycleFields(t *testing.T) {
	exitedAt := time.Date(2026, 4, 27, 8, 9, 10, 0, time.UTC)

	create := createTaskResponse(&apptask.CreateOutput{Pid: 101})
	if create.Pid != 101 {
		t.Fatalf("create response pid = %d, want 101", create.Pid)
	}

	start := startTaskResponse(&apptask.StartOutput{Pid: 202})
	if start.Pid != 202 {
		t.Fatalf("start response pid = %d, want 202", start.Pid)
	}

	ts := timestamppb.New(exitedAt)
	baseDelete := deleteResponse(9, ts, 707)
	if baseDelete.ExitStatus != 9 || baseDelete.Pid != 707 || baseDelete.ExitedAt != ts {
		t.Fatalf("base delete response = %#v, want exit 9 pid 707 timestamp identity", baseDelete)
	}

	del := deleteTaskResponse(&apptask.DeleteOutput{
		ExitStatus: 137,
		ExitedAt:   exitedAt,
		Pid:        303,
	}, ts)
	if del.ExitStatus != 137 || del.Pid != 303 || del.ExitedAt != ts {
		t.Fatalf("delete response = %#v, want exit 137 pid 303 timestamp identity", del)
	}

	wait := waitTaskResponse(&apptask.WaitOutput{ExitStatus: 42, ExitedAt: exitedAt})
	if wait.ExitStatus != 42 || !wait.ExitedAt.AsTime().Equal(exitedAt) {
		t.Fatalf("wait response = %#v, want exit 42 at %s", wait, exitedAt)
	}
}

func TestTaskManagerResponseMappersPreserveQueryFields(t *testing.T) {
	exitedAt := time.Date(2026, 4, 27, 8, 9, 10, 0, time.UTC)
	state := stateTaskResponse(&apptask.StateOutput{
		ID:         "demo",
		Bundle:     "/bundle",
		Pid:        404,
		Status:     tasktypes.Status_RUNNING,
		Stdin:      "stdin",
		Stdout:     "stdout",
		Stderr:     "stderr",
		Terminal:   true,
		ExitStatus: 0,
		ExitedAt:   exitedAt,
		ExecID:     "",
	})
	if state.ID != "demo" || state.Bundle != "/bundle" || state.Pid != 404 {
		t.Fatalf("state identity fields = (%q, %q, %d), want demo bundle pid", state.ID, state.Bundle, state.Pid)
	}
	if state.Status != tasktypes.Status_RUNNING || !state.Terminal || !state.ExitedAt.AsTime().Equal(exitedAt) {
		t.Fatalf("state runtime fields = %#v, want running terminal at %s", state, exitedAt)
	}

	pids := pidsResponse(505)
	if len(pids.Processes) != 1 || pids.Processes[0].Pid != 505 {
		t.Fatalf("pids response = %#v, want one pid 505", pids)
	}

	metrics := &ptypes.Any{TypeUrl: "types.test", Value: []byte{1, 2, 3}}
	if statsResponse(metrics).Stats != metrics {
		t.Fatal("stats response did not preserve metrics payload")
	}

	connect := connectResponse(606)
	if connect.ShimPid != 606 || connect.TaskPid != 606 {
		t.Fatalf("connect response pids = (%d, %d), want 606", connect.ShimPid, connect.TaskPid)
	}
}
