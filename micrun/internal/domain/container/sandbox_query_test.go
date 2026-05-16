package container

import (
	"context"
	"errors"
	"strings"
	"testing"

	er "micrun/internal/support/errors"
)

func TestStatusContainerReturnsNotFoundForMissingContainer(t *testing.T) {
	sandbox := &Sandbox{
		id:         "sandbox-status",
		containers: map[string]*Container{},
	}

	_, err := sandbox.StatusContainer(context.Background(), "missing")
	if !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("StatusContainer error = %v, want ContainerNotFound", err)
	}
}

func TestAnnotationReportsMissingConfig(t *testing.T) {
	_, err := (&Sandbox{}).Annotation("name")
	if err == nil || !strings.Contains(err.Error(), "sandbox config") {
		t.Fatalf("Annotation error = %v, want sandbox config error", err)
	}
}

func TestAnnotationRejectsEmptyKey(t *testing.T) {
	_, err := (&Sandbox{}).Annotation("")
	if err == nil || !strings.Contains(err.Error(), "annotation key") {
		t.Fatalf("Annotation error = %v, want annotation key error", err)
	}
}

func TestAnnotationAllowsMissingLockForRestoredTestState(t *testing.T) {
	sandbox := &Sandbox{
		config: &SandboxConfig{
			Annotations: map[string]string{"name": "value"},
		},
	}

	got, err := sandbox.Annotation("name")
	if err != nil {
		t.Fatalf("Annotation returned error: %v", err)
	}
	if got != "value" {
		t.Fatalf("Annotation = %q, want value", got)
	}
}

func TestStatusContainerReturnsEmptyContainerID(t *testing.T) {
	sandbox := &Sandbox{
		id:         "sandbox-status",
		containers: map[string]*Container{},
	}

	_, err := sandbox.StatusContainer(context.Background(), "")
	if !errors.Is(err, er.EmptyContainerID) {
		t.Fatalf("StatusContainer error = %v, want EmptyContainerID", err)
	}
}

func TestStatusContainerReportsMissingContainerConfig(t *testing.T) {
	sandbox := &Sandbox{
		id: "sandbox-status",
		containers: map[string]*Container{
			"container1": {id: "container1", state: ContainerState{State: StateReady}},
		},
	}

	_, err := sandbox.StatusContainer(context.Background(), "container1")
	if err == nil || !strings.Contains(err.Error(), "container config") {
		t.Fatalf("StatusContainer error = %v, want container config error", err)
	}
}

func TestSandboxContainerQueriesRejectEmptyContainerID(t *testing.T) {
	sandbox := &Sandbox{
		id:         "sandbox-query",
		state:      SandboxState{State: StateRunning},
		containers: map[string]*Container{},
	}
	ctx := context.Background()

	if _, err := sandbox.StatsContainer(ctx, ""); !errors.Is(err, er.EmptyContainerID) {
		t.Fatalf("StatsContainer error = %v, want EmptyContainerID", err)
	}
	if _, _, _, err := sandbox.IOStream(ctx, "", "task1"); !errors.Is(err, er.EmptyContainerID) {
		t.Fatalf("IOStream error = %v, want EmptyContainerID", err)
	}
	if _, err := sandbox.WaitContainerExit(ctx, ""); !errors.Is(err, er.EmptyContainerID) {
		t.Fatalf("WaitContainerExit error = %v, want EmptyContainerID", err)
	}
	if err := sandbox.WinResize(ctx, "", 24, 80); !errors.Is(err, er.EmptyContainerID) {
		t.Fatalf("WinResize error = %v, want EmptyContainerID", err)
	}
	if _, _, err := sandbox.OpenTTYs(ctx, ""); !errors.Is(err, er.EmptyContainerID) {
		t.Fatalf("OpenTTYs error = %v, want EmptyContainerID", err)
	}
}

func TestSandboxContainerQueriesHonorCanceledContext(t *testing.T) {
	sandbox := &Sandbox{
		id:         "sandbox-query",
		state:      SandboxState{State: StateRunning},
		containers: map[string]*Container{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := sandbox.StatusContainer(ctx, "container1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("StatusContainer error = %v, want context.Canceled", err)
	}
	if _, err := sandbox.StatsContainer(ctx, "container1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("StatsContainer error = %v, want context.Canceled", err)
	}
	if _, _, _, err := sandbox.IOStream(ctx, "container1", "task1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("IOStream error = %v, want context.Canceled", err)
	}
	if _, err := sandbox.WaitContainerExit(ctx, "container1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitContainerExit error = %v, want context.Canceled", err)
	}
	if err := sandbox.WinResize(ctx, "container1", 24, 80); !errors.Is(err, context.Canceled) {
		t.Fatalf("WinResize error = %v, want context.Canceled", err)
	}
	if _, _, err := sandbox.OpenTTYs(ctx, "container1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenTTYs error = %v, want context.Canceled", err)
	}
}

