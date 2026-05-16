package io

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewBinaryIONilURI(t *testing.T) {
	_, err := NewBinaryIO(context.Background(), "container-binary", nil)
	if err == nil {
		t.Fatal("expected error for nil URI")
	}
	if !strings.Contains(err.Error(), "binary URI is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewBinaryIORejectsNonBinaryScheme(t *testing.T) {
	_, err := NewBinaryIO(context.Background(), "container-binary", &url.URL{
		Scheme: "fd",
		Path:   "/tmp/binary",
	})
	if err == nil {
		t.Fatal("expected error for non-binary URI")
	}
	if !strings.Contains(err.Error(), "invalid URI scheme") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewBinaryIORejectsEmptyPath(t *testing.T) {
	_, err := NewBinaryIO(context.Background(), "container-binary", &url.URL{
		Scheme: "binary",
		Path:   "",
	})
	if err == nil {
		t.Fatal("expected error for empty binary path")
	}
	if !strings.Contains(err.Error(), "empty binary path in URI") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewBinaryIORejectsCanceledContextBeforeStartingProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewBinaryIO(ctx, "container-binary", &url.URL{
		Scheme: "binary",
		Path:   "/does/not/need/to/exist",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("NewBinaryIO error = %v, want context.Canceled", err)
	}
}

func TestNewBinaryIOAcceptsNilContext(t *testing.T) {
	_, err := NewBinaryIO(nil, "container-binary", &url.URL{
		Scheme: "binary",
		Path:   "/does/not/exist",
	})
	if err == nil {
		t.Fatal("expected process start error for missing binary")
	}
	if !strings.Contains(err.Error(), "failed to start binary process") {
		t.Fatalf("NewBinaryIO error = %v, want process start error", err)
	}
}

func TestBinaryIOCloseIsIdempotent(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "binary-io-helper-close-idempotent.sh")
	script := `#!/bin/sh
sleep 2
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write helper: %v", err)
	}

	binaryIO, err := NewBinaryIO(context.Background(), "container-binary", &url.URL{
		Scheme: "binary",
		Path:   scriptPath,
	})
	if err != nil {
		t.Fatalf("NewBinaryIO returned error: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(3)
	errs := make(chan error, 3)
	go func() {
		defer wg.Done()
		errs <- binaryIO.Close()
	}()
	go func() {
		defer wg.Done()
		errs <- binaryIO.Close()
	}()
	go func() {
		defer wg.Done()
		errs <- binaryIO.Close()
	}()
	wg.Wait()

	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Close returned unexpected error: %v", err)
		}
	}
}

func TestBinaryIOKeepsContainerStdoutWriterOpen(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "binary-io-helper.sh")
	script := `#!/bin/sh
cat <&3 >/dev/null &
cat <&4 >/dev/null &
sleep 2
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write helper: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	binaryIO, err := NewBinaryIO(ctx, "container-binary", &url.URL{
		Scheme: "binary",
		Path:   scriptPath,
	})
	if err != nil {
		t.Fatalf("NewBinaryIO returned error: %v", err)
	}
	defer binaryIO.Close()

	if _, err := binaryIO.Stdout().Write([]byte("hello\n")); err != nil {
		t.Fatalf("Stdout writer should stay open after start: %v", err)
	}
}

func TestClosePipeIgnoresClosedError(t *testing.T) {
	binaryIO := &BinaryIO{container: "container-binary"}

	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}

	binaryIO.stdinR = readPipe
	if err := readPipe.Close(); err != nil {
		t.Fatalf("close read pipe: %v", err)
	}

	if errs := binaryIO.closePipe("stdin read pipe", &binaryIO.stdinR); len(errs) != 0 {
		t.Fatalf("closePipe returned errors for already closed fd: %v", errs)
	}
	if binaryIO.stdinR != nil {
		t.Fatalf("closePipe should reset pipe handle")
	}

	writePipe.Close()
}

func TestBinaryEnvExpandsQueryParams(t *testing.T) {
	env := binaryEnv([]string{"BASE=1"}, url.Values{
		"MODE":    {"first", "last"},
		"LOG":     {"/tmp/micrun.log"},
		"":        {"ignored"},
		"BAD=KEY": {"ignored"},
	})

	want := []string{"BASE=1", "LOG=/tmp/micrun.log", "MODE=last"}
	if len(env) != len(want) {
		t.Fatalf("env = %v, want %v", env, want)
	}
	for i := range want {
		if env[i] != want[i] {
			t.Fatalf("env = %v, want %v", env, want)
		}
	}
}

func TestBinaryEnvOverridesExistingKeys(t *testing.T) {
	env := binaryEnv([]string{"BASE=1", "MODE=old"}, url.Values{
		"MODE": {"new"},
		"LOG":  {"/tmp/micrun.log"},
	})

	want := []string{"BASE=1", "MODE=new", "LOG=/tmp/micrun.log"}
	if len(env) != len(want) {
		t.Fatalf("env = %v, want %v", env, want)
	}
	for i := range want {
		if env[i] != want[i] {
			t.Fatalf("env = %v, want %v", env, want)
		}
	}
}
