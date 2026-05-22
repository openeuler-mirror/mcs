package libmica

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStartWrapsUnderlyingMicaCtlError(t *testing.T) {
	original := micaCtlFn
	t.Cleanup(func() {
		micaCtlFn = original
	})

	expectedErr := errors.New("micad refused start")
	micaCtlFn = func(context.Context, MicaCommand, string, ...string) error {
		return expectedErr
	}

	err := Start("demo")
	if err == nil {
		t.Fatal("Start() expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Start() error chain does not contain the original error: %v", err)
	}
	if !strings.Contains(err.Error(), "failed to start container demo") {
		t.Fatalf("Start() error = %q, want container context", err)
	}
}

func TestStartContextPropagatesContextToMicaCtl(t *testing.T) {
	original := micaCtlFn
	t.Cleanup(func() {
		micaCtlFn = original
	})

	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	var got context.Context
	micaCtlFn = func(ctx context.Context, _ MicaCommand, _ string, _ ...string) error {
		got = ctx
		return nil
	}

	if err := StartContext(ctx, "demo"); err != nil {
		t.Fatalf("StartContext returned error: %v", err)
	}
	if got != ctx {
		t.Fatal("StartContext did not propagate caller context")
	}
}
