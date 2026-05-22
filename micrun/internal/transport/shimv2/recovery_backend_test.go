package shim

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	cntr "micrun/internal/domain/container"

	ctrannotations "github.com/containerd/containerd/pkg/cri/annotations"
	podmanannotations "github.com/containers/podman/v4/pkg/annotations"
)

func TestRecoveredTaskSandboxRole(t *testing.T) {
	tests := map[string]struct {
		annotations map[string]string
		canSandbox  bool
		isSandbox   bool
	}{
		"nil annotations default to single container": {
			canSandbox: true,
		},
		"empty annotations default to single container": {
			annotations: map[string]string{},
			canSandbox:  true,
		},
		"containerd pod container cannot become sandbox": {
			annotations: map[string]string{
				ctrannotations.SandboxID: "pod-1",
			},
		},
		"podman pod container cannot become sandbox": {
			annotations: map[string]string{
				podmanannotations.SandboxID: "pod-1",
			},
		},
		"containerd sandbox remains sandbox": {
			annotations: map[string]string{
				ctrannotations.ContainerType: ctrannotations.ContainerTypeSandbox,
			},
			canSandbox: true,
			isSandbox:  true,
		},
		"podman sandbox remains sandbox": {
			annotations: map[string]string{
				podmanannotations.ContainerType: podmanannotations.ContainerTypeSandbox,
			},
			canSandbox: true,
			isSandbox:  true,
		},
		"cri workload without sandbox id can become single container": {
			annotations: map[string]string{
				ctrannotations.ContainerType: ctrannotations.ContainerTypeContainer,
			},
			canSandbox: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			canSandbox, isSandbox := recoveredTaskSandboxRole(tc.annotations)
			if canSandbox != tc.canSandbox || isSandbox != tc.isSandbox {
				t.Fatalf("role = (%v, %v), want (%v, %v)", canSandbox, isSandbox, tc.canSandbox, tc.isSandbox)
			}
		})
	}
}

func TestRecoveredTaskFromContainerRejectsNilContainer(t *testing.T) {
	if _, err := recoveredTaskFromContainer(nil, true); err == nil {
		t.Fatal("recoveredTaskFromContainer expected error for nil container")
	}
}

func TestRecoveredTaskFromContainerRejectsEmptyID(t *testing.T) {
	_, err := recoveredTaskFromContainer(recoveryContainer{}, true)
	if err == nil || !strings.Contains(err.Error(), "id is empty") {
		t.Fatalf("recoveredTaskFromContainer error = %v, want empty id error", err)
	}
}

func TestRecoveredTaskFromContainerMapsContainerFields(t *testing.T) {
	task, err := recoveredTaskFromContainer(recoveryContainer{
		id: "sandbox",
		annotations: map[string]string{
			ctrannotations.ContainerType: ctrannotations.ContainerTypeSandbox,
		},
	}, true)
	if err != nil {
		t.Fatalf("recoveredTaskFromContainer returned error: %v", err)
	}
	if task.ID != "sandbox" || !task.IsRunning || !task.CanSandbox || !task.IsSandbox {
		t.Fatalf("unexpected recovered task: %+v", task)
	}
}

type recoveryContainer struct {
	id          string
	annotations map[string]string
}

func (c recoveryContainer) ID() string                        { return c.id }
func (c recoveryContainer) GetAnnotations() map[string]string { return c.annotations }
func (recoveryContainer) GetPid() int                         { return 0 }
func (recoveryContainer) Sandbox() cntr.SandboxTraits         { return nil }
func (recoveryContainer) GetMemoryLimit() uint64              { return 0 }
func (recoveryContainer) Status() cntr.StateString            { return cntr.StateDown }
func (recoveryContainer) State() *cntr.ContainerState {
	return &cntr.ContainerState{State: cntr.StateDown}
}
func (recoveryContainer) StateSnapshot() (cntr.ContainerState, error) {
	return cntr.ContainerState{}, nil
}
func (recoveryContainer) GetClientCPU() string { return "" }
func (recoveryContainer) SaveState() error     { return nil }
func (recoveryContainer) Signal(context.Context, syscall.Signal) error {
	return nil
}

func TestRecoveredTasksFromContainersAddsIndexContext(t *testing.T) {
	_, err := recoveredTasksFromContainers([]cntr.ContainerTraits{nil}, true)
	if err == nil || !strings.Contains(err.Error(), "recovered container[0]") {
		t.Fatalf("recoveredTasksFromContainers error = %v, want indexed context", err)
	}
}

