package container

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"micrun/internal/ports"
	er "micrun/internal/support/errors"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type contextAwareStateStore struct {
	*memoryStateStore
}

func (s *contextAwareStateStore) Load(ctx context.Context, namespace, taskID string) (*ports.RuntimeSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.memoryStateStore.Load(ctx, namespace, taskID)
}

func TestCreateContainerValidatesInputs(t *testing.T) {
	var nilSandbox *Sandbox
	if _, err := nilSandbox.CreateContainer(context.Background(), ContainerConfig{ID: "container1"}); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("nil sandbox CreateContainer error = %v, want SandboxNotFound", err)
	}

	sandbox := &Sandbox{}
	if _, err := sandbox.CreateContainer(context.Background(), ContainerConfig{}); !errors.Is(err, er.EmptyContainerID) {
		t.Fatalf("empty id CreateContainer error = %v, want EmptyContainerID", err)
	}
	if _, err := sandbox.CreateContainer(context.Background(), ContainerConfig{ID: "container1"}); err == nil || !strings.Contains(err.Error(), "sandbox config") {
		t.Fatalf("missing config CreateContainer error = %v, want sandbox config error", err)
	}
}

func TestCreateContainerInitializesConfigMapsBeforeDelegating(t *testing.T) {
	sandbox := &Sandbox{
		id:     "sandbox1",
		ctx:    context.Background(),
		config: &SandboxConfig{},
	}

	_, err := sandbox.CreateContainer(context.Background(), ContainerConfig{ID: "container1"})
	if err == nil {
		t.Fatal("CreateContainer expected downstream dependency error, got nil")
	}
	if sandbox.containers == nil {
		t.Fatal("CreateContainer did not initialize containers map")
	}
	if sandbox.config.ContainerConfigs == nil {
		t.Fatal("CreateContainer did not initialize container config map")
	}
	if _, ok := sandbox.config.ContainerConfigs["container1"]; ok {
		t.Fatal("CreateContainer did not roll back failed config insertion")
	}
}

func TestCreateContainerUsesCallContextForStateRestore(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	store := &contextAwareStateStore{memoryStateStore: newMemoryStateStore()}
	deps := testDepsWithStore(store)
	sandbox := &Sandbox{
		id:         "sandbox1",
		ctx:        canceledCtx,
		stateRepo:  stateRepositoryFromStore(store),
		deps:       deps,
		config:     &SandboxConfig{},
		containers: map[string]*Container{},
	}

	_, err := sandbox.CreateContainer(context.Background(), ContainerConfig{
		ID:        "container1",
		IsInfra:   true,
		Resources: &specs.LinuxResources{},
	})
	if err != nil {
		t.Fatalf("CreateContainer with canceled sandbox context returned error: %v", err)
	}
}

func TestCreateContainerRollsBackAfterPostAddFailure(t *testing.T) {
	firmware := t.TempDir() + "/firmware.elf"
	if err := os.WriteFile(firmware, []byte("firmware"), 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}

	store := newMemoryStateStore()
	deps := testDepsWithStore(store)
	guestCtl := &stubGuestControl{}
	deps.CreateGuest = func(context.Context, GuestClientConfig) error {
		guestCtl.exists = true
		return nil
	}

	sandbox := &Sandbox{
		id:           "sandbox1",
		ctx:          context.Background(),
		stateRepo:    stateRepositoryFromStore(store),
		deps:         deps,
		config:       &SandboxConfig{EnableVCPUsPinning: true},
		containers:   map[string]*Container{},
		guestControl: guestCtl,
	}

	_, err := sandbox.CreateContainer(context.Background(), ContainerConfig{
		ID:           "container1",
		OS:           "uniproton",
		ImageAbsPath: firmware,
		PedestalType: PedestalBaremetal,
		Resources: &specs.LinuxResources{
			CPU: &specs.LinuxCPU{Cpus: "bad"},
		},
	})

	if err == nil || !strings.Contains(err.Error(), "CPUSet") {
		t.Fatalf("CreateContainer error = %v, want CPUSet error", err)
	}
	if _, ok := sandbox.containers["container1"]; ok {
		t.Fatal("CreateContainer did not roll back container map after post-add failure")
	}
	if _, ok := sandbox.config.ContainerConfigs["container1"]; ok {
		t.Fatal("CreateContainer did not roll back config map after post-add failure")
	}
}

type createCleanupGuestControl struct {
	stubGuestControl
	stopErr error
}

func (g *createCleanupGuestControl) Stop(context.Context, string) error {
	return g.stopErr
}

func TestCreateContainerReturnsPostAddCleanupErrors(t *testing.T) {
	firmware := t.TempDir() + "/firmware.elf"
	if err := os.WriteFile(firmware, []byte("firmware"), 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}

	stopErr := errors.New("stop failed")
	store := newMemoryStateStore()
	deps := testDepsWithStore(store)
	guestCtl := &createCleanupGuestControl{stopErr: stopErr}
	deps.CreateGuest = func(context.Context, GuestClientConfig) error {
		guestCtl.exists = true
		return nil
	}

	sandbox := &Sandbox{
		id:           "sandbox1",
		ctx:          context.Background(),
		stateRepo:    stateRepositoryFromStore(store),
		deps:         deps,
		config:       &SandboxConfig{EnableVCPUsPinning: true},
		containers:   map[string]*Container{},
		guestControl: guestCtl,
	}

	_, err := sandbox.CreateContainer(context.Background(), ContainerConfig{
		ID:           "container1",
		OS:           "uniproton",
		ImageAbsPath: firmware,
		PedestalType: PedestalBaremetal,
		Resources: &specs.LinuxResources{
			CPU: &specs.LinuxCPU{Cpus: "bad"},
		},
	})

	if !errors.Is(err, stopErr) {
		t.Fatalf("CreateContainer error = %v, want stop cleanup error", err)
	}
}

func TestDeleteContainerUsesLookupValidation(t *testing.T) {
	var nilSandbox *Sandbox
	if _, err := nilSandbox.DeleteContainer(context.Background(), "container1"); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("nil sandbox DeleteContainer error = %v, want SandboxNotFound", err)
	}

	sandbox := &Sandbox{
		id: "sandbox1",
		containers: map[string]*Container{
			"nil": nil,
		},
	}
	if _, err := sandbox.DeleteContainer(context.Background(), ""); !errors.Is(err, er.EmptyContainerID) {
		t.Fatalf("empty id DeleteContainer error = %v, want EmptyContainerID", err)
	}
	if _, err := sandbox.DeleteContainer(context.Background(), "nil"); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("nil container DeleteContainer error = %v, want ContainerNotFound", err)
	}
}
