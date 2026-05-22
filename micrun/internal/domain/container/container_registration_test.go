package container

import (
	"context"
	"errors"
	"os"
	"testing"

	er "micrun/internal/support/errors"
)

func TestEnsureClientPresenceValidatesContainerAndGuestControl(t *testing.T) {
	var nilContainer *Container
	if _, err := nilContainer.ensureClientPresence(); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("nil ensureClientPresence error = %v, want ContainerNotFound", err)
	}

	container := &Container{
		ctx:     context.Background(),
		id:      "container1",
		config:  &ContainerConfig{ID: "container1"},
		sandbox: &Sandbox{},
		state:   ContainerState{State: StateDown},
	}
	if _, err := container.ensureClientPresence(); err == nil || err.Error() != "guest control is nil" {
		t.Fatalf("missing guest control ensureClientPresence error = %v, want guest control error", err)
	}
}

func TestEnsureClientPresenceReturnsGuestExistsError(t *testing.T) {
	expected := errors.New("exists failed")
	container := &Container{
		ctx:    context.Background(),
		id:     "container1",
		config: &ContainerConfig{ID: "container1", OS: "uniproton"},
		sandbox: &Sandbox{
			guestControl: &stubGuestControl{existsErr: expected},
		},
		state: ContainerState{State: StateDown},
	}

	if _, err := container.ensureClientPresence(); !errors.Is(err, expected) {
		t.Fatalf("ensureClientPresence error = %v, want %v", err, expected)
	}
}

func TestRegisterClientRequiresGuestExecutorBeforeCreateGuest(t *testing.T) {
	firmware := t.TempDir() + "/firmware.elf"
	if err := os.WriteFile(firmware, []byte("firmware"), 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}

	createGuestCalled := false
	deps := testDepsWithStore(newMemoryStateStore())
	deps.CreateGuest = func(context.Context, GuestClientConfig) error {
		createGuestCalled = true
		return nil
	}
	container := &Container{
		ctx: context.Background(),
		id:  "container1",
		config: &ContainerConfig{
			ID:           "container1",
			OS:           "uniproton",
			ImageAbsPath: firmware,
		},
		sandbox: &Sandbox{deps: deps},
	}

	err := container.registerClient(context.Background())

	if !errors.Is(err, er.FactoryNotConfigured) {
		t.Fatalf("registerClient error = %v, want FactoryNotConfigured", err)
	}
	if createGuestCalled {
		t.Fatal("registerClient called CreateGuest before validating guest executor")
	}
}

func TestRegisterClientUsesOperationContextForCreateGuest(t *testing.T) {
	firmware := t.TempDir() + "/firmware.elf"
	if err := os.WriteFile(firmware, []byte("firmware"), 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}

	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	expectedErr := errors.New("create failed")
	var createGuestCtx context.Context
	deps := testDepsWithStore(newMemoryStateStore())
	deps.CreateGuest = func(ctx context.Context, _ GuestClientConfig) error {
		createGuestCtx = ctx
		return expectedErr
	}
	container := &Container{
		ctx:       context.Background(),
		id:        "container1",
		guestExec: recordingGuestExecutor{},
		config: &ContainerConfig{
			ID:           "container1",
			OS:           "uniproton",
			ImageAbsPath: firmware,
		},
		sandbox: &Sandbox{deps: deps},
	}

	err := container.registerClient(ctx)

	if !errors.Is(err, expectedErr) {
		t.Fatalf("registerClient error = %v, want %v", err, expectedErr)
	}
	if createGuestCtx != ctx {
		t.Fatal("CreateGuest did not receive operation context")
	}
}
