package io

import (
	"context"
	"io"
	"testing"
)

type fdReadCloser struct {
	fd uintptr
}

func (f fdReadCloser) Read(p []byte) (int, error) { return 0, io.EOF }
func (f fdReadCloser) Close() error               { return nil }
func (f fdReadCloser) Fd() uintptr                { return f.fd }

func TestMarkStdinDataReceivedMarksAttachAfterInitialData(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "stdin-initial-data"})
	defer copier.finishStop(0)

	copier.markStdinDataReceived()

	if !copier.attachClientConnected {
		t.Fatal("attachClientConnected = false, want true")
	}
	if copier.stdinEOFSeen {
		t.Fatal("stdinEOFSeen = true, want false")
	}
}

func TestMarkStdinDataReceivedClearsEOFAndReenablesWaiter(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "stdin-reattach-data"})
	defer copier.finishStop(0)
	copier.stdinEOFSeen = true
	copier.stdinWaiter.disabled = true

	copier.markStdinDataReceived()

	if copier.stdinEOFSeen {
		t.Fatal("stdinEOFSeen = true, want false")
	}
	if !copier.attachClientConnected {
		t.Fatal("attachClientConnected = false, want true")
	}
	if copier.stdinWaiter.disabled {
		t.Fatal("stdin waiter should be reenabled")
	}
}

func TestHandleStdinEAGAINClearsEOFAndReenablesWaiter(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "stdin-eagain"})
	defer copier.finishStop(0)
	copier.stdinEOFSeen = true
	copier.stdinWaiter.disabled = true

	copier.handleStdinEAGAIN()

	if copier.stdinEOFSeen {
		t.Fatal("stdinEOFSeen = true, want false")
	}
	if copier.stdinWaiter.disabled {
		t.Fatal("stdin waiter should be reenabled")
	}
}

func TestStdinFIFOFDUsesGenericFDProvider(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "stdin-fd"})
	defer copier.finishStop(0)
	copier.SetStdin(fdReadCloser{fd: 99})

	if got := copier.stdinFIFOFD(); got != 99 {
		t.Fatalf("stdinFIFOFD = %d, want 99", got)
	}

	copier.SetStdin(failingReadCloser{})
	if got := copier.stdinFIFOFD(); got != -1 {
		t.Fatalf("stdinFIFOFD without Fd = %d, want -1", got)
	}
}

func TestWaitForDataFallsBackWhenTargetFdMissingForCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	copier := NewCopier(Config{ContainerID: "wait-data-missing-fd", Context: ctx})
	defer copier.finishStop(0)

	if copier.waitForData(-1) {
		t.Fatal("waitForData(-1) should return false for canceled context")
	}
}

func TestWaitForStdinOrCancelFallsBackWhenTargetFdMissingForCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	copier := NewCopier(Config{ContainerID: "wait-stdin-missing-fd", Context: ctx})
	defer copier.finishStop(0)

	if copier.waitForStdinOrCancel(-1) {
		t.Fatal("waitForStdinOrCancel(-1) should return false for canceled context")
	}
}
