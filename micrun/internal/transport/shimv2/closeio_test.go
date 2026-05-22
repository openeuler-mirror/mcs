package shim

import (
	"context"
	"io"
	"testing"
	"time"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"
)

type trackingWriteCloser struct {
	closed chan struct{}
}

func (t *trackingWriteCloser) Write(p []byte) (int, error) {
	return len(p), nil
}

func (t *trackingWriteCloser) Close() error {
	select {
	case <-t.closed:
	default:
		close(t.closed)
	}
	return nil
}

var _ io.WriteCloser = (*trackingWriteCloser)(nil)

func TestCloseIODoesNotHoldServiceLockWhileWaitingForStdinCloser(t *testing.T) {
	t.Parallel()

	stdinClosed := make(chan struct{})
	stdinCloser := make(chan struct{})

	svc := newTaskRPCShimService()
	container := &shimContainer{
		id:          "closeio-test",
		status:      task.Status_RUNNING,
		stdinPipe:   &trackingWriteCloser{closed: stdinClosed},
		stdinCloser: stdinCloser,
	}
	svc.containers[container.id] = container

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.CloseIO(context.Background(), &taskAPI.CloseIORequest{
			ID:    container.id,
			Stdin: true,
		})
	}()

	select {
	case <-stdinClosed:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("CloseIO did not close stdin pipe")
	}

	lockAcquired := make(chan struct{})
	go func() {
		svc.Lock()
		svc.Unlock() //nolint:staticcheck // SA2001: intentional — verifies lock is free
		close(lockAcquired)
	}()

	select {
	case <-lockAcquired:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("service lock remained held while CloseIO was waiting on stdinCloser")
	}

	close(stdinCloser)

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("CloseIO did not return after stdinCloser closed")
	}
}
