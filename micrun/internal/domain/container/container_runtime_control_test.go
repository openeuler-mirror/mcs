package container

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"micrun/internal/ports"
	er "micrun/internal/support/errors"
)

type runtimeControlGuest struct {
	startErr  error
	stopErr   error
	removeErr error
	existsCtx context.Context
	removeCtx context.Context
}

func (g *runtimeControlGuest) Start(context.Context, string) error { return g.startErr }
func (g *runtimeControlGuest) Stop(context.Context, string) error  { return g.stopErr }
func (g *runtimeControlGuest) Remove(ctx context.Context, _ string) error {
	g.removeCtx = ctx
	return g.removeErr
}
func (g *runtimeControlGuest) Pause(context.Context, string) error  { return nil }
func (g *runtimeControlGuest) Resume(context.Context, string) error { return nil }
func (g *runtimeControlGuest) Exists(ctx context.Context, _ string) (bool, error) {
	g.existsCtx = ctx
	return true, nil
}
func (g *runtimeControlGuest) Status(context.Context, string) (ports.GuestStatus, error) {
	return ports.GuestStatus{Running: true}, nil
}

func TestRuntimeControlRequiresGuestControl(t *testing.T) {
	ctx := context.Background()
	sandbox := &Sandbox{
		ctx:   ctx,
		state: SandboxState{State: StateRunning},
	}
	container := &Container{
		ctx:       ctx,
		id:        "container1",
		config:    &ContainerConfig{ID: "container1"},
		guestExec: recordingGuestExecutor{},
		sandbox:   sandbox,
		state:     ContainerState{State: StateReady},
	}

	checks := []struct {
		name string
		fn   func() error
	}{
		{"startGuest", func() error { return container.startGuest(ctx, StateReady) }},
		{"doStop", func() error { return container.doStop(ctx, false) }},
		{"kill", func() error { return container.kill(ctx) }},
		{"delete", func() error { return container.delete(ctx) }},
		{"pause", func() error {
			container.state.State = StateRunning
			return container.pause(ctx)
		}},
		{"resume", func() error {
			container.state.State = StatePaused
			return container.resume(ctx)
		}},
		{"signal", func() error {
			container.state.State = StateReady
			return container.Signal(ctx, 0)
		}},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.fn()
			if err == nil || !strings.Contains(err.Error(), "guest control") {
				t.Fatalf("%s error = %v, want guest control error", check.name, err)
			}
		})
	}
}

func TestRuntimeControlUsesOperationContextForPresenceChecks(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	guest := &runtimeControlGuest{}
	sandbox := &Sandbox{
		ctx:          context.Background(),
		state:        SandboxState{State: StateRunning},
		guestControl: guest,
	}
	container := &Container{
		ctx:       context.Background(),
		id:        "container-context",
		config:    &ContainerConfig{ID: "container-context"},
		guestExec: recordingGuestExecutor{},
		sandbox:   sandbox,
		state:     ContainerState{State: StateRunning},
	}

	if err := container.Signal(ctx, 0); err != nil {
		t.Fatalf("Signal returned error: %v", err)
	}
	if guest.existsCtx != ctx {
		t.Fatal("presence check did not receive operation context")
	}
}

func TestRuntimeControlUsesOperationContextForStateChecks(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	guest := &runtimeControlGuest{}
	sandbox := &Sandbox{
		ctx:          context.Background(),
		state:        SandboxState{State: StateRunning},
		guestControl: guest,
	}
	container := &Container{
		ctx:       context.Background(),
		id:        "container-state-context",
		config:    &ContainerConfig{ID: "container-state-context"},
		guestExec: recordingGuestExecutor{},
		sandbox:   sandbox,
		state:     ContainerState{State: StateRunning},
	}

	if err := container.doStop(ctx, false); err != nil {
		t.Fatalf("doStop returned error: %v", err)
	}
	if guest.existsCtx != ctx {
		t.Fatal("state check did not receive operation context")
	}
}

