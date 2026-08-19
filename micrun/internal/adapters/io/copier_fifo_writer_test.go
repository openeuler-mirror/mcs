package io

import (
	"context"
	"errors"
	"io"
	"syscall"
	"testing"
	"time"
)

func TestWriteOutputFIFORetriesEPIPEAfterReaderLeaves(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "fifo-epipe-after-reader", Terminal: false})
	defer copier.finishStop(0, false)

	// start -d closes the first reader; `ctr task attach` reopens the same
	// FIFO without a new Start RPC. Keep waiting instead of stopping.
	fifo := retryOnceWriter{err: syscall.EPIPE}
	if got := copier.writeOutputFIFO("stdout", &fifo, []byte("data")); got != outputWriteContinue {
		t.Fatalf("write decision = %v, want continue after the next reader appears", got)
	}
}

func TestWriteOutputFIFORetriesEPIPEBeforeFirstReader(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "fifo-epipe-before-reader", Terminal: false})
	defer copier.finishStop(0, false)

	// Detached start has a write fd but no attach client yet. EPIPE/ENXIO
	// must wait for a reader, not kill the output worker.
	fifo := retryOnceWriter{err: syscall.EPIPE}
	if got := copier.writeOutputFIFO("stdout", &fifo, []byte("data")); got != outputWriteContinue {
		t.Fatalf("write decision = %v, want continue after first reader appears", got)
	}
}

func TestWriteOutputFIFORetriesENXIOBeforeFirstReader(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "fifo-enxio-before-reader", Terminal: false})
	defer copier.finishStop(0, false)

	fifo := retryOnceWriter{err: syscall.ENXIO}
	if got := copier.writeOutputFIFO("stdout", &fifo, []byte("data")); got != outputWriteContinue {
		t.Fatalf("write decision = %v, want continue after first reader appears", got)
	}
}

func TestWriteOutputFIFORetriesOnEAGAIN(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "fifo-eagain", Terminal: false})
	defer copier.finishStop(0, false)

	// EAGAIN is transient backpressure, not a dead reader: the writer must
	// retry (never drop data, never kill the output worker) and succeed
	// once the backpressure clears.
	writes := 0
	fifo := retryOnceWriter{err: syscall.EAGAIN, after: func() { writes++ }}
	if got := copier.writeOutputFIFO("stdout", &fifo, []byte("data")); got != outputWriteContinue {
		t.Fatalf("write decision = %v, want continue after retry", got)
	}
	if writes != 2 {
		t.Fatalf("write attempts = %d, want 2 (one EAGAIN, one success)", writes)
	}
}

type retryOnceWriter struct {
	err   error
	after func()
}

func (w *retryOnceWriter) Write(p []byte) (int, error) {
	if w.after != nil {
		w.after()
	}
	if w.err != nil {
		err := w.err
		w.err = nil
		return 0, err
	}
	return len(p), nil
}

func TestOutputWriteDecisionString(t *testing.T) {
	cases := map[outputWriteDecision]string{
		outputWriteStop:         "stop",
		outputWriteContinue:     "continue",
		outputWriteRetry:        "retry",
		outputWriteDecision(99): "unknown",
	}
	for decision, want := range cases {
		if got := decision.String(); got != want {
			t.Fatalf("decision %d String() = %q, want %q", decision, got, want)
		}
	}
}

func TestWriteOutputFIFORetriesEPIPEInTerminalMode(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()
	events := bus.Subscribe(StdinClosed)
	copier := NewCopier(Config{ContainerID: "fifo-terminal", Terminal: true, EventBus: bus})
	defer copier.finishStop(0, false)

	fifo := retryOnceWriter{err: syscall.EPIPE}
	if got := copier.writeOutputFIFO("stdout", &fifo, []byte("data")); got != outputWriteContinue {
		t.Fatalf("write decision = %v, want continue after the next reader appears", got)
	}

	select {
	case event := <-events:
		t.Fatalf("unexpected StdinClosed event for stdout EPIPE: %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWriteOutputFIFOTreatsClosedWriterAsDisconnect(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()
	events := bus.Subscribe(StdinClosed)
	copier := NewCopier(Config{ContainerID: "fifo-closed-writer", Terminal: true, EventBus: bus})
	defer copier.finishStop(0, false)

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
	defer copier.finishStop(0, false)

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
	defer copier.finishStop(0, false)

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
	defer copier.finishStop(0, false)

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

func TestWriteOutputFIFORetriesOnEAGAINInTerminalMode(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()
	events := bus.Subscribe(IOError)
	copier := NewCopier(Config{ContainerID: "fifo-eagain-terminal", Terminal: true, EventBus: bus})
	defer copier.finishStop(0, false)

	// Terminal mode must also retry on EAGAIN: it is backpressure, not a
	// dead reader — stopping the output worker would leave the session
	// half-dead with no recovery path. No IOError may be published for a
	// transient EAGAIN.
	if got := copier.writeOutputFIFO("stdout", &retryOnceWriter{err: syscall.EAGAIN}, []byte("data")); got != outputWriteContinue {
		t.Fatalf("write decision = %v, want continue after retry", got)
	}

	select {
	case event := <-events:
		t.Fatalf("unexpected IOError event for EAGAIN: %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}
