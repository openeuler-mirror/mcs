package io

import (
	"context"
	"io"
	"syscall"
	"testing"
	"time"
)

type fdReadCloser struct {
	fd uintptr
}

func (f fdReadCloser) Read(p []byte) (int, error) { return 0, io.EOF }
func (f fdReadCloser) Close() error               { return nil }
func (f fdReadCloser) Fd() uintptr                { return f.fd }

func TestMarkStdinDataReceivedMarksAttachAfterInitialData(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "stdin-initial-data"})
	defer copier.finishStop(0, false)

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
	defer copier.finishStop(0, false)
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

func TestHandleStdinEOFNonTTYKeepsSessionForReattach(t *testing.T) {
	bus := NewEventBus(context.Background())
	t.Cleanup(bus.Close)
	detached := bus.Subscribe(ClientDetached)
	copier := NewCopier(Config{
		ContainerID: "nontty-keep",
		Terminal:    false,
		EventBus:    bus,
	})
	t.Cleanup(func() { copier.finishStop(0, false) })
	copier.attachClientConnected = true
	copier.liveClientPublished.Store(true)

	if got := copier.handleStdinEOF(); got != stdinLoopContinue {
		t.Fatalf("handleStdinEOF = %v, want continue so start -d can later attach", got)
	}
	if copier.attachClientConnected {
		t.Fatal("attach client flag should clear after stdin EOF")
	}
	if !copier.stdinEOFSeen {
		t.Fatal("stdinEOFSeen should wait for the next attach writer")
	}
	if copier.liveClientPublished.Load() {
		t.Fatal("liveClientPublished should reset so the next attach can mark attached")
	}
	select {
	case ev := <-detached:
		if ev.ContainerID != "nontty-keep" {
			t.Fatalf("ClientDetached container = %q", ev.ContainerID)
		}
	case <-time.After(time.Second):
		t.Fatal("expected ClientDetached when a non-TTY attach writer closes stdin")
	}
}

func TestHandleStdinEAGAINIsNoOp(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "stdin-eagain"})
	defer copier.finishStop(0, false)
	copier.stdinEOFSeen = true
	copier.stdinWaiter.disabled = true

	// EAGAIN after EOF does NOT mean a writer appeared — only actual data
	// does. The handler must not clear the EOF-seen state (that would
	// reintroduce the epoll busy-loop on the continuous HUP).
	copier.handleStdinEAGAIN()

	if !copier.stdinEOFSeen {
		t.Fatal("stdinEOFSeen = false, want true (EAGAIN is not a writer signal)")
	}
}

func TestStdinFIFOFDUsesGenericFDProvider(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "stdin-fd"})
	defer copier.finishStop(0, false)
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
	defer copier.finishStop(0, false)

	if copier.waitForData(&copier.ttyWaiter, -1) {
		t.Fatal("waitForData(-1) should return false for canceled context")
	}
}

type scriptedStdin struct {
	reads []scriptedRead
	i     int
}

type scriptedRead struct {
	err error
}

func (s *scriptedStdin) Read([]byte) (int, error) {
	if s.i >= len(s.reads) {
		return 0, io.EOF
	}
	step := s.reads[s.i]
	s.i++
	return 0, step.err
}

func (s *scriptedStdin) Close() error { return nil }

func TestCopyStdinMarksLiveClientOnEAGAINAfterEOF(t *testing.T) {
	bus := NewEventBus(context.Background())
	t.Cleanup(bus.Close)
	attached := bus.Subscribe(ClientAttached)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	copier := NewCopier(Config{
		ContainerID: "eagain-after-eof",
		Context:     ctx,
		EventBus:    bus,
	})
	t.Cleanup(func() { copier.finishStop(0, false) })
	copier.SetStdin(&scriptedStdin{reads: []scriptedRead{
		{err: io.EOF},
		{err: syscall.EAGAIN},
	}})

	copier.wg.Add(1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		copier.copyStdin()
	}()

	select {
	case ev := <-attached:
		if ev.ContainerID != "eagain-after-eof" {
			t.Fatalf("ClientAttached container = %q", ev.ContainerID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected ClientAttached when a writer appears after create-time EOF")
	}
	cancel()
	<-done
}

func TestCopyStdinRepublishesLiveClientAfterNonTTYDetach(t *testing.T) {
	bus := NewEventBus(context.Background())
	t.Cleanup(bus.Close)
	attached := bus.Subscribe(ClientAttached)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	copier := NewCopier(Config{
		ContainerID: "reattach-live-client",
		Context:     ctx,
		EventBus:    bus,
		Terminal:    false,
	})
	t.Cleanup(func() { copier.finishStop(0, false) })
	copier.SetStdin(&scriptedStdin{reads: []scriptedRead{
		{err: io.EOF},
		{err: syscall.EAGAIN},
		{err: io.EOF},
		{err: syscall.EAGAIN},
	}})

	copier.wg.Add(1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		copier.copyStdin()
	}()

	for i := 1; i <= 2; i++ {
		select {
		case ev := <-attached:
			if ev.ContainerID != "reattach-live-client" {
				t.Fatalf("ClientAttached #%d container = %q", i, ev.ContainerID)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("expected ClientAttached #%d after attach writer appeared", i)
		}
	}
	cancel()
	<-done
}

func TestWaitForStdinOrCancelFallsBackWhenTargetFdMissingForCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	copier := NewCopier(Config{ContainerID: "wait-stdin-missing-fd", Context: ctx})
	defer copier.finishStop(0, false)

	if copier.waitForStdinOrCancel(-1) {
		t.Fatal("waitForStdinOrCancel(-1) should return false for canceled context")
	}
}
