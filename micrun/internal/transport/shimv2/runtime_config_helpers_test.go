package shim

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	oci "micrun/internal/adapters/config/oci"
	runtimecfg "micrun/internal/adapters/config/runtimeconfig"
	"micrun/internal/ports"
	defs "micrun/internal/support/definitions"
)

func TestLoadRuntimeConfigRejectsNilService(t *testing.T) {
	_, err := loadRuntimeConfig(nil, ports.TaskCreateRequest{}, nil)
	if err == nil || !strings.Contains(err.Error(), "shim service") {
		t.Fatalf("loadRuntimeConfig error = %v, want shim service error", err)
	}
}

func TestLoadRuntimeConfigReusesCurrentConfig(t *testing.T) {
	current := oci.NewRuntimeConfigWithHost(oci.HostProfile{})
	service := &shimService{config: current, configFromCreate: true}

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

// The daemon's applyHostRuntimeConfig pre-sets s.config to a host-only
// baseline (configFromCreate=false). The first Create must not short-circuit
// on it: its pod annotations/options have to be overlaid, or documented
// per-workload runtime settings silently fall back to host defaults.
func TestLoadRuntimeConfigOverlaysAnnotationsOnHostBaseline(t *testing.T) {
	stateDir := t.TempDir()
	confPath := filepath.Join(t.TempDir(), "micrun.toml")
	if err := os.WriteFile(confPath, []byte("[mica]\nstate_dir = \""+stateDir+"\"\n"), 0o644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	t.Setenv(defs.MicrunConfEnv, confPath)

	baseline := oci.NewRuntimeConfigWithHost(oci.HostProfile{})
	baseline.StateDir = stateDir
	service := &shimService{
		config: baseline, // as set by applyHostRuntimeConfig at daemon startup
		runtimeDeps: runtimeDependencies{
			runtimeResolver: runtimecfg.NewResolver(oci.HostProfile{}),
			containerDeps:   buildContainerDependencies(testRuntimeEnvironment()),
		},
	}

	annotations := map[string]string{defs.RuntimeDebug: "true"}
	cfg, err := loadRuntimeConfig(service, ports.TaskCreateRequest{}, annotations)
	if err != nil {
		t.Fatalf("loadRuntimeConfig returned error: %v", err)
	}
	if cfg == baseline {
		t.Fatal("loadRuntimeConfig short-circuited on the host baseline")
	}
	if !cfg.Debug {
		t.Fatal("annotation debug=true was not overlaid on the host baseline")
	}
	if !service.configFromCreate {
		t.Fatal("configFromCreate must be set after a Create-time resolution")
	}
	if service.config != cfg {
		t.Fatal("service config not updated to the Create-resolved config")
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
		config:           current,
		configFromCreate: true,
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
		config:           current,
		configFromCreate: true,
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
		config:           current,
		configFromCreate: true,
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
	service := &shimService{config: current, configFromCreate: true}

	_, err := loadRuntimeConfig(service, ports.TaskCreateRequest{}, nil)
	if err == nil || !strings.Contains(err.Error(), "state directory") {
		t.Fatalf("loadRuntimeConfig error = %v, want state directory error", err)
	}
}
