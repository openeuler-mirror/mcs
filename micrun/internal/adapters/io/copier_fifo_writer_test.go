package io

import (
	"context"
	"errors"
	"io"
	"syscall"
	"testing"
	"time"
)

func TestWriteOutputFIFOContinuesForDetachedNonTerminal(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "fifo-non-terminal", Terminal: false})
	defer copier.finishStop(0)

	if got := copier.writeOutputFIFO("stdout", writeError{err: syscall.EPIPE}, []byte("data")); got != outputWriteContinue {
		t.Fatalf("write decision = %v, want continue", got)
	}
}

func TestWriteOutputFIFOContinuesForDetachedEAGAIN(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "fifo-eagain-non-terminal", Terminal: false})
	defer copier.finishStop(0)

	if got := copier.writeOutputFIFO("stdout", writeError{err: syscall.EAGAIN}, []byte("data")); got != outputWriteContinue {
		t.Fatalf("write decision = %v, want continue", got)
	}
}

func TestOutputWriteDecisionString(t *testing.T) {
	cases := map[outputWriteDecision]string{
		outputWriteStop:         "stop",
		outputWriteContinue:     "continue",
		outputWriteDecision(99): "unknown",
	}
	for decision, want := range cases {
		if got := decision.String(); got != want {
			t.Fatalf("decision %d String() = %q, want %q", decision, got, want)
		}
	}
}

func TestWriteOutputFIFOPublishesStdinClosedForTerminal(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()
	events := bus.Subscribe(StdinClosed)
	copier := NewCopier(Config{ContainerID: "fifo-terminal", Terminal: true, EventBus: bus})
	defer copier.finishStop(0)

	if got := copier.writeOutputFIFO("stdout", writeError{err: syscall.EPIPE}, []byte("data")); got != outputWriteStop {
		t.Fatalf("write decision = %v, want stop", got)
	}

	select {
	case event := <-events:
		if event.Type != StdinClosed || event.ContainerID != "fifo-terminal" {
			t.Fatalf("event = %+v, want StdinClosed for fifo-terminal", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for StdinClosed event")
	}
}

func TestWriteOutputFIFOTreatsClosedWriterAsDisconnect(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()
	events := bus.Subscribe(StdinClosed)
	copier := NewCopier(Config{ContainerID: "fifo-closed-writer", Terminal: true, EventBus: bus})
	defer copier.finishStop(0)

	if got := copier.writeOutputFIFO("stdout", writeError{err: io.ErrClosedPipe}, []byte("data")); got != outputWriteStop {
		t.Fatalf("write decision = %v, want stop", got)
	}

	assertIOEvent(t, events, StdinClosed)
}

func TestWriteOutputFIFONilWriterPublishesIOError(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()
	events := bus.Subscribe(IOError)
	copier := NewCopier(Config{ContainerID: "fifo-nil", Terminal: true, EventBus: bus})
	defer copier.finishStop(0)

	if got := copier.writeOutputFIFO("stdout", nil, []byte("data")); got != outputWriteStop {
		t.Fatalf("write decision = %v, want stop", got)
	}

	select {
	case event := <-events:
		if event.Type != IOError || !errors.Is(event.Err, io.ErrClosedPipe) {
			t.Fatalf("event = %+v, want IOError wrapping closed pipe", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for IOError event")
	}
}

func TestWriteOutputFIFOPublishesIOError(t *testing.T) {
	expectedErr := errors.New("write failed")
	bus := NewEventBus(context.Background())
	defer bus.Close()
	events := bus.Subscribe(IOError)
	copier := NewCopier(Config{ContainerID: "fifo-error", EventBus: bus})
	defer copier.finishStop(0)

	if got := copier.writeOutputFIFO("stderr", writeError{err: expectedErr}, []byte("data")); got != outputWriteStop {
		t.Fatalf("write decision = %v, want stop", got)
	}

	select {
	case event := <-events:
		if event.Type != IOError || !errors.Is(event.Err, expectedErr) {
			t.Fatalf("event = %+v, want IOError wrapping expected error", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for IOError event")
	}
}

func assertIOEvent(t *testing.T, ch EventSubscriber, want EventType) {
	t.Helper()
	select {
	case event := <-ch:
		if event.Type != want {
			t.Fatalf("event type = %v, want %v", event.Type, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for event %v", want)
	}
}

func TestWriteOutputFIFOPublishesIOErrorForShortWrite(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()
	events := bus.Subscribe(IOError)
	copier := NewCopier(Config{ContainerID: "fifo-short-write", EventBus: bus})
	defer copier.finishStop(0)

	if got := copier.writeOutputFIFO("stdout", shortWriter{}, []byte("data")); got != outputWriteStop {
		t.Fatalf("write decision = %v, want stop", got)
	}

	select {
	case event := <-events:
		if event.Type != IOError || !errors.Is(event.Err, io.ErrShortWrite) {
			t.Fatalf("event = %+v, want IOError wrapping short write", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for IOError event")
	}
}

func TestWriteOutputFIFOPublishesIOErrorForTerminalEAGAIN(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()
	events := bus.Subscribe(IOError)
	copier := NewCopier(Config{ContainerID: "fifo-eagain-terminal", Terminal: true, EventBus: bus})
	defer copier.finishStop(0)

	if got := copier.writeOutputFIFO("stdout", writeError{err: syscall.EAGAIN}, []byte("data")); got != outputWriteStop {
		t.Fatalf("write decision = %v, want stop", got)
	}

	select {
	case event := <-events:
		if event.Type != IOError || !errors.Is(event.Err, syscall.EAGAIN) {
			t.Fatalf("event = %+v, want IOError wrapping EAGAIN", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for IOError event")
	}
}
