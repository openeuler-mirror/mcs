package shim

import (
	"bytes"
	"context"
	"testing"
	"time"

	cntr "micrun/internal/domain/container"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	tasktypes "github.com/containerd/containerd/api/types/task"
	ptypes "github.com/containerd/containerd/protobuf/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTaskManagerStatsReturnsEmptyMetricsForMissingTask(t *testing.T) {
	service := newTaskRPCShimService()
	manager, err := service.getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	resp, err := manager.Stats(context.Background(), &taskAPI.StatsRequest{ID: "missing"})
	if err != nil {
		t.Fatalf("Stats returned unexpected error: %v", err)
	}
	assertSameMetricsAny(t, resp.Stats, service.EmptyMetrics())
}

func TestTaskManagerStatsReturnsEmptyMetricsOnCollectionError(t *testing.T) {
	service := newTaskRPCShimService()
	service.containers["demo"] = &shimContainer{id: "demo", status: tasktypes.Status_RUNNING}
	manager, err := service.getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	resp, err := manager.Stats(context.Background(), &taskAPI.StatsRequest{ID: "demo"})
	if err != nil {
		t.Fatalf("Stats returned unexpected error: %v", err)
	}
	assertSameMetricsAny(t, resp.Stats, service.EmptyMetrics())
}

type lockProbeSandbox struct {
	cntr.SandboxTraits
	probe func()
}

func (s lockProbeSandbox) StatsContainer(context.Context, string) (cntr.ContainerStats, error) {
	s.probe()
	return cntr.ContainerStats{}, nil
}

func TestTaskManagerStatsCollectsOutsideRuntimeLock(t *testing.T) {
	service := newTaskRPCShimService()
	service.containers["demo"] = &shimContainer{id: "demo", status: tasktypes.Status_RUNNING}
	service.sandbox = lockProbeSandbox{probe: func() {
		acquired := make(chan struct{})
		go func() {
			service.Lock()
			service.Unlock()
			close(acquired)
		}()
		select {
		case <-acquired:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("runtime lock was held while collecting stats")
		}
	}}
	manager, err := service.getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	if _, err := manager.Stats(context.Background(), &taskAPI.StatsRequest{ID: "demo"}); err != nil {
		t.Fatalf("Stats returned unexpected error: %v", err)
	}
}

func TestTaskManagerQueryRPCsRejectNilRequests(t *testing.T) {
	service := newTaskRPCShimService()
	manager, err := service.getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	tests := []struct {
		name string
		call func() error
	}{
		{name: "pids", call: func() error {
			_, err := manager.Pids(context.Background(), nil)
			return err
		}},
		{name: "connect", call: func() error {
			_, err := manager.Connect(context.Background(), nil)
			return err
		}},
		{name: "shutdown", call: func() error {
			_, err := manager.Shutdown(context.Background(), nil)
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("%s nil request error = %v, want code %s", tt.name, err, codes.InvalidArgument)
			}
		})
	}
}

func TestTaskManagerQueryRPCsUseRuntimeShimPID(t *testing.T) {
	service := newTaskRPCShimService()
	service.shimPid = 4242
	manager, err := service.getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	pids, err := manager.Pids(context.Background(), &taskAPI.PidsRequest{})
	if err != nil {
		t.Fatalf("Pids returned error: %v", err)
	}
	if got := pids.Processes[0].Pid; got != 4242 {
		t.Fatalf("Pids pid = %d, want 4242", got)
	}

	connect, err := manager.Connect(context.Background(), &taskAPI.ConnectRequest{})
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	if connect.ShimPid != 4242 || connect.TaskPid != 4242 {
		t.Fatalf("Connect pids = (%d, %d), want 4242", connect.ShimPid, connect.TaskPid)
	}
}

func TestTaskManagerShutdownSkipsEffectsWhenTasksRemain(t *testing.T) {
	service := newTaskRPCShimService()
	service.containers["demo"] = &shimContainer{id: "demo", status: tasktypes.Status_RUNNING}
	effectsCalled := false
	service.shutdown = shutdownEffects{
		exit: func(int) {
			effectsCalled = true
		},
	}
	manager, err := service.getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	if _, err := manager.Shutdown(context.Background(), &taskAPI.ShutdownRequest{}); err != nil {
		t.Fatalf("Shutdown returned unexpected error: %v", err)
	}
	if effectsCalled {
		t.Fatal("shutdown effects ran while tasks remain")
	}
}

func TestTaskManagerShutdownRunsInjectedEffects(t *testing.T) {
	service := newTaskRPCShimService()
	shutdownCalled := false
	exitCode := -1
	removedSocket := ""
	service.ss = func() {
		shutdownCalled = true
	}
	service.shutdown = shutdownEffects{
		readAddress: func(path string) (string, error) {
			if path != "address" {
				t.Fatalf("readAddress path = %q, want address", path)
			}
			return "unix:///tmp/micrun-shim.sock", nil
		},
		removeSocket: func(addr string) error {
			removedSocket = addr
			return nil
		},
		exit: func(code int) {
			exitCode = code
		},
	}
	manager, err := service.getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	if _, err := manager.Shutdown(context.Background(), &taskAPI.ShutdownRequest{}); err != nil {
		t.Fatalf("Shutdown returned unexpected error: %v", err)
	}
	if !shutdownCalled {
		t.Fatal("shutdown callback was not called")
	}
	if removedSocket != "unix:///tmp/micrun-shim.sock" {
		t.Fatalf("removed socket = %q, want injected address", removedSocket)
	}
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
}

func assertSameMetricsAny(t *testing.T, got, want *ptypes.Any) {
	t.Helper()
	if got == nil {
		t.Fatal("metrics payload is nil")
	}
	if got.GetTypeUrl() != want.GetTypeUrl() || !bytes.Equal(got.GetValue(), want.GetValue()) {
		t.Fatalf("metrics payload mismatch: got type %q len %d, want type %q len %d",
			got.GetTypeUrl(), len(got.GetValue()), want.GetTypeUrl(), len(want.GetValue()))
	}
}
