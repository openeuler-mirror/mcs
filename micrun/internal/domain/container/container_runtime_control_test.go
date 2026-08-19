package container

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"micrun/internal/ports"
	er "micrun/internal/support/errors"
)

type runtimeControlGuest struct {
	startErr    error
	stopErr     error
	removeErr   error
	exists      *bool // nil → true (historical default for most tests)
	existsCtx   context.Context
	removeCtx   context.Context
	stopCalls   int
	removeCalls int
	// startEntered is closed (once) when Start is invoked; startGate, when
	// non-nil, blocks Start until closed — used to park Start mid-sequence.
	startEntered chan struct{}
	startGate    chan struct{}
	startOnce    sync.Once
}

func (g *runtimeControlGuest) Start(context.Context, string) error {
	if g.startEntered != nil {
		g.startOnce.Do(func() { close(g.startEntered) })
	}
	if g.startGate != nil {
		<-g.startGate
	}
	return g.startErr
}
func (g *runtimeControlGuest) Stop(context.Context, string) error {
	g.stopCalls++
	return g.stopErr
}
func (g *runtimeControlGuest) Remove(ctx context.Context, _ string) error {
	g.removeCalls++
	g.removeCtx = ctx
	return g.removeErr
}
func (g *runtimeControlGuest) Pause(context.Context, string) error  { return nil }
func (g *runtimeControlGuest) Resume(context.Context, string) error { return nil }
func (g *runtimeControlGuest) Exists(ctx context.Context, _ string) (bool, error) {
	g.existsCtx = ctx
	if g.exists != nil {
		return *g.exists, nil
	}
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

	// Signal is now NotSupported (no POSIX signals for RTOS guests); verify
	// presence-check context propagation via checkStateWithContext directly.
	if _, err := container.checkStateWithContext(ctx); err != nil {
		t.Fatalf("checkState returned error: %v", err)
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

	// StoreSandbox failed, so the container was rolled back into the in-memory
	// map. Its per-container state must NOT be deleted: the sandbox snapshot on
	// disk still references it, and removing the state would make the next
	// restore fail to find it.
	if len(store.deleted) != 0 {
		t.Fatalf("deleted snapshots = %v, want none (StoreSandbox failed)", store.deleted)
	}
}

// TestDeleteStoppedContainerTreatsAbsentGuestAsSuccess models the real
// adapter contract: with the guest socket absent, guestControl.Remove routes
// to destroyLingeringDomain and succeeds (no-op when the domain is gone,
// destroy when it lingers). Delete must still CALL Remove (asserted by
// TestDeleteDownContainerStillRemovesGuestClient) and succeed when the
// adapter reports the guest absent — an earlier version of this test
// stubbed Remove to fail and asserted delete skips it, which encoded the
// orphaned-domain behavior fixed for scan item 2.1.
func TestDeleteStoppedContainerTreatsAbsentGuestAsSuccess(t *testing.T) {
	ctx := context.Background()
	absent := false
	guest := &runtimeControlGuest{
		exists: &absent,
	}
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
}

func TestDeleteStoppedContainerPropagatesRemoveErrorWhenGuestStillExists(t *testing.T) {
	ctx := context.Background()
	present := true
	removeErr := errors.New("remove timed out")
	guest := &runtimeControlGuest{removeErr: removeErr, exists: &present}
	store := newMemoryStateStore()
	sandbox := &Sandbox{
		ctx:          ctx,
		id:           "sandbox-delete-stopped-linger",
		stateRepo:    stateRepositoryFromStore(store),
		guestControl: guest,
		config: &SandboxConfig{
			ID: "sandbox-delete-stopped-linger",
			ContainerConfigs: map[string]*ContainerConfig{
				"container-delete-linger": &ContainerConfig{ID: "container-delete-linger"},
			},
		},
		containers: map[string]*Container{},
		state:      SandboxState{State: StateRunning},
	}
	container := &Container{
		ctx:           ctx,
		id:            "container-delete-linger",
		config:        &ContainerConfig{ID: "container-delete-linger", CacheRoot: t.TempDir()},
		sandbox:       sandbox,
		containerPath: filepath.Join(sandbox.id, "container-delete-linger"),
		state:         ContainerState{State: StateStopped},
	}
	sandbox.containers[container.id] = container

	err := container.delete(ctx)
	if err == nil || !strings.Contains(err.Error(), removeErr.Error()) {
		t.Fatalf("delete error = %v, want wrapped %v", err, removeErr)
	}
	if _, ok := sandbox.containers[container.id]; !ok {
		t.Fatal("container was removed from sandbox despite failed guest remove")
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

// --- Regression tests: StateDown must not skip guest teardown (scan 2.1) ---
//
// Exists is socket-only (contribution-guide invariant 3): a micad crash wipes
// the control socket while the Xen domain lives on. checkState marks such a
// container Down, but Stop/Kill/Delete must STILL call guestControl so the
// adapter can destroy the lingering domain. Skipping teardown leaves an
// orphan Xen domain that containerd believes is gone.

func newLingeringDomainSandbox(t *testing.T, guest ports.GuestControl, containerState StateString) (*Sandbox, *Container) {
	t.Helper()
	ctx := context.Background()
	sandboxID := "sandbox-lingering-" + strings.ReplaceAll(strings.ToLower(string(containerState)), " ", "")
	containerID := "container-lingering"
	store := newMemoryStateStore()
	sandbox := &Sandbox{
		ctx:          ctx,
		id:           sandboxID,
		stateRepo:    stateRepositoryFromStore(store),
		guestControl: guest,
		config: &SandboxConfig{
			ID: sandboxID,
			ContainerConfigs: map[string]*ContainerConfig{
				containerID: {ID: containerID},
			},
		},
		containers: map[string]*Container{},
		state:      SandboxState{State: StateRunning},
	}
	container := &Container{
		ctx:           ctx,
		id:            containerID,
		config:        &ContainerConfig{ID: containerID, CacheRoot: t.TempDir()},
		sandbox:       sandbox,
		containerPath: filepath.Join(sandboxID, containerID),
		state:         ContainerState{State: containerState},
	}
	sandbox.containers[container.id] = container
	return sandbox, container
}

func TestStopDownContainerStillTearsDownLingeringDomain(t *testing.T) {
	ctx := context.Background()
	absent := false
	guest := &runtimeControlGuest{exists: &absent}
	_, container := newLingeringDomainSandbox(t, guest, StateRunning)

	if err := container.stop(ctx, false); err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
	if guest.stopCalls == 0 {
		t.Fatal("stop skipped guest teardown on StateDown: guestControl.Stop never called, lingering Xen domain would be orphaned")
	}
	if got := container.currentState(); got != StateStopped {
		t.Fatalf("container state = %s, want stopped", got)
	}
}

func TestKillDownContainerStillTearsDownLingeringDomain(t *testing.T) {
	ctx := context.Background()
	absent := false
	guest := &runtimeControlGuest{exists: &absent}
	_, container := newLingeringDomainSandbox(t, guest, StateRunning)

	if err := container.kill(ctx); err != nil {
		t.Fatalf("kill returned error: %v", err)
	}
	if guest.stopCalls == 0 {
		t.Fatal("kill skipped guest teardown on StateDown: guestControl.Stop never called, lingering Xen domain would be orphaned")
	}
	if got := container.currentState(); got != StateStopped {
		t.Fatalf("container state = %s, want stopped", got)
	}
}

func TestDeleteDownContainerStillRemovesGuestClient(t *testing.T) {
	ctx := context.Background()
	absent := false
	// Model the real adapter: with the socket absent, Remove routes to
	// destroyLingeringDomain, which no-ops successfully when the domain is
	// gone and destroys it when alive.
	guest := &runtimeControlGuest{exists: &absent}
	sandbox, container := newLingeringDomainSandbox(t, guest, StateStopped)

	if err := container.delete(ctx); err != nil {
		t.Fatalf("delete returned error: %v", err)
	}
	if guest.removeCalls == 0 {
		t.Fatal("delete skipped guest removal on StateDown: guestControl.Remove never called, lingering Xen domain would be orphaned")
	}
	if _, ok := sandbox.containers[container.id]; ok {
		t.Fatal("container remained in sandbox after delete")
	}
}

// --- Regression: Kill/Stop must serialize with Start via startMu (scan 2.2).
// Start holds startMu across ensureClientPresence + startClient; a Kill in
// that window used to run unsynchronized: it could destroy the domain Start
// was building, and Start's second presence check would then re-register a
// NEW domain after the Kill had already returned success.

func TestKillWaitsForInFlightStart(t *testing.T) {
	ctx := context.Background()
	entered := make(chan struct{})
	release := make(chan struct{})
	// Planned start failure: startClient reaches guestControl.Start (and the
	// gate) only with a non-nil guest executor; failing Start afterwards
	// keeps the post-guest path (CPU/memory setup) out of the test.
	guest := &runtimeControlGuest{startErr: errors.New("planned start failure")}
	guest.startEntered = entered
	guest.startGate = release
	sandbox := &Sandbox{
		ctx:          ctx,
		id:           "sandbox-kill-start-race",
		stateRepo:    stateRepositoryFromStore(newMemoryStateStore()),
		guestControl: guest,
		config: &SandboxConfig{
			ID: "sandbox-kill-start-race",
			ContainerConfigs: map[string]*ContainerConfig{
				"container-race": {ID: "container-race"},
			},
		},
		containers: map[string]*Container{},
		state:      SandboxState{State: StateRunning},
	}
	container := &Container{
		ctx:           ctx,
		id:            "container-race",
		config:        &ContainerConfig{ID: "container-race", CacheRoot: t.TempDir()},
		guestExec:     recordingGuestExecutor{},
		sandbox:       sandbox,
		containerPath: filepath.Join(sandbox.id, "container-race"),
		state:         ContainerState{State: StateReady},
	}
	sandbox.containers[container.id] = container

	startDone := make(chan error, 1)
	go func() { startDone <- container.start(ctx) }()
	<-entered // Start is inside its critical section, holding startMu.

	killDone := make(chan error, 1)
	go func() { killDone <- container.kill(ctx) }()

	// While Start is parked mid-sequence, Kill must NOT have torn the guest
	// down: it has to wait for startMu instead of racing the domain build.
	deadline := time.After(300 * time.Millisecond)
	select {
	case <-killDone:
		t.Fatal("kill completed while start was still in its critical section: no serialization with Start")
	case <-deadline:
	}
	if guest.stopCalls != 0 {
		t.Fatalf("guest teardown ran during start window: stopCalls=%d, want 0", guest.stopCalls)
	}

	close(release)
	if err := <-startDone; err == nil || !strings.Contains(err.Error(), "planned start failure") {
		t.Fatalf("start error = %v, want the planned start failure", err)
	}
	if err := <-killDone; err != nil {
		t.Fatalf("kill returned error: %v", err)
	}
	if guest.stopCalls == 0 {
		t.Fatal("kill never tore down the guest after start finished")
	}
	if got := container.currentState(); got != StateStopped {
		t.Fatalf("container state = %s, want stopped", got)
	}
}

// vanishingSocketGuest reports the control socket present for the first
// probe (checkState -> Stopped branch) and absent afterwards, modeling a
// micad that dies mid-MRemove: the remove fails and the socket listener is
// gone, while the Xen domain's fate is unknown.
type vanishingSocketGuest struct {
	runtimeControlGuest
	probes int
}

func (g *vanishingSocketGuest) Exists(ctx context.Context, id string) (bool, error) {
	g.probes++
	if g.probes == 1 {
		return true, nil
	}
	return g.runtimeControlGuest.Exists(ctx, id)
}

// TestDeleteStoppedContainerCrossChecksDomainAfterRemoveFailure guards the
// Stopped-branch exemption in removeGuestClientForDelete: after a failed
// Remove whose failure coincides with the control socket disappearing
// (micad died mid-MRemove), the socket-only Exists probe reports absence
// and would grant the exemption — reporting delete success while a live
// Xen domain leaks. The hypervisor layer must confirm the domain is gone
// first (same shape as the StateDown branch).
func TestDeleteStoppedContainerCrossChecksDomainAfterRemoveFailure(t *testing.T) {
	newSandbox := func(hyp ports.HypervisorControl) (*Sandbox, *Container, *vanishingSocketGuest) {
		absent := false
		guest := &vanishingSocketGuest{runtimeControlGuest: runtimeControlGuest{
			removeErr: errors.New("xl destroy failed"), exists: &absent,
		}}
		store := newMemoryStateStore()
		sandbox := &Sandbox{
			ctx:               context.Background(),
			id:                "sandbox-delete-stopped-cross",
			stateRepo:         stateRepositoryFromStore(store),
			guestControl:      guest,
			hypervisorControl: hyp,
			config: &SandboxConfig{
				ID: "sandbox-delete-stopped-cross",
				ContainerConfigs: map[string]*ContainerConfig{
					"container-cross": &ContainerConfig{ID: "container-cross"},
				},
			},
			containers: map[string]*Container{},
			state:      SandboxState{State: StateRunning},
		}
		container := &Container{
			ctx:           context.Background(),
			id:            "container-cross",
			config:        &ContainerConfig{ID: "container-cross", CacheRoot: t.TempDir()},
			sandbox:       sandbox,
			containerPath: filepath.Join(sandbox.id, "container-cross"),
			state:         ContainerState{State: StateStopped},
		}
		sandbox.containers[container.id] = container
		return sandbox, container, guest
	}

	// Domain confirmed alive: delete must fail and keep the container so a
	// retry can converge; reporting success would orphan the live domain.
	sandbox, container, guest := newSandbox(&fakeHypervisorControl{name: "running"})
	if err := container.delete(context.Background()); err == nil {
		t.Fatalf("delete must fail when the domain is still alive after a failed remove (Exists probes=%d)", guest.probes)
	}
	if _, ok := sandbox.containers[container.id]; !ok {
		t.Fatal("container was removed from sandbox despite a live lingering domain")
	}

	// Domain confirmed gone at the hypervisor layer: the exemption holds and
	// delete succeeds.
	_, container2, _ := newSandbox(&erroringDomainProbeControl{probeErr: errors.New("domain does not exist")})
	if err := container2.delete(context.Background()); err != nil {
		t.Fatalf("delete with domain confirmed gone should succeed, got %v", err)
	}
}
