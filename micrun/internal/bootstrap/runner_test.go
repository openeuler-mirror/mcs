package bootstrap

import (
	"bytes"
	"context"
	"testing"

	log "micrun/internal/support/logger"

	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

var stubInit shimv2.Init = func(context.Context, string, shimv2.Publisher, func()) (shimv2.Shim, error) {
	return nil, nil
}

func TestRunHandlesEarlyCommandWithoutStartingShim(t *testing.T) {
	var out bytes.Buffer
	started := false

	code := Run(Config{
		ShimName: "io.containerd.mica.v2",
		Args:     []string{"micrun", "--version"},
		Stdout:   &out,
		RunHolderCommand: func([]string) bool {
			return false
		},
		RunShim: func(string, shimv2.Init, ...shimv2.BinaryOpts) {
			started = true
		},
	})

	if code != 0 {
		t.Fatalf("Run() = %d, want 0", code)
	}
	if started {
		t.Fatal("shim was started for early command")
	}
	if out.Len() == 0 {
		t.Fatal("version output is empty")
	}
}

func TestRunDelegatesHolderCommandBeforeEarlyCommand(t *testing.T) {
	started := false
	var out bytes.Buffer

	code := Run(Config{
		ShimName: "io.containerd.mica.v2",
		Args:     []string{"micrun", "--version"},
		Stdout:   &out,
		RunHolderCommand: func([]string) bool {
			return true
		},
		RunShim: func(string, shimv2.Init, ...shimv2.BinaryOpts) {
			started = true
		},
	})

	if code != 0 {
		t.Fatalf("Run() = %d, want 0", code)
	}
	if started {
		t.Fatal("shim was started after holder command")
	}
	if out.Len() != 0 {
		t.Fatalf("early command output = %q, want empty", out.String())
	}
}

func TestRunStartsShimWithMicrunBinaryOptions(t *testing.T) {
	t.Setenv(log.ContainerdLogPathEnv, t.TempDir()+"/missing-fifo")
	oldNamespace := log.GetNamespace()
	oldContainerID := log.GetContainerID()
	defer func() {
		log.SetNamespace(oldNamespace)
		log.SetContainerID(oldContainerID)
	}()

	started := false
	var gotName string
	gotConfig := shimv2.Config{}

	code := Run(Config{
		ShimName: "io.containerd.mica.v2",
		Args:     []string{"micrun", "-namespace", "k8s.io", "-id", "pod-1"},
		Stderr:   &bytes.Buffer{},
		Init:     stubInit,
		RunHolderCommand: func([]string) bool {
			return false
		},
		RunShim: func(name string, _ shimv2.Init, opts ...shimv2.BinaryOpts) {
			started = true
			gotName = name
			for _, opt := range opts {
				opt(&gotConfig)
			}
		},
	})

	if code != 0 {
		t.Fatalf("Run() = %d, want 0", code)
	}
	if !started {
		t.Fatal("shim was not started")
	}
	if gotName != "io.containerd.mica.v2" {
		t.Fatalf("shim name = %q, want io.containerd.mica.v2", gotName)
	}
	if !gotConfig.NoReaper {
		t.Fatal("NoReaper = false, want true")
	}
	if !gotConfig.NoSubreaper {
		t.Fatal("NoSubreaper = false, want true")
	}
	if !gotConfig.NoSetupLogger {
		t.Fatal("NoSetupLogger = false, want true")
	}
	if got := log.GetNamespace(); got != "k8s.io" {
		t.Fatalf("namespace = %q, want k8s.io", got)
	}
	if got := log.GetContainerID(); got != "pod-1" {
		t.Fatalf("container id = %q, want pod-1", got)
	}
}

func TestRunRejectsMissingShimInit(t *testing.T) {
	started := false
	var stderr bytes.Buffer

	code := Run(Config{
		ShimName: "io.containerd.mica.v2",
		Args:     []string{"micrun", "-namespace", "k8s.io", "-id", "pod-1"},
		Stderr:   &stderr,
		RunHolderCommand: func([]string) bool {
			return false
		},
		RunShim: func(string, shimv2.Init, ...shimv2.BinaryOpts) {
			started = true
		},
	})

	if code != 2 {
		t.Fatalf("Run() = %d, want 2", code)
	}
	if started {
		t.Fatal("shim was started without init")
	}
	if got := stderr.String(); got == "" {
		t.Fatal("expected missing init error on stderr")
	}
}
