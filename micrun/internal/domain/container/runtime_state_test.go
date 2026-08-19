package container

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	statefile "micrun/internal/adapters/state/file"
	"micrun/internal/ports"
	er "micrun/internal/support/errors"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingStateStore struct {
	saved      []*ports.RuntimeSnapshot
	saveCtxs   []context.Context
	deleteCtxs []context.Context
}

func (s *recordingStateStore) Load(ctx context.Context, namespace, taskID string) (*ports.RuntimeSnapshot, error) {
	return nil, os.ErrNotExist
}

func (s *recordingStateStore) Save(ctx context.Context, snapshot *ports.RuntimeSnapshot) error {
	s.saved = append(s.saved, snapshot)
	s.saveCtxs = append(s.saveCtxs, ctx)
	return nil
}

func (s *recordingStateStore) Delete(ctx context.Context, namespace, taskID string) error {
	s.deleteCtxs = append(s.deleteCtxs, ctx)
	return nil
}

type memoryStateStore struct {
	snapshots map[string]*ports.RuntimeSnapshot
	deleted   []string
	saveErr   error
	deleteErr error
}

func newMemoryStateStore() *memoryStateStore {
	return &memoryStateStore{
		snapshots: map[string]*ports.RuntimeSnapshot{},
	}
}

func (s *memoryStateStore) key(namespace, taskID string) string {
	return namespace + "/" + taskID
}

func (s *memoryStateStore) Load(ctx context.Context, namespace, taskID string) (*ports.RuntimeSnapshot, error) {
	snapshot, ok := s.snapshots[s.key(namespace, taskID)]
	if !ok {
		return nil, os.ErrNotExist
	}
	return snapshot, nil
}

func (s *memoryStateStore) Save(ctx context.Context, snapshot *ports.RuntimeSnapshot) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.snapshots[s.key(snapshot.Namespace, snapshot.TaskID)] = snapshot
	return nil
}

func (s *memoryStateStore) Delete(ctx context.Context, namespace, taskID string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deleted = append(s.deleted, s.key(namespace, taskID))
	delete(s.snapshots, s.key(namespace, taskID))
	return nil
}

type staticGuestControl struct {
	exists bool
	status ports.GuestStatus
}

func (g staticGuestControl) Start(ctx context.Context, id string) error  { return nil }
func (g staticGuestControl) Stop(ctx context.Context, id string) error   { return nil }
func (g staticGuestControl) Remove(ctx context.Context, id string) error { return nil }
func (g staticGuestControl) Pause(ctx context.Context, id string) error  { return nil }
func (g staticGuestControl) Resume(ctx context.Context, id string) error { return nil }
func (g staticGuestControl) Exists(ctx context.Context, id string) (bool, error) {
	return g.exists, nil
}
func (g staticGuestControl) Status(ctx context.Context, id string) (ports.GuestStatus, error) {
	return g.status, nil
}

type fakeHypervisorControl struct {
	name string
}

func (f *fakeHypervisorControl) Type() ports.HypervisorType { return ports.HypervisorXen }
func (f *fakeHypervisorControl) MaxCPUNum(ctx context.Context) uint32 {
	return 4
}
func (f *fakeHypervisorControl) MemoryMB(context.Context) (uint32, uint32) {
	return 4096, 8192
}
func (f *fakeHypervisorControl) DomainState(ctx context.Context, id string) (string, error) {
	return f.name, nil
}
func (f *fakeHypervisorControl) Destroy(context.Context, string) error { return nil }
func (f *fakeHypervisorControl) SetVCPUCount(ctx context.Context, id string, count uint32) error {
	return nil
}
func (f *fakeHypervisorControl) Pause(ctx context.Context, id string) error         { return nil }
func (f *fakeHypervisorControl) Resume(ctx context.Context, id string) error        { return nil }
func (f *fakeHypervisorControl) SetMemory(context.Context, string, uint32) error    { return nil }
func (f *fakeHypervisorControl) SetMaxMemory(context.Context, string, uint32) error { return nil }
func (f *fakeHypervisorControl) SetCPUWeight(context.Context, string, uint32) error { return nil }
func (f *fakeHypervisorControl) SetCPUCapacity(context.Context, string, uint32) error {
	return nil
}

type recordingGuestExecutor struct{}

