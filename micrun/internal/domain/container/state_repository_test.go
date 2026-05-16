package container

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"micrun/internal/ports"
	defs "micrun/internal/support/definitions"
	er "micrun/internal/support/errors"
)

func TestContainerLegacyStatePathsDeduplicatesCandidates(t *testing.T) {
	containerRoot := t.TempDir()
	repo := stateRepositoryWithLegacyRoots(newMemoryStateStore(), t.TempDir(), containerRoot)
	legacyPath := repo.legacy.containerStatePathByID("container1")
	extraPath := filepath.Join(containerRoot, "extra", defs.MicrunContainerStateFile)

	got := repo.legacy.containerStatePaths("container1", "container1", []string{
		"",
		legacyPath,
		"relative/state.json",
		filepath.Join(containerRoot, "extra", "not-state.json"),
		extraPath,
	})
	want := []string{legacyPath, extraPath}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("containerStatePaths = %v, want %v", got, want)
	}
}

func TestRemoveLegacyContainerStateFileSkipsUnsafePath(t *testing.T) {
	repo := stateRepositoryWithLegacyRoots(newMemoryStateStore(), t.TempDir(), t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "not-state.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	repo.legacy.removeContainerStateFile(path)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("unsafe legacy path should remain: %v", err)
	}
}

func TestContainerLegacyStatePathByIDRejectsUnsafeID(t *testing.T) {
	repo := stateRepositoryWithLegacyRoots(newMemoryStateStore(), t.TempDir(), t.TempDir())

	for _, id := range []string{"", " container", "container ", "../container", `parent\container`} {
		t.Run(id, func(t *testing.T) {
			if got := repo.legacy.containerStatePathByID(id); got != "" {
				t.Fatalf("containerStatePathByID(%q) = %q, want empty", id, got)
			}
		})
	}
}

func TestLegacyStatePathsRejectInvalidRoots(t *testing.T) {
	repo := stateRepositoryWithLegacyRoots(newMemoryStateStore(), "relative-sandbox", "relative-container")

	if got := repo.legacy.sandboxStatePath("sandbox"); got != "" {
		t.Fatalf("sandboxStatePath with relative root = %q, want empty", got)
	}
	if got := repo.legacy.containerStatePathByID("container"); got != "" {
		t.Fatalf("containerStatePathByID with relative root = %q, want empty", got)
	}
	if err := repo.legacy.removeSandboxState("sandbox"); err == nil || !strings.Contains(err.Error(), "path must be absolute") {
		t.Fatalf("removeSandboxState error = %v, want absolute root error", err)
	}
}

func TestRemoveLegacySandboxStateRejectsUnsafeID(t *testing.T) {
	repo := stateRepositoryWithLegacyRoots(newMemoryStateStore(), t.TempDir(), t.TempDir())

	for _, id := range []string{"", " sandbox", "sandbox ", "../sandbox", `parent\sandbox`} {
		t.Run(id, func(t *testing.T) {
			if err := repo.legacy.removeSandboxState(id); err == nil {
				t.Fatal("expected unsafe legacy sandbox id to fail")
			}
		})
	}
}

func TestLoadLegacyContainerStateReportsCorruptCandidate(t *testing.T) {
	legacyDir := t.TempDir()
	legacyPath := filepath.Join(legacyDir, defs.MicrunContainerStateFile)
	if err := os.WriteFile(legacyPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("write corrupt legacy state: %v", err)
	}

	repo := stateRepositoryWithLegacyRoots(newMemoryStateStore(), t.TempDir(), legacyDir)
	_, err := repo.loadLegacyContainerState(context.Background(), "container1", "container1", []string{legacyPath})
	if err == nil {
		t.Fatal("loadLegacyContainerState returned nil error, want corrupt JSON error")
	}
	if !strings.Contains(err.Error(), legacyPath) {
		t.Fatalf("loadLegacyContainerState error = %v, want legacy path", err)
	}
}

