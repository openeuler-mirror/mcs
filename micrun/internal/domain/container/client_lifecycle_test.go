package container

import (
	"context"
	"errors"
	"strings"
	"testing"

	er "micrun/internal/support/errors"
)

func TestStartClientValidatesDependencies(t *testing.T) {
	ctx := context.Background()

	if err := startClient(ctx, nil); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("startClient nil error = %v, want ContainerNotFound", err)
	}
	if err := startClient(ctx, &Container{}); err == nil || !strings.Contains(err.Error(), "container config") {
		t.Fatalf("startClient missing config error = %v, want config error", err)
	}

	container := &Container{
		id:      "container1",
		config:  &ContainerConfig{ID: "container1"},
		sandbox: &Sandbox{},
	}
	if err := startClient(ctx, container); err == nil || !strings.Contains(err.Error(), "guest control") {
		t.Fatalf("startClient missing guest control error = %v, want guest control error", err)
	}

	container.sandbox.guestControl = &runtimeControlGuest{}
	if err := startClient(ctx, container); err == nil || !strings.Contains(err.Error(), "guest executor") {
		t.Fatalf("startClient missing guest executor error = %v, want guest executor error", err)
	}
}

func TestApplyInitialCPUSettingsValidatesDependencies(t *testing.T) {
	ctx := context.Background()
	container := &Container{
		id:     "container1",
		config: &ContainerConfig{ID: "container1"},
	}

	if err := container.applyInitialCPUSettings(ctx); !errors.Is(err, er.ContainerSandboxNil) {
		t.Fatalf("applyInitialCPUSettings missing sandbox error = %v, want ContainerSandboxNil", err)
	}

	container.sandbox = &Sandbox{}
	if err := container.applyInitialCPUSettings(ctx); err == nil || !strings.Contains(err.Error(), "guest executor") {
		t.Fatalf("applyInitialCPUSettings missing guest executor error = %v, want guest executor error", err)
	}
}

func TestCreateMicaClientConfValidatesContainer(t *testing.T) {
	if _, err := createMicaClientConf(nil); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("createMicaClientConf nil error = %v, want ContainerNotFound", err)
	}
	if _, err := createMicaClientConf(&Container{}); err == nil || !strings.Contains(err.Error(), "container config") {
		t.Fatalf("createMicaClientConf missing config error = %v, want config error", err)
	}
}