func TestShimRecoveryBackendCleanupOrphansUsesConfiguredPaths(t *testing.T) {
	containersDir := t.TempDir()
	taskDirRoot := t.TempDir()
	namespace := "ns-test"
	activeID := "active"
	orphanID := "orphan"

	mustMkdirAll(t, filepath.Join(containersDir, activeID))
	mustMkdirAll(t, filepath.Join(containersDir, orphanID))
	mustMkdirAll(t, filepath.Join(taskDirRoot, namespace, activeID))

	backend := shimRecoveryBackend{
		containersDir: containersDir,
		taskDirRoot:   taskDirRoot,
	}
	if err := backend.CleanupOrphans(context.Background(), namespace); err != nil {
		t.Fatalf("CleanupOrphans returned unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(containersDir, activeID)); err != nil {
		t.Fatalf("expected active container dir to remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(containersDir, orphanID)); !os.IsNotExist(err) {
		t.Fatalf("expected orphan container dir to be removed, stat err: %v", err)
	}
}

func TestShimRecoveryBackendCleanupOrphansIgnoresMissingContainersDir(t *testing.T) {
	backend := shimRecoveryBackend{
		containersDir: filepath.Join(t.TempDir(), "missing"),
		taskDirRoot:   t.TempDir(),
	}

	if err := backend.CleanupOrphans(context.Background(), "ns-test"); err != nil {
		t.Fatalf("CleanupOrphans should ignore missing containers dir, got: %v", err)
	}
}

func TestShimRecoveryBackendCleanupOrphansAcceptsNilContext(t *testing.T) {
	backend := shimRecoveryBackend{
		containersDir: filepath.Join(t.TempDir(), "missing"),
		taskDirRoot:   t.TempDir(),
	}

	if err := backend.CleanupOrphans(nil, "ns-test"); err != nil {
		t.Fatalf("CleanupOrphans should accept nil context, got: %v", err)
	}
}

func TestShimRecoveryBackendCleanupOrphansHonorsCanceledContext(t *testing.T) {
	containersDir := t.TempDir()
	taskDirRoot := t.TempDir()
	orphanID := "orphan"
	mustMkdirAll(t, filepath.Join(containersDir, orphanID))

	backend := shimRecoveryBackend{
		containersDir: containersDir,
		taskDirRoot:   taskDirRoot,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := backend.CleanupOrphans(ctx, "ns-test")
	if err != context.Canceled {
		t.Fatalf("CleanupOrphans error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(filepath.Join(containersDir, orphanID)); err != nil {
		t.Fatalf("orphan directory should remain after canceled cleanup: %v", err)
	}
}

func TestShimRecoveryBackendCleanupOrphansReturnsErrorOnInvalidTaskDirRoot(t *testing.T) {
	containersDir := t.TempDir()
	taskDirRoot := filepath.Join(t.TempDir(), "not-dir")
	orphanID := "orphan"
	mustMkdirAll(t, filepath.Join(containersDir, orphanID))

	if err := os.WriteFile(taskDirRoot, []byte("blocked"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	backend := shimRecoveryBackend{
		containersDir: containersDir,
		taskDirRoot:   taskDirRoot,
	}

	err := backend.CleanupOrphans(context.Background(), "ns-test")
	if err == nil {
		t.Fatalf("expected cleanup error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to stat task directory") {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(containersDir, orphanID)); err != nil {
		t.Fatalf("orphan directory should remain after failed cleanup, stat error: %v", err)
	}
}

func TestShimRecoveryBackendCleanupOrphansRejectsUnsafeNamespace(t *testing.T) {
	backend := shimRecoveryBackend{
		containersDir: t.TempDir(),
		taskDirRoot:   t.TempDir(),
	}

	for _, namespace := range []string{"", " ns-test", "ns-test ", "../ns", `parent\ns`} {
		t.Run(namespace, func(t *testing.T) {
			if err := backend.CleanupOrphans(context.Background(), namespace); err == nil {
				t.Fatal("expected error for unsafe namespace")
			}
		})
	}
}

func TestShimRecoveryBackendCleanupOrphansRejectsUnsafeContainerDirName(t *testing.T) {
	containersDir := t.TempDir()
	unsafeID := "orphan "
	mustMkdirAll(t, filepath.Join(containersDir, unsafeID))

	backend := shimRecoveryBackend{
		containersDir: containersDir,
		taskDirRoot:   t.TempDir(),
	}

	err := backend.CleanupOrphans(context.Background(), "ns-test")
	if err == nil || !strings.Contains(err.Error(), "container id is invalid") {
		t.Fatalf("CleanupOrphans error = %v, want invalid container id", err)
	}
	if _, statErr := os.Stat(filepath.Join(containersDir, unsafeID)); statErr != nil {
		t.Fatalf("unsafe directory should remain after rejected cleanup: %v", statErr)
	}
}

func TestShimRecoveryBackendCleanupOrphansRejectsRelativeCleanupRoots(t *testing.T) {
	tests := []struct {
		name          string
		containersDir string
		taskDirRoot   string
		want          string
	}{
		{
			name:          "containers dir",
			containersDir: "relative-containers",
			taskDirRoot:   t.TempDir(),
			want:          "containers directory",
		},
		{
			name:          "task dir root",
			containersDir: t.TempDir(),
			taskDirRoot:   "relative-tasks",
			want:          "task directory root",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			backend := shimRecoveryBackend{
				containersDir: tc.containersDir,
				taskDirRoot:   tc.taskDirRoot,
			}

			err := backend.CleanupOrphans(context.Background(), "ns-test")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("CleanupOrphans error = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestShimRecoveryBackendRestoreHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	backend := shimRecoveryBackend{guestControl: stubGuestControl{}}

	_, _, err := backend.Restore(ctx, "sandbox1")
	if err != context.Canceled {
		t.Fatalf("Restore error = %v, want context.Canceled", err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("failed to create %s: %v", path, err)
	}
}