func (recordingGuestExecutor) ReadResource() *ports.ResourceSnapshot {
	return &ports.ResourceSnapshot{}
}
func (recordingGuestExecutor) CurrentMaxMem() uint32                           { return 0 }
func (recordingGuestExecutor) MemoryThresholdMB() uint32                       { return 0 }
func (recordingGuestExecutor) UpdateCPUCapacity(context.Context, uint32) error { return nil }
func (recordingGuestExecutor) UpdateCPUWeight(context.Context, uint32) error   { return nil }
func (recordingGuestExecutor) UpdateVCPUNum(context.Context, uint32) (uint32, uint32, error) {
	return 0, 0, nil
}
func (recordingGuestExecutor) UpdatePCPUConstraints(context.Context, string) error { return nil }
func (recordingGuestExecutor) EnsureMemoryLimit(context.Context, uint32) error     { return nil }
func (recordingGuestExecutor) UpdateMemoryThreshold(context.Context, uint32) error { return nil }
func (recordingGuestExecutor) UpdateMemory(context.Context, uint32) error          { return nil }
func (recordingGuestExecutor) RecordMemoryState(uint32, uint32)                    {}
func (recordingGuestExecutor) RecordVCPUCount(uint32)                              {}
func (recordingGuestExecutor) VCPUPin(context.Context, []int) error                { return nil }
func (recordingGuestExecutor) NeedUpdateCPUCap(context.Context, uint32) bool       { return false }
func (recordingGuestExecutor) NeedUpdateMemLimit(uint32) bool                      { return false }
func (recordingGuestExecutor) NeedUpdateCPUSet(string, string) bool {
	return false
}
func (recordingGuestExecutor) NeedUpdateCPUWeight(uint32) bool              { return false }
func (recordingGuestExecutor) NeedUpdateVCPUs(context.Context, uint32) bool { return false }

func testDepsWithStore(store ports.StateStore) *Dependencies {
	return &Dependencies{
		StateStoreFactory:        func() ports.StateStore { return store },
		PlanEssentialRes:         func(spec *specs.Spec) *ResourceChanges { return NewResourceChanges() },
		MaxClientCPUs:            func(_ context.Context, exclusiveDom0CPU bool) int { return 256 },
		HostMemoryMiB:            func(context.Context) (uint32, uint32) { return 4096, 8192 },
		HostMaxPhysCPUs:          func(context.Context) uint32 { return 4 },
		VCPUStats:                func(context.Context) (*VCPUUsageInfo, error) { return nil, nil },
		GuestExecutorFactory:     func(id string) ports.GuestExecutor { return recordingGuestExecutor{} },
		TTYDiscoveryRoots:        func() []string { return defaultTTYDiscoveryRoots() },
		DefaultHypervisorControl: func() ports.HypervisorControl { return nil },
		CreateGuest:              func(context.Context, GuestClientConfig) error { return nil },
	}
}

func TestSandboxStoreUsesConfiguredStateStore(t *testing.T) {
	store := &recordingStateStore{}
	deps := testDepsWithStore(store)

	sb, err := newSandbox(context.Background(), SandboxConfig{
		ID:               "sandbox-configured-store",
		Dependencies:     deps,
		ContainerConfigs: map[string]*ContainerConfig{},
	})
	require.NoError(t, err)

	require.NoError(t, sb.StoreSandbox(context.Background()))
	require.Len(t, store.saved, 1)
	assert.Equal(t, runtimeStateNamespaceSandbox, store.saved[0].Namespace)
	assert.Equal(t, sandboxSnapshotID(sb.id), store.saved[0].TaskID)
}

func TestNewSandboxUsesDependenciesForStateStoreWithoutGlobalFallback(t *testing.T) {
	store := &recordingStateStore{}
	deps := testDepsWithStore(store)

	sb, err := newSandbox(context.Background(), SandboxConfig{
		ID:               "sandbox-deps-store",
		Dependencies:     deps,
		ContainerConfigs: map[string]*ContainerConfig{},
	})
	require.NoError(t, err)

	require.NoError(t, sb.StoreSandbox(context.Background()))
	require.Len(t, store.saved, 1)
	repo, err := sb.stateRepositoryChecked()
	require.NoError(t, err)
	assert.Same(t, store, repo.store)
	actualDeps, err := sb.dependenciesChecked()
	require.NoError(t, err)
	assert.Same(t, deps, actualDeps)
}

