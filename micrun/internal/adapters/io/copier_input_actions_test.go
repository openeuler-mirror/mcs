package io

import (
	"context"
	"errors"
	"testing"
	"time"

	"micrun/internal/domain/console"
)

type errorTTYWriter struct {
	err error
}

func (w errorTTYWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (w errorTTYWriter) Close() error {
	return nil
}

func TestInputActionHandlersDispatchAndStop(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()
	events := bus.Subscribe(ExitCommandDetected)

	ttyOut := &recordingTTYWriter{}
	stdoutFIFO := &recordingTTYWriter{}
	stdoutEcho := &recordingTTYWriter{}
	copier := NewCopier(Config{
		ContainerID: "input-action-stop",
		EventBus:    bus,
		Terminal:    true,
	})
	defer copier.finishStop(0)
	copier.SetTTYs(ttyOut, nil, nil)
	copier.SetStdout(stdoutFIFO)
	copier.SetStdoutFifoForEcho(stdoutEcho)

	copier.executeInputActions([]console.Action{
		{Kind: console.ActionWriteTTY, Data: []byte("hello\n")},
		{Kind: console.ActionLocalEcho, Data: []byte("echo")},
		{Kind: console.ActionWriteStdout, Data: []byte("from-stdin")},
		{Kind: console.ActionTrackEcho, Data: []byte("xy")},
		{Kind: console.ActionEmitEvent, Event: console.EventExitCommand, StopMode: console.ActionStopClose},
		{Kind: console.ActionWriteStdout, Data: []byte("should-not-run")},
	})

	if got, want := ttyOut.String(), "hello\n"; got != want {
		t.Fatalf("tty writes = %q, want %q", got, want)
	}
	if got, want := stdoutFIFO.String(), "from-stdin"; got != want {
		t.Fatalf("stdout FIFO writes = %q, want %q", got, want)
	}
	if got, want := stdoutEcho.String(), "echo"; got != want {
		t.Fatalf("local echo writes = %q, want %q", got, want)
	}
	if copier.echoSuppressor.Len() != 2 {
		t.Fatalf("echo suppressor len = %d, want 2", copier.echoSuppressor.Len())
	}
	if !copier.stopped.Load() {
		t.Fatal("copier should stop after action with Stop=true")
	}

	select {
	case event := <-events:
		if event.Type != ExitCommandDetected {
			t.Fatalf("event type = %v, want ExitCommandDetected", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ExitCommandDetected")
	}
}

func TestExecuteInputActionsDetachStopsWithoutClosingStreams(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()
	events := bus.Subscribe(DetachDetected)

	copier := NewCopier(Config{
		ContainerID: "input-action-detach",
		EventBus:    bus,
		Terminal:    true,
	})
	defer copier.finishStop(0)
	copier.SetStdin(failingReadCloser{})
	copier.SetStdout(failingWriteCloser{})
	copier.SetStderr(failingWriteCloser{})
	copier.SetTTYs(failingWriteCloser{}, failingReadCloser{}, failingReadCloser{})

	copier.executeInputActions([]console.Action{
		{Kind: console.ActionEmitEvent, Event: console.EventDetach},
	})

	if !copier.stopped.Load() {
		t.Fatal("copier should stop after detach action")
	}
	if copier.stdinFifo == nil || copier.stdoutFIFO == nil || copier.stderrFIFO == nil {
		t.Fatal("detach action should preserve FIFO handles")
	}
	if copier.ttyIn == nil || copier.ttyOut == nil || copier.ttyErr == nil {
		t.Fatal("detach action should preserve TTY handles")
	}

	select {
	case event := <-events:
		if event.Type != DetachDetected {
			t.Fatalf("event type = %v, want DetachDetected", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for DetachDetected")
	}
}

func TestInputActionWriteTTYPublishesIOError(t *testing.T) {
	expectedErr := errors.New("tty write failed")
	bus := NewEventBus(context.Background())
	defer bus.Close()
	events := bus.Subscribe(IOError)

	copier := NewCopier(Config{
		ContainerID: "input-action-write-error",
		EventBus:    bus,
	})
	defer copier.finishStop(0)
	copier.SetTTYs(errorTTYWriter{err: expectedErr}, nil, nil)

	copier.executeInputActions([]console.Action{
		{Kind: console.ActionWriteTTY, Data: []byte("x")},
	})

	select {
	case event := <-events:
		if event.Type != IOError {
			t.Fatalf("event type = %v, want IOError", event.Type)
		}
		if !errors.Is(event.Err, expectedErr) {
			t.Fatalf("event error = %v, want %v", event.Err, expectedErr)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for IOError")
	}
	if !copier.stopped.Load() {
		t.Fatal("copier should stop after TTY write failure")
	}
}

func TestExecuteInputActionsIgnoresUnsupportedKinds(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "input-action-unsupported"})
	defer copier.finishStop(0)

	copier.executeInputActions([]console.Action{
		{Kind: console.ActionKind(99)},
	})

	if copier.stopped.Load() {
		t.Fatal("unsupported action should not stop copier")
	}
}
