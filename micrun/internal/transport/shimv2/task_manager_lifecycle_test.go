package shim

import (
	"context"
	"errors"
	"strings"
	"testing"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	tasktypes "github.com/containerd/containerd/api/types/task"
	ptypes "github.com/containerd/containerd/protobuf/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTaskManagerExecReturnsNotImplemented(t *testing.T) {
	manager, err := newTaskRPCShimService().getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	_, err = manager.Exec(context.Background(), &taskAPI.ExecProcessRequest{
		ID:     "demo",
		ExecID: "exec-1",
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("Exec() error = %v, want gRPC code %s", err, codes.Unimplemented)
	}
}

func TestGrpcExecAwareErrorTranslatesOnlyExecRequests(t *testing.T) {
	baseErr := errors.New("exec is unavailable")
	if got := grpcExecAwareError("", baseErr); !errors.Is(got, baseErr) {
		t.Fatalf("grpcExecAwareError() = %v, want original error", got)
	}

	got := grpcExecAwareError("exec-1", baseErr)
	if status.Code(got) != codes.Unimplemented {
		t.Fatalf("grpcExecAwareError() code = %s, want %s", status.Code(got), codes.Unimplemented)
	}
}

func TestGrpcExecAwareErrorWithFallbackPreservesFallback(t *testing.T) {
	baseErr := errors.New("start failed")
	fallbackErr := status.Error(codes.Unknown, "converted")

	got := grpcExecAwareErrorWithFallback("", baseErr, func(error) error {
		return fallbackErr
	})
	if status.Code(got) != codes.Unknown {
		t.Fatalf("grpcExecAwareErrorWithFallback() code = %s, want %s", status.Code(got), codes.Unknown)
	}

	got = grpcExecAwareErrorWithFallback("exec-1", baseErr, func(error) error {
		return fallbackErr
	})
	if status.Code(got) != codes.Unimplemented {
		t.Fatalf("grpcExecAwareErrorWithFallback() exec code = %s, want %s", status.Code(got), codes.Unimplemented)
	}
}

func TestRequireTransportRequest(t *testing.T) {
	if err := requireTransportRequest("state", &taskAPI.StateRequest{}); err != nil {
		t.Fatalf("requireTransportRequest returned error for non-nil request: %v", err)
	}

	err := requireTransportRequest("state", (*taskAPI.StateRequest)(nil))
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("nil request error = %v, want InvalidArgument", err)
	}
	if !strings.Contains(err.Error(), "state request is nil") {
		t.Fatalf("nil request error = %v, want state request message", err)
	}
}

func TestTaskManagerNilRequestsReturnInvalidArgument(t *testing.T) {
	manager, err := newTaskRPCShimService().getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}
	ctx := context.Background()
	tests := map[string]func() error{
		"Create": func() error { _, err := manager.Create(ctx, nil); return err },
		"Start":  func() error { _, err := manager.Start(ctx, nil); return err },
		"Delete": func() error { _, err := manager.Delete(ctx, nil); return err },
		"Pause":  func() error { _, err := manager.Pause(ctx, nil); return err },
		"Resume": func() error { _, err := manager.Resume(ctx, nil); return err },
		"Kill":   func() error { _, err := manager.Kill(ctx, nil); return err },
		"Resize": func() error { _, err := manager.ResizePty(ctx, nil); return err },
		"CloseIO": func() error {
			_, err := manager.CloseIO(ctx, nil)
			return err
		},
		"Update": func() error { _, err := manager.Update(ctx, nil); return err },
		"Wait":   func() error { _, err := manager.Wait(ctx, nil); return err },
		"Exec":   func() error { _, err := manager.Exec(ctx, nil); return err },
		"Checkpoint": func() error {
			_, err := manager.Checkpoint(ctx, nil)
			return err
		},
		"State": func() error { _, err := manager.State(ctx, nil); return err },
		"Stats": func() error { _, err := manager.Stats(ctx, nil); return err },
	}

	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked with nil request: %v", name, r)
				}
			}()
			if err := call(); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("%s nil request error = %v, want gRPC code %s", name, err, codes.InvalidArgument)
			}
		})
	}
}