func TestForcedStopTreatsConnectionResetAsAlreadyExiting(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStateStore()
	guest := &runtimeControlGuest{
		stopErr: fmt.Errorf("stop race: %w", syscall.ECONNRESET),
	}
	sandbox := &Sandbox{
		ctx:          ctx,
		id:           "sandbox-stop-race",
		config:       &SandboxConfig{ID: "sandbox-stop-race"},
		containers:   map[string]*Container{},
		state:        SandboxState{State: StateRunning},
		stateRepo:    stateRepositoryFromStore(store),
		deps:         testDepsWithStore(store),
		guestControl: guest,
	}
	container := &Container{
		ctx:           ctx,
		id:            "container-stop-race",
		config:        &ContainerConfig{ID: "container-stop-race"},
		guestExec:     recordingGuestExecutor{},
		sandbox:       sandbox,
		state:         ContainerState{State: StateRunning},
		containerPath: "sandbox-stop-race/container-stop-race",
	}
	sandbox.containers[container.id] = container

	err := container.stop(ctx, true)

	if err != nil {
		t.Fatalf("forced stop returned error: %v", err)
	}
	if container.state.State != StateStopped {
		t.Fatalf("container state = %s, want %s", container.state.State, StateStopped)
	}
}

func TestCleanupAfterDeleteUsesConfiguredCacheRoot(t *testing.T) {
	ctx := context.Background()
	cacheRoot := t.TempDir()
	cacheDir := filepath.Join(cacheRoot, "container-cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("create cache dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "artifact"), []byte("cache"), 0o600); err != nil {
		t.Fatalf("write cache artifact: %v", err)
	}

	store := newMemoryStateStore()
	sandbox := &Sandbox{
		ctx:       ctx,
		id:        "sandbox-cache",
		stateRepo: stateRepositoryFromStore(store),
		config: &SandboxConfig{
			ID:               "sandbox-cache",
			ContainerConfigs: map[string]*ContainerConfig{},
		},
		containers: map[string]*Container{},
		state:      SandboxState{State: StateRunning},
	}
	container := &Container{
		ctx:           ctx,
		id:            "container-cache",
		config:        &ContainerConfig{ID: "container-cache", CacheRoot: cacheRoot},
		sandbox:       sandbox,
		containerPath: filepath.Join(sandbox.id, "container-cache"),
		state:         ContainerState{State: StateStopped},
	}
	sandbox.containers[container.id] = container

	if err := container.cleanupAfterDelete(ctx); err != nil {
		t.Fatalf("cleanupAfterDelete returned error: %v", err)
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatalf("expected configured cache dir to be removed, stat err: %v", err)
	}
}

func TestCleanupAfterDeleteDeletesStateWhenSandboxStoreFails(t *testing.T) {
	ctx := context.Background()
	saveErr := errors.New("save sandbox failed")
	store := newMemoryStateStore()
	store.saveErr = saveErr

	sandbox := &Sandbox{
		ctx:       ctx,
		id:        "sandbox-cleanup",
		stateRepo: stateRepositoryFromStore(store),
		config: &SandboxConfig{
			ID: "sandbox-cleanup",
			ContainerConfigs: map[string]*ContainerConfig{
				"container-cleanup": &ContainerConfig{ID: "container-cleanup"},
			},
		},
		containers: map[string]*Container{},
		state:      SandboxState{State: StateRunning},
	}
	container := &Container{
		ctx:           ctx,
		id:            "container-cleanup",
		config:        &ContainerConfig{ID: "container-cleanup", CacheRoot: t.TempDir()},
		sandbox:       sandbox,
		containerPath: filepath.Join(sandbox.id, "container-cleanup"),
		state:         ContainerState{State: StateStopped},
	}
	sandbox.containers[container.id] = container

	err := container.cleanupAfterDelete(ctx)
	if !errors.Is(err, saveErr) {
		t.Fatalf("cleanupAfterDelete error = %v, want saveErr", err)
	}

	wantDeleted := runtimeStateNamespaceContainer + "/" + filepath.Join(sandbox.id, container.id)
	if len(store.deleted) != 1 || store.deleted[0] != wantDeleted {
		t.Fatalf("deleted snapshots = %v, want %q", store.deleted, wantDeleted)
	}
}

