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
		state:  SandboxState{State: StateReady},
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
		state:      SandboxState{State: StateReady},
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
		state:        SandboxState{State: StateReady},
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
		state:        SandboxState{State: StateReady},
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

func TestRollbackFailedContainerConfigKeepsWinnerEntry(t *testing.T) {
	winnerCfg := &ContainerConfig{ID: "c1"}
	loserCfg := &ContainerConfig{ID: "c1"}
	sandbox := &Sandbox{
		id: "sandbox-rollback-winner",
		config: &SandboxConfig{
			ID:               "sandbox-rollback-winner",
			ContainerConfigs: map[string]*ContainerConfig{"c1": winnerCfg},
		},
		containers: map[string]*Container{"c1": {id: "c1", config: winnerCfg}},
	}

	// A loser's rollback must not delete the winner's config entry: the entry
	// governs the next StoreSandbox snapshot — deleting it would drop the
	// winner container from disk, so a shim restart would fail to recover it
	// and leak its guest domain.
	sandbox.rollbackFailedContainerConfig("c1", loserCfg, true)
	if sandbox.config.ContainerConfigs["c1"] != winnerCfg {
		t.Fatal("loser rollback deleted winner's ContainerConfigs entry")
	}
	if sandbox.config.InfraOnly {
		t.Fatal("loser rollback clobbered InfraOnly while a winner is registered")
	}
}

func TestRollbackFailedContainerConfigDeletesOwnEntryOnly(t *testing.T) {
	own := &ContainerConfig{ID: "c1"}
	other := &ContainerConfig{ID: "c1"}

	// No winner registered: the failed create removes its own entry and
	// restores InfraOnly.
	sandbox := &Sandbox{
		id: "sandbox-rollback-own",
		config: &SandboxConfig{
			ID:               "sandbox-rollback-own",
			ContainerConfigs: map[string]*ContainerConfig{"c1": own},
		},
		containers: map[string]*Container{},
	}
	sandbox.rollbackFailedContainerConfig("c1", own, true)
	if _, ok := sandbox.config.ContainerConfigs["c1"]; ok {
		t.Fatal("rollback did not delete its own config entry")
	}
	if !sandbox.config.InfraOnly {
		t.Fatal("rollback did not restore InfraOnly")
	}

	// The entry was overwritten by a later same-ID create (still in flight):
	// pointer identity must protect that entry from this call's rollback.
	sandbox2 := &Sandbox{
		id: "sandbox-rollback-overwritten",
		config: &SandboxConfig{
			ID:               "sandbox-rollback-overwritten",
			ContainerConfigs: map[string]*ContainerConfig{"c1": other},
		},
		containers: map[string]*Container{},
	}
	sandbox2.rollbackFailedContainerConfig("c1", own, false)
	if sandbox2.config.ContainerConfigs["c1"] != other {
		t.Fatal("rollback deleted an entry written by a later create")
	}
}

func TestCreateContainerRejectsInFlightSameIDCreate(t *testing.T) {
	// A speculative ContainerConfigs entry (written by an in-flight create)
	// doubles as the create claim: a concurrent same-ID Create must be
	// rejected instead of overwriting it — overwriting split the pair of maps
	// or let the loser's rollback drop the entry, losing the container from
	// the StoreSandbox snapshot.
	inflight := &ContainerConfig{ID: "c1"}
	sandbox := &Sandbox{
		id:     "sandbox-claim",
		ctx:    context.Background(),
		config: &SandboxConfig{ContainerConfigs: map[string]*ContainerConfig{"c1": inflight}},
		state:  SandboxState{State: StateReady},
	}
	if _, err := sandbox.CreateContainer(context.Background(), ContainerConfig{ID: "c1"}); !errors.Is(err, er.AlreadyExists) {
		t.Fatalf("CreateContainer error = %v, want AlreadyExists", err)
	}
	if sandbox.config.ContainerConfigs["c1"] != inflight {
		t.Fatal("in-flight entry was overwritten")
	}
}

// failNthSaveStore fails the Nth Save call, used to inject a persistence
// failure at an exact point in the create sequence.
type failNthSaveStore struct {
	*memoryStateStore
	saveCalls int
	failOn    int
	err       error
}

