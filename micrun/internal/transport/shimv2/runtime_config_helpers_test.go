package shim

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	oci "micrun/internal/adapters/config/oci"
	"micrun/internal/ports"
)

func TestLoadRuntimeConfigRejectsNilService(t *testing.T) {
	_, err := loadRuntimeConfig(nil, ports.TaskCreateRequest{}, nil)
	if err == nil || !strings.Contains(err.Error(), "shim service") {
		t.Fatalf("loadRuntimeConfig error = %v, want shim service error", err)
	}
}

func TestLoadRuntimeConfigReusesCurrentConfig(t *testing.T) {
	current := oci.NewRuntimeConfigWithHost(oci.HostProfile{})
	service := &shimService{config: current}

	cfg, err := loadRuntimeConfig(service, ports.TaskCreateRequest{}, nil)
	if err != nil {
		t.Fatalf("loadRuntimeConfig returned error: %v", err)
	}
	if cfg != current {
		t.Fatal("loadRuntimeConfig did not reuse current config")
	}
	if service.config != current {
		t.Fatal("loadRuntimeConfig did not keep service config in sync")
	}
}

func TestSetupStateDirRejectsRelativePath(t *testing.T) {
	err := setupStateDir("relative-state")
	if err == nil || !strings.Contains(err.Error(), "path must be absolute") {
		t.Fatalf("setupStateDir error = %v, want absolute path error", err)
	}
}

func TestSetupStateDirRejectsRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(path, []byte("file"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err := setupStateDir(path)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("setupStateDir error = %v, want directory error", err)
	}
}

func TestNormalizeRuntimeStateDirCleansAbsolutePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "..", "runtime")

	got, err := normalizeRuntimeStateDir(path)
	if err != nil {
		t.Fatalf("normalizeRuntimeStateDir returned error: %v", err)
	}
	if got != filepath.Clean(path) {
		t.Fatalf("normalizeRuntimeStateDir = %q, want %q", got, filepath.Clean(path))
	}
}

func TestLoadRuntimeConfigAppliesStateDirToStateStore(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "runtime-state")
	current := oci.NewRuntimeConfigWithHost(oci.HostProfile{})
	current.StateDir = stateDir
	service := &shimService{
		config: current,
		runtimeDeps: runtimeDependencies{
			containerDeps: buildContainerDependencies(testRuntimeEnvironment()),
		},
	}

	_, err := loadRuntimeConfig(service, ports.TaskCreateRequest{}, nil)
	if err != nil {
		t.Fatalf("loadRuntimeConfig returned error: %v", err)
	}
	if info, err := os.Stat(stateDir); err != nil || !info.IsDir() {
		t.Fatalf("expected configured StateDir to be created, info=%v err=%v", info, err)
	}

	store := service.runtimeDeps.containerDeps.StateStoreFactory()
	err = store.Save(context.Background(), &ports.RuntimeSnapshot{
		Namespace: "runtime/test",
		TaskID:    "task",
		Data:      []byte("state"),
	})
	if err != nil {
		t.Fatalf("state store save returned error: %v", err)
	}

	path := filepath.Join(stateDir, "runtime/test", "task", "runtime.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected state snapshot under configured StateDir: %v", err)
	}
}

func TestLoadRuntimeConfigAppliesStateDirToTTYDiscoveryRoots(t *testing.T) {
	stateDir := t.TempDir()
	current := oci.NewRuntimeConfigWithHost(oci.HostProfile{})
	current.StateDir = stateDir
	service := &shimService{
		config: current,
		runtimeDeps: runtimeDependencies{
			containerDeps: buildContainerDependencies(testRuntimeEnvironment()),
		},
	}

	_, err := loadRuntimeConfig(service, ports.TaskCreateRequest{}, nil)
	if err != nil {
		t.Fatalf("loadRuntimeConfig returned error: %v", err)
	}

	roots := service.runtimeDeps.containerDeps.TTYDiscoveryRoots()
	for _, root := range roots {
		if root == stateDir {
			return
		}
	}
	t.Fatalf("TTY discovery roots = %v, want state dir %q", roots, stateDir)
}

func TestLoadRuntimeConfigRejectsRelativeStateDir(t *testing.T) {
	current := oci.NewRuntimeConfigWithHost(oci.HostProfile{})
	current.StateDir = "relative-state"
	service := &shimService{
		config: current,
		runtimeDeps: runtimeDependencies{
			containerDeps: buildContainerDependencies(testRuntimeEnvironment()),
		},
	}

	_, err := loadRuntimeConfig(service, ports.TaskCreateRequest{}, nil)
	if err == nil || !strings.Contains(err.Error(), "state directory") {
		t.Fatalf("loadRuntimeConfig error = %v, want state directory error", err)
	}
}

func TestLoadRuntimeConfigRejectsRelativeStateDirWithoutContainerDeps(t *testing.T) {
	current := oci.NewRuntimeConfigWithHost(oci.HostProfile{})
	current.StateDir = "relative-state"
	service := &shimService{config: current}

	_, err := loadRuntimeConfig(service, ports.TaskCreateRequest{}, nil)
	if err == nil || !strings.Contains(err.Error(), "state directory") {
		t.Fatalf("loadRuntimeConfig error = %v, want state directory error", err)
	}
}
