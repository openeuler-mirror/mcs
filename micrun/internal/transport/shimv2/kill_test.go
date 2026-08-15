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

// A signal that is not mapped to a guest action was never delivered to the
// guest. Answering success would make kubelet believe the workload got a
// chance to shut down gracefully and wait out the full termination grace
// period before the SIGKILL fallback (exit 137 instead of a clean stop).
func TestKillRejectsUnmappedSignalInsteadOfFakeSuccess(t *testing.T) {
	t.Parallel()

	svc := newTaskRPCShimService()
	container := &shimContainer{
		id:     "test-ctr",
		status: task.Status_RUNNING,
	}
	svc.containers[container.id] = container

	// Signal 0 is a presence probe and must stay a silent success.
	resp, err := svc.Kill(context.Background(), &taskAPI.KillRequest{
		ID:     container.id,
		Signal: 0,
	})
	if err != nil {
		t.Fatalf("Kill(signal 0) returned unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("Kill(signal 0) returned nil response")
	}

	// An unmapped signal (SIGUSR1) must surface an error, not fake success.
	_, err = svc.Kill(context.Background(), &taskAPI.KillRequest{
		ID:     container.id,
		Signal: uint32(syscall.SIGUSR1),
	})
	if err == nil {
		t.Fatal("Kill returned fake success for an unmapped signal; the guest was never signalled")
	}
}