func (s *failNthSaveStore) Save(ctx context.Context, snapshot *ports.RuntimeSnapshot) error {
	s.saveCalls++
	if s.saveCalls == s.failOn {
		return s.err
	}
	return s.memoryStateStore.Save(ctx, snapshot)
}

type stopCountingGuestControl struct {
	stubGuestControl
	stopCalls int
}

func (g *stopCountingGuestControl) Stop(context.Context, string) error {
	g.stopCalls++
	return nil
}

// c.create persists three times: Down (initial probe), Ready (registerClient),
// Ready (final setContainerState). Failing the THIRD save means registerClient
// already created the guest domain, so cleanup must stop it and remove the
// persisted state — not just roll back the config entry (the previous gap
// leaked the live domain as an orphan nothing tracked).
func TestCreateContainerCreateFailureStopsOrphanGuest(t *testing.T) {
	firmware := t.TempDir() + "/firmware.elf"
	if err := os.WriteFile(firmware, []byte("firmware"), 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}

	saveErr := errors.New("save failed")
	store := &failNthSaveStore{memoryStateStore: newMemoryStateStore(), failOn: 3, err: saveErr}
	deps := testDepsWithStore(store)
	guestCtl := &stopCountingGuestControl{}
	deps.CreateGuest = func(context.Context, GuestClientConfig) error {
		guestCtl.exists = true
		return nil
	}

	sandbox := &Sandbox{
		id:           "sandbox1",
		ctx:          context.Background(),
		stateRepo:    stateRepositoryFromStore(store),
		deps:         deps,
		config:       &SandboxConfig{},
		containers:   map[string]*Container{},
		guestControl: guestCtl,
		state:        SandboxState{State: StateReady},
	}

	_, err := sandbox.CreateContainer(context.Background(), ContainerConfig{
		ID:           "container1",
		OS:           "uniproton",
		ImageAbsPath: firmware,
		PedestalType: PedestalBaremetal,
	})

	if !errors.Is(err, saveErr) {
		t.Fatalf("CreateContainer error = %v, want save error", err)
	}
	if guestCtl.stopCalls == 0 {
		t.Fatal("guest domain was not stopped after create failure (orphan domain)")
	}
	if _, ok := sandbox.config.ContainerConfigs["container1"]; ok {
		t.Fatal("ContainerConfigs entry was not rolled back")
	}
	if len(store.deleted) == 0 {
		t.Fatal("container state file was not deleted")
	}
}

func TestStartContainerRejectsWhenSandboxNotOperational(t *testing.T) {
	// A pod container Start racing sandbox Stop/Delete (different task id,
	// so claimLifecycle does not serialize them) must fail instead of
	// re-creating the guest domain behind teardown.
	sandbox := &Sandbox{
		id:     "sandbox-stopped-start",
		ctx:    context.Background(),
		config: &SandboxConfig{},
		state:  SandboxState{State: StateStopped},
	}
	container := &Container{
		ctx:   context.Background(),
		id:    "c1",
		state: ContainerState{State: StateDown},
	}
	container.sandbox = sandbox
	sandbox.containers = map[string]*Container{"c1": container}

	if _, err := sandbox.StartContainer(context.Background(), "c1"); !errors.Is(err, er.SandboxNotReady) {
		t.Fatalf("StartContainer error = %v, want SandboxNotReady", err)
	}
}

func TestCreateContainerRejectsWhenSandboxNotOperational(t *testing.T) {
	// After Stop/Delete the sandbox is Stopped: a late pod Create must not
	// call CreateGuest and leave an untracked domain behind teardown.
	sandbox := &Sandbox{
		id:     "sandbox-stopped",
		ctx:    context.Background(),
		config: &SandboxConfig{},
		state:  SandboxState{State: StateStopped},
	}
	if _, err := sandbox.CreateContainer(context.Background(), ContainerConfig{ID: "c1"}); !errors.Is(err, er.SandboxNotReady) {
		t.Fatalf("CreateContainer error = %v, want SandboxNotReady", err)
	}
	if sandbox.config.ContainerConfigs != nil {
		if _, ok := sandbox.config.ContainerConfigs["c1"]; ok {
			t.Fatal("CreateContainer wrote a config claim on a non-operational sandbox")
		}
	}
}