func TestSandboxQueriesTreatNilContainerAsNotFound(t *testing.T) {
	sandbox := &Sandbox{
		id:         "sandbox-query",
		state:      SandboxState{State: StateRunning},
		containers: map[string]*Container{"container1": nil},
	}
	ctx := context.Background()

	if _, err := sandbox.StatusContainer(ctx, "container1"); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("StatusContainer error = %v, want ContainerNotFound", err)
	}
	if _, err := sandbox.StatsContainer(ctx, "container1"); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("StatsContainer error = %v, want ContainerNotFound", err)
	}
	if _, _, _, err := sandbox.IOStream(ctx, "container1", "task1"); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("IOStream error = %v, want ContainerNotFound", err)
	}
	if _, err := sandbox.WaitContainerExit(ctx, "container1"); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("WaitContainerExit error = %v, want ContainerNotFound", err)
	}
	if err := sandbox.WinResize(ctx, "container1", 24, 80); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("WinResize error = %v, want ContainerNotFound", err)
	}
	if _, _, err := sandbox.OpenTTYs(ctx, "container1"); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("OpenTTYs error = %v, want ContainerNotFound", err)
	}
}

func TestWaitContainerExitAcceptsNilContext(t *testing.T) {
	exitNotifier := make(chan struct{})
	close(exitNotifier)
	sandbox := &Sandbox{
		id: "sandbox-query",
		containers: map[string]*Container{
			"container1": {
				id:           "container1",
				state:        ContainerState{State: StateRunning},
				exitNotifier: exitNotifier,
			},
		},
	}

	if _, err := sandbox.WaitContainerExit(nil, "container1"); err != nil {
		t.Fatalf("WaitContainerExit returned error for nil context: %v", err)
	}
}

func TestGetAllContainersReturnsSortedByID(t *testing.T) {
	sandbox := &Sandbox{
		id: "sandbox-query",
		containers: map[string]*Container{
			"worker-b": {id: "worker-b"},
			"infra":    {id: "infra"},
			"worker-a": {id: "worker-a"},
		},
	}

	containers := sandbox.GetAllContainers()
	if len(containers) != 3 {
		t.Fatalf("GetAllContainers length = %d, want 3", len(containers))
	}

	got := []string{containers[0].ID(), containers[1].ID(), containers[2].ID()}
	want := []string{"infra", "worker-a", "worker-b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GetAllContainers IDs = %v, want %v", got, want)
		}
	}
}

func TestGetAllContainersSkipsNilEntries(t *testing.T) {
	sandbox := &Sandbox{
		id: "sandbox-query",
		containers: map[string]*Container{
			"worker": {id: "worker"},
			"nil":    nil,
		},
	}

	containers := sandbox.GetAllContainers()
	if len(containers) != 1 {
		t.Fatalf("GetAllContainers length = %d, want 1", len(containers))
	}
	if got := containers[0].ID(); got != "worker" {
		t.Fatalf("GetAllContainers ID = %q, want worker", got)
	}
}

func TestNilSandboxGetAllContainersReturnsEmpty(t *testing.T) {
	var sandbox *Sandbox
	if got := sandbox.GetAllContainers(); len(got) != 0 {
		t.Fatalf("nil GetAllContainers length = %d, want 0", len(got))
	}
}

func TestNilSandboxIDReturnsEmptyString(t *testing.T) {
	var sandbox *Sandbox
	if got := sandbox.SandboxID(); got != "" {
		t.Fatalf("nil SandboxID() = %q, want empty", got)
	}
}

func TestNilSandboxReadOnlyQueriesReturnZeroValues(t *testing.T) {
	var sandbox *Sandbox
	if got := sandbox.GetNetNamespace(); got != "" {
		t.Fatalf("nil GetNetNamespace() = %q, want empty", got)
	}
	if got := sandbox.GuestCtl(); got != nil {
		t.Fatalf("nil GuestCtl() = %v, want nil", got)
	}
	if got := sandbox.NetnsHolderPID(); got != 0 {
		t.Fatalf("nil NetnsHolderPID() = %d, want 0", got)
	}
	if got := sandbox.GetState(); got != StateDown {
		t.Fatalf("nil GetState() = %s, want %s", got, StateDown)
	}
	if !sandbox.notOperational() {
		t.Fatal("nil sandbox should be not operational")
	}
}

func TestNilSandboxErrorQueriesReturnSandboxNotFound(t *testing.T) {
	var sandbox *Sandbox
	ctx := context.Background()

	if _, err := sandbox.StatusContainer(ctx, "container1"); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("StatusContainer error = %v, want SandboxNotFound", err)
	}
	if _, err := sandbox.StatsContainer(ctx, "container1"); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("StatsContainer error = %v, want SandboxNotFound", err)
	}
	if _, _, _, err := sandbox.IOStream(ctx, "container1", "task1"); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("IOStream error = %v, want SandboxNotFound", err)
	}
	if _, err := sandbox.WaitContainerExit(ctx, "container1"); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("WaitContainerExit error = %v, want SandboxNotFound", err)
	}
	if err := sandbox.WinResize(ctx, "container1", 24, 80); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("WinResize error = %v, want SandboxNotFound", err)
	}
	if _, _, err := sandbox.OpenTTYs(ctx, "container1"); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("OpenTTYs error = %v, want SandboxNotFound", err)
	}
}

func TestNilSandboxStateSettersReturnSandboxNotFound(t *testing.T) {
	var sandbox *Sandbox
	if err := sandbox.SetSandboxState(StateRunning); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("SetSandboxState error = %v, want SandboxNotFound", err)
	}
	if err := sandbox.setSandboxState(StateRunning); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("setSandboxState error = %v, want SandboxNotFound", err)
	}
}
