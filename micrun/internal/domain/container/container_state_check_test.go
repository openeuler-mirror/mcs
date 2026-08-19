package container

import (
	"context"
	"errors"
	"testing"

	"micrun/internal/ports"
	er "micrun/internal/support/errors"
)

// statusProbeGuest is a GuestControl stub whose Status returns a fixed value,
// used to drive checkStateWithContext probing.
type statusProbeGuest struct {
	status ports.GuestStatus
}

func (g *statusProbeGuest) Start(context.Context, string) error  { return nil }
func (g *statusProbeGuest) Stop(context.Context, string) error   { return nil }
func (g *statusProbeGuest) Remove(context.Context, string) error { return nil }
func (g *statusProbeGuest) Pause(context.Context, string) error  { return nil }
func (g *statusProbeGuest) Resume(context.Context, string) error { return nil }
func (g *statusProbeGuest) Exists(context.Context, string) (bool, error) {
	return true, nil
}
func (g *statusProbeGuest) Status(context.Context, string) (ports.GuestStatus, error) {
	return g.status, nil
}

func newStateProbeContainer(state StateString, status ports.GuestStatus) (*Container, *memoryStateStore) {
	store := newMemoryStateStore()
	ctx := context.Background()
	sandbox := &Sandbox{
		ctx:          ctx,
		id:           "sandbox-probe",
		config:       &SandboxConfig{ID: "sandbox-probe"},
		containers:   map[string]*Container{},
		state:        SandboxState{State: StateRunning},
		stateRepo:    stateRepositoryFromStore(store),
		deps:         testDepsWithStore(store),
		guestControl: &statusProbeGuest{status: status},
	}
	c := &Container{
		ctx:           ctx,
		id:            "container-probe",
		config:        &ContainerConfig{ID: "container-probe"},
		sandbox:       sandbox,
		state:         ContainerState{State: state},
		containerPath: "sandbox-probe/container-probe",
	}
	sandbox.containers[c.id] = c
	return c, store
}

// Crash between guest Pause and persisting StatePaused leaves disk Running
// while Xen reports Suspended. Converge to Paused so recovery Resume works.
func TestCheckStateRunningGuestSuspendedConvergesToPaused(t *testing.T) {
	c, _ := newStateProbeContainer(StateRunning, ports.GuestStatus{State: "Suspended"})
	got, err := c.checkStateWithContext(context.Background())
	if err != nil {
		t.Fatalf("checkStateWithContext error = %v", err)
	}
	if got != StatePaused {
		t.Fatalf("checkStateWithContext = %s, want %s", got, StatePaused)
	}
	if c.currentState() != StatePaused {
		t.Fatalf("container state = %s, want %s", c.currentState(), StatePaused)
	}
}

// A paused container whose guest was intentionally shut down must stay
// Paused: non-Xen pedestals implement Pause as a full mica stop
// (MPause->MStop), after which micad reports Stopped/Offline while the client
// remains registered. Marking it Down would fabricate exit 130, wake Wait,
// and make Resume a silent no-op while the workload is lost.
func TestCheckStatePausedGuestIntentionallyStopped(t *testing.T) {
	for _, status := range []ports.GuestStatus{
		{State: "Stopped", Stopped: true},
		{State: "Offline"},
	} {
		c, _ := newStateProbeContainer(StatePaused, status)
		got, err := c.checkStateWithContext(context.Background())
		if err != nil {
			t.Fatalf("checkStateWithContext(%q) error = %v", status.State, err)
		}
		if got != StatePaused {
			t.Fatalf("checkStateWithContext(%q) = %s, want %s", status.State, got, StatePaused)
		}
		if c.currentState() != StatePaused {
			t.Fatalf("container state after probe(%q) = %s, want %s", status.State, c.currentState(), StatePaused)
		}
	}
}

// The same Stopped/Offline reports for a RUNNING container still mean the
// guest really died: the paused exemption must not weaken crash detection.
func TestCheckStateRunningGuestStoppedStillDown(t *testing.T) {
	for _, status := range []ports.GuestStatus{
		{State: "Stopped", Stopped: true},
		{State: "Offline"},
	} {
		c, _ := newStateProbeContainer(StateRunning, status)
		got, err := c.checkStateWithContext(context.Background())
		if err != nil {
			t.Fatalf("checkStateWithContext(%q) error = %v", status.State, err)
		}
		if got != StateDown {
			t.Fatalf("checkStateWithContext(%q) = %s, want %s", status.State, got, StateDown)
		}
		if c.currentState() != StateDown {
			t.Fatalf("container state after probe(%q) = %s, want %s", status.State, c.currentState(), StateDown)
		}
	}
}

