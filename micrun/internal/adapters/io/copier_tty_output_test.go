package io

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	"micrun/internal/domain/console"
)

func TestHandleTTYReadErrorContinuesOnEAGAIN(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "tty-read-eagain"})
	defer copier.finishStop(0)

	if got := copier.handleTTYReadError("TTY stdout", syscall.EAGAIN); got != ttyReadContinue {
		t.Fatalf("handleTTYReadError = %v, want continue", got)
	}
}

func TestHandleTTYReadErrorPublishesIOError(t *testing.T) {
	expectedErr := errors.New("read failed")
	bus := NewEventBus(context.Background())
	defer bus.Close()
	events := bus.Subscribe(IOError)
	copier := NewCopier(Config{ContainerID: "tty-read-error", EventBus: bus})
	defer copier.finishStop(0)

	if got := copier.handleTTYReadError("TTY stdout", expectedErr); got != ttyReadStop {
		t.Fatalf("handleTTYReadError = %v, want stop", got)
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

func TestPublishTTYReadyOncePublishesSingleEvent(t *testing.T) {
	bus := NewEventBus(context.Background())
	defer bus.Close()
	events := bus.Subscribe(TTYReady)
	copier := NewCopier(Config{ContainerID: "tty-ready-once", EventBus: bus})
	defer copier.finishStop(0)

	copier.publishTTYReadyOnce()
	copier.publishTTYReadyOnce()

	select {
	case event := <-events:
		if event.Type != TTYReady || event.ContainerID != "tty-ready-once" {
			t.Fatalf("event = %+v, want TTYReady for tty-ready-once", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for TTYReady event")
	}

	select {
	case event := <-events:
		t.Fatalf("unexpected second TTYReady event: %+v", event)
	default:
	}
}

func TestWaitForTTYReadStopsWhenContextCanceled(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "tty-read-canceled"})
	defer copier.finishStop(0)
	copier.cancel()

	if copier.waitForTTYRead(ttyReadSource{fd: -1}, "test copier") {
		t.Fatal("waitForTTYRead = true, want false after cancellation")
	}
}

func TestOutputWriteCanceledReportsCanceledContext(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "tty-write-canceled"})
	defer copier.finishStop(0)
	copier.cancel()

	if !copier.outputWriteCanceled("test copier") {
		t.Fatal("outputWriteCanceled = false, want true after cancellation")
	}
}

func TestNormalizeTTYOutputAppliesNormalizerAndEchoSuppression(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "tty-normalize", Terminal: true})
	defer copier.finishStop(0)
	normalizer := console.NewOutputNormalizer(console.OutputConfig{FilterNUL: true})
	copier.trackSentCharForEcho('a')

	got := copier.normalizeTTYOutput(normalizer, []byte{'a', 0, 'b'}, true)

	if string(got) != "b" {
		t.Fatalf("normalizeTTYOutput = %q, want %q", string(got), "b")
	}
}