func TestMigrateLegacySnapshotSkipsCanceledContext(t *testing.T) {
	store := &recordingStateStore{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	migrateLegacySnapshot(ctx, store, "container", "container1", runtimeStateNamespaceContainer, "container1", ContainerStorage{})
	if len(store.saved) != 0 {
		t.Fatalf("legacy migration saved %d snapshots after cancellation, want 0", len(store.saved))
	}
}

func TestDeleteSnapshotHonorsCanceledContext(t *testing.T) {
	store := newMemoryStateStore()
	repo := stateRepositoryFromStore(store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := repo.deleteSnapshot(ctx, runtimeStateNamespaceContainer, "container1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("deleteSnapshot error = %v, want context.Canceled", err)
	}
	if len(store.deleted) != 0 {
		t.Fatalf("deleted snapshots = %d, want 0", len(store.deleted))
	}
}

func TestDeleteSnapshotRejectsTypedNilStore(t *testing.T) {
	var store *memoryStateStore
	repo := stateRepositoryFromStore(store)

	err := repo.deleteSnapshot(context.Background(), runtimeStateNamespaceContainer, "container1")
	if err == nil || err.Error() != "state store is nil" {
		t.Fatalf("deleteSnapshot typed nil error = %v, want state store is nil", err)
	}
}

func TestSaveSandboxReportsMissingInputs(t *testing.T) {
	repo := stateRepositoryFromStore(newMemoryStateStore())
	tests := []struct {
		name    string
		sandbox *Sandbox
		want    string
	}{
		{
			name: "nil sandbox",
			want: "sandbox is nil",
		},
		{
			name:    "missing config",
			sandbox: &Sandbox{id: "sandbox1"},
			want:    "sandbox config",
		},
		{
			name:    "empty id",
			sandbox: &Sandbox{config: &SandboxConfig{}},
			want:    er.EmptySandboxID.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.SaveSandbox(context.Background(), tt.sandbox)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("SaveSandbox error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestSaveSandboxUsesInjectedMetadataSources(t *testing.T) {
	store := newMemoryStateStore()
	repo := stateRepositoryFromStore(store)
	repo.now = func() time.Time { return time.Unix(123, 0) }
	repo.processID = func() int { return 456 }
	sandbox := &Sandbox{
		id:     "sandbox-metadata",
		config: &SandboxConfig{ID: "sandbox-metadata"},
		state:  SandboxState{State: StateReady},
	}

	if err := repo.SaveSandbox(context.Background(), sandbox); err != nil {
		t.Fatalf("SaveSandbox returned error: %v", err)
	}

	loaded, err := repo.LoadSandbox(context.Background(), sandbox.id)
	if err != nil {
		t.Fatalf("LoadSandbox returned error: %v", err)
	}
	if loaded.CreatedAt != 123 || loaded.ShimPID != 456 {
		t.Fatalf("sandbox metadata = (createdAt=%d shimPID=%d), want (123, 456)", loaded.CreatedAt, loaded.ShimPID)
	}
}

func TestStateRepositoryFromDependenciesUsesInjectedClock(t *testing.T) {
	store := newMemoryStateStore()
	deps := stubDeps()
	deps.StateStoreFactory = func() ports.StateStore { return store }
	deps.Now = func() time.Time { return time.Unix(789, 0) }

	repo, err := stateRepositoryFromDependenciesChecked(deps)
	if err != nil {
		t.Fatalf("stateRepositoryFromDependenciesChecked returned error: %v", err)
	}
	repo.processID = func() int { return 321 }
	sandbox := &Sandbox{
		id:     "sandbox-deps-clock",
		config: &SandboxConfig{ID: "sandbox-deps-clock"},
		state:  SandboxState{State: StateReady},
	}

	if err := repo.SaveSandbox(context.Background(), sandbox); err != nil {
		t.Fatalf("SaveSandbox returned error: %v", err)
	}

	loaded, err := repo.LoadSandbox(context.Background(), sandbox.id)
	if err != nil {
		t.Fatalf("LoadSandbox returned error: %v", err)
	}
	if loaded.CreatedAt != 789 || loaded.ShimPID != 321 {
		t.Fatalf("sandbox metadata = (createdAt=%d shimPID=%d), want (789, 321)", loaded.CreatedAt, loaded.ShimPID)
	}
}

func TestStateRepositoryFromDependenciesRejectsTypedNilStateStore(t *testing.T) {
	deps := stubDeps()
	var store *memoryStateStore
	deps.StateStoreFactory = func() ports.StateStore { return store }

	_, err := stateRepositoryFromDependenciesChecked(deps)
	if err == nil || !strings.Contains(err.Error(), "non-nil StateStore") {
		t.Fatalf("stateRepositoryFromDependenciesChecked error = %v, want non-nil StateStore", err)
	}
}