// A deleted container must not be resurrected by a late SaveState from an
// in-flight Kill/State probe. With the combined sandbox document this holds
// by construction: the container is out of the containers map, so the
// persisted document simply carries no record for it.
func TestSaveStateAfterDeleteOmitsContainerRecord(t *testing.T) {
	c, store := newStateProbeContainer(StateStopped, ports.GuestStatus{})
	delete(c.sandbox.containers, c.id)

	if err := c.SaveState(); err != nil {
		t.Fatalf("SaveState error = %v", err)
	}

	ss := loadSandboxSnapshotFromStore(t, store, c.sandbox.id)
	if _, ok := ss.Containers[c.id]; ok {
		t.Fatalf("deleted container %s resurrected in sandbox document", c.id)
	}
}

// cleanupAfterDelete persists a sandbox document without the deleted
// container's record and config.
func TestCleanupAfterDeleteDropsContainerRecord(t *testing.T) {
	c, store := newStateProbeContainer(StateStopped, ports.GuestStatus{})
	c.config.CacheRoot = t.TempDir()
	if err := c.cleanupAfterDelete(context.Background()); err != nil {
		t.Fatalf("cleanupAfterDelete error = %v", err)
	}
	ss := loadSandboxSnapshotFromStore(t, store, c.sandbox.id)
	if _, ok := ss.Containers[c.id]; ok {
		t.Fatal("cleanupAfterDelete persisted a document still carrying the deleted container's record")
	}
	if _, ok := ss.Config.ContainerConfigs[c.id]; ok {
		t.Fatal("cleanupAfterDelete persisted a document still carrying the deleted container's config")
	}
}

// When StoreSandbox fails, cleanupAfterDelete rolls back the in-memory
// removal so a retry can find the container again — and a later persist must
// still include the container's record (the delete never landed on disk).
func TestCleanupAfterDeleteStoreFailureKeepsRecordPersistable(t *testing.T) {
	c, store := newStateProbeContainer(StateStopped, ports.GuestStatus{})
	c.config.CacheRoot = t.TempDir()
	store.saveErr = errors.New("save sandbox failed")
	if err := c.cleanupAfterDelete(context.Background()); err == nil {
		t.Fatal("cleanupAfterDelete expected StoreSandbox error, got nil")
	}
	store.saveErr = nil
	if err := c.SaveState(); err != nil {
		t.Fatalf("SaveState after rollback error = %v", err)
	}
	ss := loadSandboxSnapshotFromStore(t, store, c.sandbox.id)
	if _, ok := ss.Containers[c.id]; !ok {
		t.Fatal("rolled-back container's record missing from sandbox document")
	}
}

// Restored records are consumed one-shot: after the rebuild's restoreState
// took the record, a same-id re-create (Delete then Create) must start fresh
// instead of resurrecting the pre-restart state.
func TestRestoredContainerRecordConsumedOneShot(t *testing.T) {
	c, _ := newStateProbeContainer(StateDown, ports.GuestStatus{})
	sb := c.sandbox
	sb.restoredContainers = map[string]ContainerRuntimeRecord{
		c.id: {State: ContainerState{State: StateRunning}},
	}

	if err := c.restoreState(context.Background()); err != nil {
		t.Fatalf("restoreState error = %v", err)
	}
	if got := c.currentState(); got != StateRunning {
		t.Fatalf("restored state = %s, want %s", got, StateRunning)
	}

	recreated := &Container{
		ctx:           context.Background(),
		id:            c.id,
		config:        c.config,
		sandbox:       sb,
		state:         ContainerState{State: StateDown},
		containerPath: c.containerPath,
	}
	err := recreated.restoreState(context.Background())
	if !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("second restore error = %v, want ContainerNotFound (record must be consumed)", err)
	}
	if got := recreated.currentState(); got != StateDown {
		t.Fatalf("re-created container state = %s, want fresh %s", got, StateDown)
	}
}

func loadSandboxSnapshotFromStore(t *testing.T, store *memoryStateStore, sandboxID string) *SandboxStorage {
	t.Helper()
	repo := stateRepositoryFromStore(store)
	ss, err := repo.loadSandboxRuntimeSnapshot(context.Background(), sandboxSnapshotID(sandboxID))
	if err != nil {
		t.Fatalf("load sandbox snapshot: %v", err)
	}
	return ss
}