func TestDeleteStoppedContainerTreatsGuestRemoveAsBestEffort(t *testing.T) {
	ctx := context.Background()
	guest := &runtimeControlGuest{removeErr: errors.New("guest already shut down")}
	store := newMemoryStateStore()
	sandbox := &Sandbox{
		ctx:          ctx,
		id:           "sandbox-delete-stopped",
		stateRepo:    stateRepositoryFromStore(store),
		guestControl: guest,
		config: &SandboxConfig{
			ID: "sandbox-delete-stopped",
			ContainerConfigs: map[string]*ContainerConfig{
				"container-delete-stopped": &ContainerConfig{ID: "container-delete-stopped"},
			},
		},
		containers: map[string]*Container{},
		state:      SandboxState{State: StateRunning},
	}
	container := &Container{
		ctx:           ctx,
		id:            "container-delete-stopped",
		config:        &ContainerConfig{ID: "container-delete-stopped", CacheRoot: t.TempDir()},
		sandbox:       sandbox,
		containerPath: filepath.Join(sandbox.id, "container-delete-stopped"),
		state:         ContainerState{State: StateStopped},
	}
	sandbox.containers[container.id] = container

	if err := container.delete(ctx); err != nil {
		t.Fatalf("delete returned error: %v", err)
	}
	if _, ok := sandbox.containers[container.id]; ok {
		t.Fatal("container remained in sandbox after delete")
	}
	if _, ok := guest.removeCtx.Deadline(); !ok {
		t.Fatal("stopped container guest remove did not use a bounded context")
	}
}

func TestDeleteReadyContainerPropagatesGuestRemoveError(t *testing.T) {
	ctx := context.Background()
	removeErr := errors.New("remove failed")
	guest := &runtimeControlGuest{removeErr: removeErr}
	store := newMemoryStateStore()
	sandbox := &Sandbox{
		ctx:          ctx,
		id:           "sandbox-delete-ready",
		stateRepo:    stateRepositoryFromStore(store),
		guestControl: guest,
		config: &SandboxConfig{
			ID: "sandbox-delete-ready",
			ContainerConfigs: map[string]*ContainerConfig{
				"container-delete-ready": &ContainerConfig{ID: "container-delete-ready"},
			},
		},
		containers: map[string]*Container{},
		state:      SandboxState{State: StateRunning},
	}
	container := &Container{
		ctx:           ctx,
		id:            "container-delete-ready",
		config:        &ContainerConfig{ID: "container-delete-ready", CacheRoot: t.TempDir()},
		sandbox:       sandbox,
		containerPath: filepath.Join(sandbox.id, "container-delete-ready"),
		state:         ContainerState{State: StateReady},
	}
	sandbox.containers[container.id] = container

	err := container.delete(ctx)
	if !errors.Is(err, removeErr) {
		t.Fatalf("delete error = %v, want removeErr", err)
	}
	if _, ok := sandbox.containers[container.id]; !ok {
		t.Fatal("ready container was removed from sandbox after failed guest remove")
	}
}

func TestRequireGuestControlReturnsContainerNotFoundForNilContainer(t *testing.T) {
	var container *Container
	if err := container.requireGuestControl(); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("requireGuestControl error = %v, want ContainerNotFound", err)
	}
}

func TestStartGuestReturnsStopCleanupError(t *testing.T) {
	ctx := context.Background()
	startErr := errors.New("start failed")
	stopErr := errors.New("stop failed")
	sandbox := &Sandbox{
		ctx:          ctx,
		state:        SandboxState{State: StateRunning},
		guestControl: &runtimeControlGuest{startErr: startErr, stopErr: stopErr},
	}
	container := &Container{
		ctx:       ctx,
		id:        "container1",
		config:    &ContainerConfig{ID: "container1"},
		guestExec: recordingGuestExecutor{},
		sandbox:   sandbox,
		state:     ContainerState{State: StateReady},
	}

	err := container.startGuest(ctx, StateReady)

	if !errors.Is(err, startErr) {
		t.Fatalf("startGuest error = %v, want start error", err)
	}
	if !errors.Is(err, stopErr) {
		t.Fatalf("startGuest error = %v, want stop cleanup error", err)
	}
}
