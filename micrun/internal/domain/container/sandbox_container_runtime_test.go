package container

import (
	"context"
	"errors"
	"strings"
	"testing"

	er "micrun/internal/support/errors"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func TestNilSandboxContainerRuntimeMethodsReturnNotFound(t *testing.T) {
	var sandbox *Sandbox
	ctx := context.Background()

	if _, err := sandbox.StartContainer(ctx, "container1"); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("StartContainer error = %v, want SandboxNotFound", err)
	}
	if _, err := sandbox.StopContainer(ctx, "container1", false); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("StopContainer error = %v, want SandboxNotFound", err)
	}
	if _, err := sandbox.KillContainer(ctx, "container1"); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("KillContainer error = %v, want SandboxNotFound", err)
	}
	if err := sandbox.PauseContainer(ctx, "container1"); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("PauseContainer error = %v, want SandboxNotFound", err)
	}
	if err := sandbox.ResumeContainer(ctx, "container1"); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("ResumeContainer error = %v, want SandboxNotFound", err)
	}
	if err := sandbox.UpdateContainer(ctx, "container1", specs.LinuxResources{}); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("UpdateContainer error = %v, want SandboxNotFound", err)
	}
}

func TestUpdateContainerReportsMissingSandboxConfig(t *testing.T) {
	sandbox := &Sandbox{}

	err := sandbox.UpdateContainer(context.Background(), "container1", specs.LinuxResources{})

	if err == nil || !strings.Contains(err.Error(), "sandbox config") {
		t.Fatalf("UpdateContainer error = %v, want sandbox config error", err)
	}
}

func TestKillContainerReportsMissingGuestControl(t *testing.T) {
	sandbox := &Sandbox{
		containers: map[string]*Container{
			"container1": {id: "container1"},
		},
	}

	_, err := sandbox.KillContainer(context.Background(), "container1")

	if err == nil || !strings.Contains(err.Error(), "guest control") {
		t.Fatalf("KillContainer error = %v, want guest control error", err)
	}
}
