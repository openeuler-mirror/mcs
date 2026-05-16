package shim

import (
	"context"
	"syscall"
	"testing"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"
)

func TestKillIgnoresSIGCONTWhenSandboxIsMissing(t *testing.T) {
	t.Parallel()

	svc := newTaskRPCShimService()
	container := &shimContainer{
		id:     "test-ctr",
		status: task.Status_STOPPED,
	}
	svc.containers[container.id] = container

	resp, err := svc.Kill(context.Background(), &taskAPI.KillRequest{
		ID:     container.id,
		Signal: uint32(syscall.SIGCONT),
	})
	if err != nil {
		t.Fatalf("Kill returned unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("Kill returned nil response")
	}
}