func TestTaskManagerCheckpointReturnsNotImplemented(t *testing.T) {
	manager, err := newTaskRPCShimService().getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	_, err = manager.Checkpoint(context.Background(), &taskAPI.CheckpointTaskRequest{ID: "demo"})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("Checkpoint() error = %v, want gRPC code %s", err, codes.Unimplemented)
	}
}

func TestTaskManagerUpdateRejectsInvalidResources(t *testing.T) {
	manager, err := newTaskRPCShimService().getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	_, err = manager.Update(context.Background(), &taskAPI.UpdateTaskRequest{
		ID:        "demo",
		Resources: &ptypes.Any{TypeUrl: "invalid", Value: []byte("not-a-protobuf")},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Update invalid resources error = %v, want gRPC code %s", err, codes.InvalidArgument)
	}
}

func TestTaskManagerStartExecReturnsNotImplemented(t *testing.T) {
	service := newTaskRPCShimService()
	service.containers["demo"] = &shimContainer{id: "demo", status: tasktypes.Status_CREATED}
	manager, err := service.getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	_, err = manager.Start(context.Background(), &taskAPI.StartRequest{
		ID:     "demo",
		ExecID: "exec-1",
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("Start() exec error = %v, want gRPC code %s", err, codes.Unimplemented)
	}
}

func TestTaskManagerKillExecReturnsNotImplemented(t *testing.T) {
	service := newTaskRPCShimService()
	service.containers["demo"] = &shimContainer{id: "demo", status: tasktypes.Status_RUNNING}
	manager, err := service.getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	_, err = manager.Kill(context.Background(), &taskAPI.KillRequest{
		ID:     "demo",
		ExecID: "exec-1",
		Signal: 9,
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("Kill() exec error = %v, want gRPC code %s", err, codes.Unimplemented)
	}
}

func TestTaskManagerCloseIOExecReturnsNotImplemented(t *testing.T) {
	service := newTaskRPCShimService()
	service.containers["demo"] = &shimContainer{id: "demo", status: tasktypes.Status_RUNNING}
	manager, err := service.getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	_, err = manager.CloseIO(context.Background(), &taskAPI.CloseIORequest{
		ID:     "demo",
		ExecID: "exec-1",
		Stdin:  true,
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("CloseIO() exec error = %v, want gRPC code %s", err, codes.Unimplemented)
	}
}

func TestTaskManagerResizePtyExecReturnsNotImplemented(t *testing.T) {
	service := newTaskRPCShimService()
	service.containers["demo"] = &shimContainer{id: "demo", status: tasktypes.Status_RUNNING}
	manager, err := service.getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	_, err = manager.ResizePty(context.Background(), &taskAPI.ResizePtyRequest{
		ID:     "demo",
		ExecID: "exec-1",
		Height: 24,
		Width:  80,
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("ResizePty() exec error = %v, want gRPC code %s", err, codes.Unimplemented)
	}
}

func TestTaskManagerStateExecReturnsNotImplemented(t *testing.T) {
	service := newTaskRPCShimService()
	service.containers["demo"] = &shimContainer{id: "demo", status: tasktypes.Status_RUNNING}
	manager, err := service.getTaskManager()
	if err != nil {
		t.Fatalf("getTaskManager() error = %v", err)
	}

	_, err = manager.State(context.Background(), &taskAPI.StateRequest{
		ID:     "demo",
		ExecID: "exec-1",
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("State() exec error = %v, want gRPC code %s", err, codes.Unimplemented)
	}
}

func TestShimRPCMissingTaskManagerReturnsError(t *testing.T) {
	_, err := (&shimService{}).State(context.Background(), &taskAPI.StateRequest{ID: "demo"})
	if err == nil {
		t.Fatal("State() expected error for missing task manager, got nil")
	}
	if !strings.Contains(err.Error(), "task manager is not configured") {
		t.Fatalf("State() error = %v, want missing task manager", err)
	}
}