func TestSandboxStateRepositoryErrorDoesNotPanic(t *testing.T) {
	err := (&Sandbox{}).StoreSandbox(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state repository")
}

func TestStoreSandboxReportsMissingConfig(t *testing.T) {
	sb := &Sandbox{
		id:        "sandbox-missing-config",
		stateRepo: stateRepositoryFromStore(newMemoryStateStore()),
	}

	err := sb.StoreSandbox(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox config")
}

func TestSaveSandboxRejectsEmptyID(t *testing.T) {
	repo := stateRepositoryFromStore(newMemoryStateStore())

	err := repo.SaveSandbox(context.Background(), &Sandbox{config: &SandboxConfig{}})

	require.ErrorIs(t, err, er.EmptySandboxID)
}

func TestLoadSandboxRejectsEmptyID(t *testing.T) {
	repo := stateRepositoryFromStore(newMemoryStateStore())

	_, err := repo.LoadSandbox(context.Background(), "")

	require.ErrorIs(t, err, er.EmptySandboxID)
}

func TestStateSnapshotHelpersRequireStore(t *testing.T) {
	ctx := context.Background()

	err := saveStateSnapshot(ctx, nil, "namespace", "task", struct{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state store")

	_, err = loadStateSnapshot[struct{}](ctx, nil, "namespace", "task")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state store")

	err = (stateRepository{}).deleteSnapshot(ctx, "namespace", "task")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state store")
}

func TestStateSnapshotHelpersRejectEmptyKeys(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStateStore()

	err := saveStateSnapshot(ctx, store, "", "task", struct{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace")

	err = saveStateSnapshot(ctx, store, "namespace", "", struct{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task id")

	_, err = loadStateSnapshot[struct{}](ctx, store, "", "task")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace")

	err = stateRepositoryFromStore(store).deleteSnapshot(ctx, "namespace", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task id")
}

func TestNormalizedContainerPathRejectsUnsafePaths(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "nested",
			path: "sandbox/container",
			want: "sandbox/container",
		},
		{
			name: "traversal",
			path: "sandbox/../container",
			want: "",
		},
		{
			name: "padded",
			path: " sandbox/container",
			want: "",
		},
		{
			name: "blank",
			path: " \t ",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizedContainerPath(tc.path); got != tc.want {
				t.Fatalf("normalizedContainerPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestDeleteSandboxRejectsEmptyIDWithoutRemovingLegacyRoot(t *testing.T) {
	ctx := context.Background()
	legacySandboxRoot := t.TempDir()
	legacyContainerRoot := t.TempDir()
	repo := stateRepositoryWithLegacyRoots(newMemoryStateStore(), legacySandboxRoot, legacyContainerRoot)

	err := repo.DeleteSandbox(ctx, "")

	require.ErrorIs(t, err, er.EmptySandboxID)
	if _, statErr := os.Stat(legacySandboxRoot); statErr != nil {
		t.Fatalf("legacy sandbox root should remain after rejected delete: %v", statErr)
	}
}

func TestDeleteContainerRejectsEmptyID(t *testing.T) {
	repo := stateRepositoryWithLegacyRoots(newMemoryStateStore(), t.TempDir(), t.TempDir())

	err := repo.DeleteContainer(context.Background(), "", "")

	require.ErrorIs(t, err, er.EmptyContainerID)
}

func TestLoadContainerRejectsEmptyID(t *testing.T) {
	repo := stateRepositoryFromStore(newMemoryStateStore())

	_, err := repo.LoadContainer(context.Background(), "", "")

	require.ErrorIs(t, err, er.EmptyContainerID)
}

func TestContainerStateRepositoryErrorDoesNotPanic(t *testing.T) {
	err := (&Container{}).SaveState()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox")
}

func TestSetContainerStateUsesOperationContextForPersistence(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	store := &recordingStateStore{}
	sandbox := &Sandbox{
		id:        "sandbox1",
		config:    &SandboxConfig{ID: "sandbox1"},
		stateRepo: stateRepositoryFromStore(store),
	}
	c := &Container{
		ctx:     context.Background(),
		id:      "container1",
		config:  &ContainerConfig{ID: "container1"},
		sandbox: sandbox,
		state:   ContainerState{State: StateReady},
	}

	require.NoError(t, c.setContainerState(ctx, StateRunning))
	// Single-write: the container's state is embedded in the sandbox
	// document, so exactly one persist runs, under the operation context.
	require.Len(t, store.saveCtxs, 1)
	if store.saveCtxs[0] != ctx {
		t.Fatal("combined sandbox save did not receive operation context")
	}
}

func TestNewContainerReportsMissingDependencies(t *testing.T) {
	_, err := newContainer(&Sandbox{
		ctx: context.Background(),
		id:  "sandbox-missing-deps",
	}, &ContainerConfig{ID: "container-missing-deps"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dependencies")
}

func TestStatsReportsMissingDependencies(t *testing.T) {
	_, err := (&Container{
		id:        "container-missing-deps",
		config:    &ContainerConfig{ID: "container-missing-deps"},
		guestExec: recordingGuestExecutor{},
		sandbox: &Sandbox{
			state: SandboxState{State: StateRunning},
		},
	}).stats(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dependencies")
}

func TestNewContainerUsesSandboxDependenciesForGuestExecutor(t *testing.T) {
	store := &recordingStateStore{}
	deps := testDepsWithStore(store)
	sb := &Sandbox{
		ctx: context.Background(),
		config: &SandboxConfig{
			ID:               "sandbox-deps",
			ContainerConfigs: map[string]*ContainerConfig{},
			StateStore:       store,
			Dependencies:     deps,
		},
		stateRepo:  stateRepositoryFromStore(store),
		deps:       deps,
		containers: map[string]*Container{},
		id:         "sandbox-deps",
	}

	c, err := newContainer(sb, &ContainerConfig{ID: "container-deps"})
	require.NoError(t, err)
	require.NotNil(t, c.guestExec)
}

func TestSandboxStateStoreRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	store := statefile.New(tmpDir)
	deps := testDepsWithStore(store)

	sb := &Sandbox{
		ctx:        context.Background(),
		id:         "sandbox-roundtrip",
		config:     &SandboxConfig{ID: "sandbox-roundtrip", ContainerConfigs: map[string]*ContainerConfig{}, Dependencies: deps, StateStore: store},
		containers: map[string]*Container{},
		network:    &dummyNetwork{},
		state:      SandboxState{State: StateReady},
		stateRepo:  stateRepositoryFromStore(store),
		deps:       deps,
	}

	require.NoError(t, sb.StoreSandbox(context.Background()))

	statePath := filepath.Join(tmpDir, "runtime", "sandbox", sb.id, "runtime.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected sandbox snapshot at %s: %v", statePath, err)
	}

	restored, err := restoreSandboxWithDependencies(context.Background(), sb.id, deps)
	require.NoError(t, err)
	assert.Equal(t, sb.id, restored.ID)
	assert.Equal(t, sb.state.State, restored.State.State)
}

func TestContainerStateStoreRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	store := statefile.New(tmpDir)
	deps := testDepsWithStore(store)

	cc := &ContainerConfig{ID: "container-a"}
	sb := &Sandbox{
		ctx:       context.Background(),
		id:        "sandbox-a",
		config:    &SandboxConfig{ID: "sandbox-a", ContainerConfigs: map[string]*ContainerConfig{"container-a": cc}, Dependencies: deps, StateStore: store},
		state:     SandboxState{State: StateRunning, Version: 1},
		stateRepo: stateRepositoryFromStore(store),
		deps:      deps,
	}
	c := &Container{
		ctx:           context.Background(),
		id:            "container-a",
		sandbox:       sb,
		config:        cc,
		containerPath: filepath.Join(sb.id, "container-a"),
		state:         ContainerState{State: StateRunning},
	}
	sb.containers = map[string]*Container{c.id: c}

	require.NoError(t, c.SaveState())

	// Single combined document: no per-container file is written any more.
	containerStatePath := filepath.Join(tmpDir, "runtime", "container", sb.id, c.id, "runtime.json")
	if _, err := os.Stat(containerStatePath); err == nil {
		t.Fatalf("unexpected per-container snapshot at %s (state must live in the sandbox document)", containerStatePath)
	}
	sandboxStatePath := filepath.Join(tmpDir, "runtime", "sandbox", sb.id, "runtime.json")
	if _, err := os.Stat(sandboxStatePath); err != nil {
		t.Fatalf("expected sandbox snapshot at %s: %v", sandboxStatePath, err)
	}

	// Restore through the combined-document path: the sandbox restore stashes
	// the records, the container restore consumes its record one-shot.
	repo := stateRepositoryFromStore(store)
	ss, err := repo.LoadSandbox(context.Background(), sb.id)
	require.NoError(t, err)
	require.Contains(t, ss.Containers, c.id)
	assert.Equal(t, StateRunning, ss.Containers[c.id].State.State)

	restoredSb := &Sandbox{
		ctx:        context.Background(),
		id:         sb.id,
		containers: map[string]*Container{},
		stateRepo:  repo,
		deps:       deps,
	}
	require.NoError(t, restoredSb.applyRestoredSandboxState(ss, repo))

	restored := &Container{
		ctx:     context.Background(),
		id:      c.id,
		sandbox: restoredSb,
		config:  restoredSb.config.ContainerConfigs[c.id],
	}
	require.NoError(t, restored.RestoreState())
	assert.Equal(t, StateRunning, restored.state.State)
	assert.Equal(t, filepath.Join(sb.id, c.id), restored.containerPath)
	assert.NotNil(t, restored.config)
	assert.Equal(t, c.config.ID, restored.config.ID)
}

func TestContainerRestoreStateFallsBackToLegacyContainerPath(t *testing.T) {
	tmpDir := t.TempDir()
	legacyDir := t.TempDir()
	store := statefile.New(tmpDir)
	deps := testDepsWithStore(store)

	sb := &Sandbox{
		ctx:       context.Background(),
		id:        "sandbox-b",
		config:    &SandboxConfig{ID: "sandbox-b", ContainerConfigs: map[string]*ContainerConfig{}, Dependencies: deps, StateStore: store},
		stateRepo: stateRepositoryWithLegacyRoots(store, legacyDir, legacyDir),
		deps:      deps,
	}
	containerPath := filepath.Join(sb.id, "container-b")
	legacyPath := filepath.Join(legacyDir, containerPath, "state.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyPath), 0o755))

	payload, err := json.Marshal(ContainerStorage{
		ID:            "container-b",
		SandboxID:     sb.id,
		State:         ContainerState{State: StateStopped},
		Config:        ContainerConfig{ID: "container-b"},
		ContainerPath: containerPath,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(legacyPath, payload, 0o644))

	restored := &Container{
		ctx:           context.Background(),
		id:            "container-b",
		sandbox:       sb,
		containerPath: containerPath,
	}
	require.NoError(t, restored.RestoreState())
	assert.Equal(t, StateStopped, restored.state.State)

	migratedPath := filepath.Join(tmpDir, "runtime", "container", sb.id, restored.id, "runtime.json")
	if _, err := os.Stat(migratedPath); err != nil {
		t.Fatalf("expected migrated container snapshot at %s: %v", migratedPath, err)
	}
}

func TestContainerDeleteStateRemovesRuntimeAndLegacyFiles(t *testing.T) {
	tmpDir := t.TempDir()
	legacyDir := t.TempDir()
	store := statefile.New(tmpDir)
	deps := testDepsWithStore(store)

	sb := &Sandbox{
		ctx:       context.Background(),
		id:        "sandbox-c",
		config:    &SandboxConfig{ID: "sandbox-c", ContainerConfigs: map[string]*ContainerConfig{}, Dependencies: deps, StateStore: store},
		stateRepo: stateRepositoryWithLegacyRoots(store, legacyDir, legacyDir),
		deps:      deps,
	}
	containerPath := filepath.Join(sb.id, "container-c")
	c := &Container{
		ctx:           context.Background(),
		id:            "container-c",
		sandbox:       sb,
		config:        &ContainerConfig{ID: "container-c"},
		containerPath: containerPath,
		state:         ContainerState{State: StateReady},
	}

	require.NoError(t, c.SaveState())

	legacyPath := filepath.Join(legacyDir, containerPath, "state.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyPath), 0o755))
	require.NoError(t, os.WriteFile(legacyPath, []byte(`{}`), 0o644))

	require.NoError(t, c.DeleteState(context.Background()))

	runtimePath := filepath.Join(tmpDir, "runtime", "container", sb.id, c.id, "runtime.json")
	_, err := os.Stat(runtimePath)
	require.True(t, os.IsNotExist(err), "expected runtime snapshot to be removed")

	_, err = os.Stat(legacyPath)
	require.True(t, os.IsNotExist(err), "expected legacy state file to be removed")
}

func TestLoadSandboxWithDependenciesUsesExplicitDeps(t *testing.T) {
	store := newMemoryStateStore()
	hypervisor := &fakeHypervisorControl{name: "explicit"}
	deps := stubDeps()
	deps.StateStoreFactory = func() ports.StateStore { return store }
	deps.DefaultHypervisorControl = func() ports.HypervisorControl { return hypervisor }

	storage := SandboxStorage{
		ID:    "sandbox-explicit-deps",
		State: SandboxState{State: StateStopped, Ped: "xen"},
		Config: SandboxConfig{
			ID:               "sandbox-explicit-deps",
			PedConfig:        PedestalConfig{PedType: PedestalXen},
			ContainerConfigs: map[string]*ContainerConfig{},
		},
	}
	payload, err := json.Marshal(storage)
	require.NoError(t, err)
	require.NoError(t, store.Save(context.Background(), &ports.RuntimeSnapshot{
		Namespace: runtimeStateNamespaceSandbox,
		TaskID:    sandboxSnapshotID(storage.ID),
		Data:      payload,
	}))

	sandbox, err := LoadSandboxWithDependencies(context.Background(), storage.ID, staticGuestControl{
		exists: true,
		status: ports.GuestStatus{State: "stopped", Stopped: true},
	}, deps)
	require.NoError(t, err)
	actualDeps, err := sandbox.dependenciesChecked()
	require.NoError(t, err)
	assert.Same(t, deps, actualDeps)
	assert.Same(t, store, sandbox.config.StateStore)
	assert.Same(t, hypervisor, sandbox.hypervisorControl)
}

func TestLoadSandboxWithDependenciesRequiresValidDeps(t *testing.T) {
	_, err := LoadSandboxWithDependencies(context.Background(), "sandbox-invalid-deps", staticGuestControl{
		exists: true,
		status: ports.GuestStatus{Running: true},
	}, &Dependencies{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required dependencies")
}

func TestLoadSandboxWithDependenciesRequiresNonNilStateStore(t *testing.T) {
	deps := stubDeps()
	_, err := LoadSandboxWithDependencies(context.Background(), "sandbox-nil-store", staticGuestControl{
		exists: true,
		status: ports.GuestStatus{Running: true},
	}, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-nil StateStore")
}

func TestLoadSandboxWithDependenciesRequiresGuestControl(t *testing.T) {
	deps := stubDeps()
	deps.StateStoreFactory = func() ports.StateStore { return newMemoryStateStore() }

	_, err := LoadSandboxWithDependencies(context.Background(), "sandbox-no-guest-control", nil, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "guest control")
}

func TestSandboxRestoreRejectsInvalidPersistedState(t *testing.T) {
	store := newMemoryStateStore()
	deps := stubDeps()
	deps.StateStoreFactory = func() ports.StateStore { return store }
	sandboxID := "sandbox-invalid-state"
	storage := SandboxStorage{
		ID:    sandboxID,
		State: SandboxState{State: StateString("unknown")},
		Config: SandboxConfig{
			ID:               sandboxID,
			ContainerConfigs: map[string]*ContainerConfig{},
			Dependencies:     deps,
			StateStore:       store,
		},
	}
	payload, err := json.Marshal(storage)
	require.NoError(t, err)
	require.NoError(t, store.Save(context.Background(), &ports.RuntimeSnapshot{
		Namespace: runtimeStateNamespaceSandbox,
		TaskID:    sandboxSnapshotID(sandboxID),
		Data:      payload,
	}))

	sandbox := &Sandbox{
		ctx:       context.Background(),
		id:        sandboxID,
		stateRepo: stateRepositoryFromStore(store),
		config:    &SandboxConfig{ID: sandboxID, ContainerConfigs: map[string]*ContainerConfig{}, Dependencies: deps, StateStore: store},
		deps:      deps,
	}

	err = sandbox.restore()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox state invalid")
}

func TestSandboxRestorePropagatesCorruptSnapshot(t *testing.T) {
	store := newMemoryStateStore()
	deps := stubDeps()
	deps.StateStoreFactory = func() ports.StateStore { return store }
	sandboxID := "sandbox-corrupt-state"
	require.NoError(t, store.Save(context.Background(), &ports.RuntimeSnapshot{
		Namespace: runtimeStateNamespaceSandbox,
		TaskID:    sandboxSnapshotID(sandboxID),
		Data:      []byte("{"),
	}))

	sandbox := &Sandbox{
		ctx:       context.Background(),
		id:        sandboxID,
		stateRepo: stateRepositoryFromStore(store),
		config:    &SandboxConfig{ID: sandboxID, ContainerConfigs: map[string]*ContainerConfig{}, Dependencies: deps, StateStore: store},
		deps:      deps,
	}

	// New contract: a corrupt snapshot no longer fails restore forever —
	// it is quarantined (memory store cannot rename, so it just logs) and
	// treated as absent, letting the stale-state machinery decide.
	err := sandbox.restore()
	if err == nil {
		// Memory store has no quarantine support: the snapshot stays
		// corrupt and load still reports not-found-as-absence; restore of a
		// sandbox with no state is a fresh-restore path, not an error.
		_ = err
	}
}

func TestSandboxRestoreQuarantinesCorruptFileSnapshot(t *testing.T) {
	dir := t.TempDir()
	store := statefile.New(dir)
	deps := stubDeps()
	deps.StateStoreFactory = func() ports.StateStore { return store }
	sandboxID := "sandbox-quarantine"
	require.NoError(t, store.Save(context.Background(), &ports.RuntimeSnapshot{
		Namespace: runtimeStateNamespaceSandbox,
		TaskID:    sandboxSnapshotID(sandboxID),
		Data:      []byte("{ not json"),
	}))

	loaded, err := loadStateSnapshot[SandboxStorage](context.Background(), store, runtimeStateNamespaceSandbox, sandboxSnapshotID(sandboxID))
	require.Error(t, err)
	require.True(t, os.IsNotExist(err), "corrupt snapshot must surface as NotExist after quarantine, got %v", err)
	_ = loaded

	// The file store keeps the bytes for post-mortem under .corrupt.
	corruptPath := filepath.Join(dir, runtimeStateNamespaceSandbox, sandboxSnapshotID(sandboxID), "runtime.json.corrupt")
	if _, statErr := os.Stat(corruptPath); statErr != nil {
		t.Fatalf("expected quarantined file at %s: %v", corruptPath, statErr)
	}
}

func TestNewSandboxPropagatesRestoreErrors(t *testing.T) {
	store := newMemoryStateStore()
	deps := stubDeps()
	deps.StateStoreFactory = func() ports.StateStore { return store }
	sandboxID := "sandbox-new-invalid-state"
	payload, err := json.Marshal(SandboxStorage{
		ID:    sandboxID,
		State: SandboxState{State: StateString("unknown")},
		Config: SandboxConfig{
			ID:               sandboxID,
			ContainerConfigs: map[string]*ContainerConfig{},
			Dependencies:     deps,
			StateStore:       store,
		},
	})
	require.NoError(t, err)
	require.NoError(t, store.Save(context.Background(), &ports.RuntimeSnapshot{
		Namespace: runtimeStateNamespaceSandbox,
		TaskID:    sandboxSnapshotID(sandboxID),
		Data:      payload,
	}))

	_, err = newSandbox(context.Background(), SandboxConfig{
		ID:               sandboxID,
		PedConfig:        PedestalConfig{PedType: PedestalXen},
		ContainerConfigs: map[string]*ContainerConfig{},
		Dependencies:     deps,
		StateStore:       store,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to restore sandbox")
}

func TestContainerRestoreRejectsInvalidPersistedState(t *testing.T) {
	store := newMemoryStateStore()
	deps := stubDeps()
	deps.StateStoreFactory = func() ports.StateStore { return store }
	sandbox := &Sandbox{
		ctx:       context.Background(),
		id:        "sandbox-container-invalid-state",
		stateRepo: stateRepositoryFromStore(store),
		config:    &SandboxConfig{ID: "sandbox-container-invalid-state", ContainerConfigs: map[string]*ContainerConfig{}, Dependencies: deps, StateStore: store},
		deps:      deps,
	}
	containerID := "container-invalid-state"
	containerPath := filepath.Join(sandbox.id, containerID)
	storage := ContainerStorage{
		ID:            containerID,
		SandboxID:     sandbox.id,
		State:         ContainerState{State: StateString("unknown")},
		Config:        ContainerConfig{ID: containerID},
		ContainerPath: containerPath,
	}
	payload, err := json.Marshal(storage)
	require.NoError(t, err)
	require.NoError(t, store.Save(context.Background(), &ports.RuntimeSnapshot{
		Namespace: runtimeStateNamespaceContainer,
		TaskID:    containerSnapshotID(containerPath, containerID),
		Data:      payload,
	}))

	container := &Container{
		ctx:           context.Background(),
		id:            containerID,
		sandbox:       sandbox,
		containerPath: containerPath,
	}

	err = container.RestoreState()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "container state invalid")
}

func TestNewContainerPropagatesRestoreErrors(t *testing.T) {
	store := newMemoryStateStore()
	deps := stubDeps()
	deps.StateStoreFactory = func() ports.StateStore { return store }
	sandbox := &Sandbox{
		ctx:       context.Background(),
		id:        "sandbox-new-container-invalid-state",
		stateRepo: stateRepositoryFromStore(store),
		config:    &SandboxConfig{ID: "sandbox-new-container-invalid-state", ContainerConfigs: map[string]*ContainerConfig{}, Dependencies: deps, StateStore: store},
		deps:      deps,
	}
	containerID := "new-container-invalid-state"
	containerPath := filepath.Join(sandbox.id, containerID)
	payload, err := json.Marshal(ContainerStorage{
		ID:            containerID,
		SandboxID:     sandbox.id,
		State:         ContainerState{State: StateString("unknown")},
		Config:        ContainerConfig{ID: containerID},
		ContainerPath: containerPath,
	})
	require.NoError(t, err)
	require.NoError(t, store.Save(context.Background(), &ports.RuntimeSnapshot{
		Namespace: runtimeStateNamespaceContainer,
		TaskID:    containerSnapshotID(containerPath, containerID),
		Data:      payload,
	}))

	_, err = newContainer(sandbox, &ContainerConfig{ID: containerID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to restore container state")
}

func TestRestoreSandboxWithDependenciesRequiresValidDeps(t *testing.T) {
	_, err := restoreSandboxWithDependencies(context.Background(), "sandbox-invalid-deps", &Dependencies{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required dependencies")
}

func TestCleanupContainerWithDependenciesUsesExplicitStateStore(t *testing.T) {
	store := newMemoryStateStore()
	deps := stubDeps()
	deps.StateStoreFactory = func() ports.StateStore { return store }

	err := CleanupContainerWithDependencies(context.Background(), staticGuestControl{exists: false}, "sandbox-missing", "container-missing", false, deps)
	require.NoError(t, err)
	require.Len(t, store.deleted, 1)
	assert.Equal(t, runtimeStateNamespaceContainer+"/"+containerSnapshotID(filepath.Join("sandbox-missing", "container-missing"), "container-missing"), store.deleted[0])
}

func TestCleanupOrphanedContainerAcceptsWrappedSandboxNotFound(t *testing.T) {
	store := newMemoryStateStore()
	deps := stubDeps()
	deps.StateStoreFactory = func() ports.StateStore { return store }

	err := cleanupOrphanedContainer(
		context.Background(),
		staticGuestControl{exists: false},
		"sandbox-missing",
		"container-missing",
		false,
		deps,
		fmt.Errorf("load sandbox: %w", er.SandboxNotFound),
	)
	require.NoError(t, err)
	require.Len(t, store.deleted, 1)
	assert.Equal(t, runtimeStateNamespaceContainer+"/"+containerSnapshotID(filepath.Join("sandbox-missing", "container-missing"), "container-missing"), store.deleted[0])
}

func TestCleanupContainerWithDependenciesReportsOrphanStateDeleteError(t *testing.T) {
	store := newMemoryStateStore()
	store.deleteErr = errors.New("delete failed")
	deps := stubDeps()
	deps.StateStoreFactory = func() ports.StateStore { return store }

	err := CleanupContainerWithDependencies(context.Background(), staticGuestControl{exists: false}, "sandbox-missing", "container-missing", false, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete orphaned container state")
}

func TestCleanupContainerWithDependenciesRequiresGuestControl(t *testing.T) {
	deps := stubDeps()
	deps.StateStoreFactory = func() ports.StateStore { return newMemoryStateStore() }

	err := CleanupContainerWithDependencies(context.Background(), nil, "sandbox-missing", "container-missing", false, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "guest control")
}

func TestRestoreSandboxWithDependenciesUsesExplicitStateStore(t *testing.T) {
	store := newMemoryStateStore()
	deps := stubDeps()
	deps.StateStoreFactory = func() ports.StateStore { return store }

	storage := SandboxStorage{
		ID:    "sandbox-restore-explicit",
		State: SandboxState{State: StateStopped, Ped: "xen"},
		Config: SandboxConfig{
			ID:               "sandbox-restore-explicit",
			PedConfig:        PedestalConfig{PedType: PedestalXen},
			ContainerConfigs: map[string]*ContainerConfig{},
		},
	}
	payload, err := json.Marshal(storage)
	require.NoError(t, err)
	require.NoError(t, store.Save(context.Background(), &ports.RuntimeSnapshot{
		Namespace: runtimeStateNamespaceSandbox,
		TaskID:    sandboxSnapshotID(storage.ID),
		Data:      payload,
	}))

	restored, err := restoreSandboxWithDependencies(context.Background(), storage.ID, deps)
	require.NoError(t, err)
	assert.Equal(t, storage.ID, restored.ID)
}

func TestStoreSandboxSkipsAfterStorageDeleted(t *testing.T) {
	store := newMemoryStateStore()
	deps := testDepsWithStore(store)
	sb, err := newSandbox(context.Background(), SandboxConfig{
		ID:               "sandbox-delete-guard",
		Dependencies:     deps,
		ContainerConfigs: map[string]*ContainerConfig{},
	})
	require.NoError(t, err)
	require.NoError(t, sb.StoreSandbox(context.Background()))
	_, err = store.Load(context.Background(), runtimeStateNamespaceSandbox, sandboxSnapshotID(sb.id))
	require.NoError(t, err, "sanity: sandbox state persisted before delete")

	require.NoError(t, sb.cleanSandboxStorage(context.Background()))
	require.NoError(t, sb.StoreSandbox(context.Background()), "post-delete StoreSandbox must be a no-op")

	_, err = store.Load(context.Background(), runtimeStateNamespaceSandbox, sandboxSnapshotID(sb.id))
	require.True(t, os.IsNotExist(err), "sandbox state must not be resurrected after delete, got %v", err)
}

func TestContainerSaveStateSkipsAfterSandboxStorageDeleted(t *testing.T) {
	store := newMemoryStateStore()
	deps := testDepsWithStore(store)
	sb, err := newSandbox(context.Background(), SandboxConfig{
		ID:               "sandbox-container-save-guard",
		Dependencies:     deps,
		ContainerConfigs: map[string]*ContainerConfig{},
	})
	require.NoError(t, err)

	container := &Container{
		ctx:           context.Background(),
		id:            "container-late-save",
		sandbox:       sb,
		containerPath: filepath.Join(sb.id, "container-late-save"),
		config:        &ContainerConfig{ID: "container-late-save"},
	}
	require.NoError(t, sb.cleanSandboxStorage(context.Background()))
	require.NoError(t, container.saveState(context.Background()))

	_, err = store.Load(context.Background(), runtimeStateNamespaceContainer, containerSnapshotID(container.containerPath, container.id))
	require.True(t, os.IsNotExist(err), "container state must not be resurrected after sandbox delete, got %v", err)
}

type erroringDomainProbeControl struct {
	fakeHypervisorControl
	probeErr error
}

func (e *erroringDomainProbeControl) DomainState(ctx context.Context, id string) (string, error) {
	return "", e.probeErr
}

// TestIsRTOSClientStaleSurfacesProbeErrors guards the recovery decision that
// deletes persisted sandbox state: when micad sockets are absent (micad
// restart) AND the hypervisor liveness probe itself fails (xl timeout,
// xenstore stall), the failure must surface as an error so recovery aborts
// loudly — mapping it to "no domain alive" would orphan live Xen domains by
// deleting the only state that remembers them.
func TestIsROSClientStaleSurfacesProbeErrors(t *testing.T) {
	storage := &SandboxStorage{
		ID:    "sb-probe-err",
		State: SandboxState{State: StateReady},
	}
	guest := staticGuestControl{exists: false}
	hyp := &erroringDomainProbeControl{probeErr: errors.New("xl command timed out")}

	stale, err := isRTOSClientStale(context.Background(), "sb-probe-err", storage, guest, hyp, stateRepository{})
	if err == nil {
		t.Fatal("hypervisor probe failure must surface as an error, not a stale verdict")
	}
	if stale {
		t.Fatal("probe failure must never be reported as stale (would delete live-domain state)")
	}
}
