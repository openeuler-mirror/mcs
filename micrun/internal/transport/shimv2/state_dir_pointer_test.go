package shim

import (
	"os"
	"path/filepath"
	"testing"

	defs "micrun/internal/support/definitions"
)

func writePointer(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write pointer: %v", err)
	}
}

func TestSyncStateDirPointerRecordsCustomDir(t *testing.T) {
	pointer := filepath.Join(t.TempDir(), "state-dir")
	custom := t.TempDir()

	syncStateDirPointerAt(pointer, custom)

	got, ok := readStateDirPointerAt(pointer)
	if !ok {
		t.Fatal("pointer not readable after syncing a custom state dir")
	}
	if got != custom {
		t.Fatalf("pointer = %q, want %q", got, custom)
	}
}

// A default-dir binding must NOT clear the pointer. On a mixed node the
// default binding's Create runs in a different shim than the custom
// state_dir workload; removing the pointer here would make the custom
// workload's next shim restart fall back to the default root and lose its
// task + Xen domain (regression: single-slot pointer cross-destruction).
func TestSyncStateDirPointerKeepsPointerOnDefaultBinding(t *testing.T) {
	pointer := filepath.Join(t.TempDir(), "state-dir")
	custom := t.TempDir()
	writePointer(t, pointer, custom+"\n")

	syncStateDirPointerAt(pointer, defs.MicrunStateDir)
	syncStateDirPointerAt(pointer, "")

	got, ok := readStateDirPointerAt(pointer)
	if !ok || got != custom {
		t.Fatalf("pointer after default binding = (%q, %v), want recorded (%q, true)", got, ok, custom)
	}
}

func TestSyncStateDirPointerMissingIsNotAnError(t *testing.T) {
	pointer := filepath.Join(t.TempDir(), "state-dir")
	// A default binding with no pointer present must be a silent no-op.
	syncStateDirPointerAt(pointer, defs.MicrunStateDir)
	syncStateDirPointerAt(pointer, "")
}

// The pointer must land via tmp+rename so a crash mid-write can never leave
// a truncated target that recovery would treat as garbage.
func TestSyncStateDirPointerWriteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	pointer := filepath.Join(dir, "state-dir")
	custom := t.TempDir()

	syncStateDirPointerAt(pointer, custom)

	got, ok := readStateDirPointerAt(pointer)
	if !ok || got != custom {
		t.Fatalf("pointer = (%q, %v), want (%q, true)", got, ok, custom)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "state-dir" {
		t.Fatalf("leftover temp files next to pointer: %v", entries)
	}
}

func TestAdoptStateDirPointerPrefersExplicitStateDir(t *testing.T) {
	pointer := filepath.Join(t.TempDir(), "state-dir")
	pointed := t.TempDir()
	writePointer(t, pointer, pointed+"\n")

	explicit := t.TempDir()
	if got := adoptStateDirPointerAt(pointer, explicit); got != explicit {
		t.Fatalf("adopt = %q, want explicit %q (host config must win)", got, explicit)
	}
}

func TestAdoptStateDirPointerAdoptsRecordedDir(t *testing.T) {
	pointer := filepath.Join(t.TempDir(), "state-dir")
	pointed := t.TempDir()
	writePointer(t, pointer, pointed+"\n")

	if got := adoptStateDirPointerAt(pointer, ""); got != pointed {
		t.Fatalf("adopt = %q, want recorded %q", got, pointed)
	}
	if got := adoptStateDirPointerAt(pointer, defs.MicrunStateDir); got != pointed {
		t.Fatalf("adopt(default) = %q, want recorded %q", got, pointed)
	}
}

func TestAdoptStateDirPointerWithoutPointerFallsBackToDefault(t *testing.T) {
	pointer := filepath.Join(t.TempDir(), "state-dir")
	if got := adoptStateDirPointerAt(pointer, ""); got != defs.MicrunStateDir {
		t.Fatalf("adopt = %q, want default %q", got, defs.MicrunStateDir)
	}
}

func TestAdoptStateDirPointerIgnoresGarbage(t *testing.T) {
	pointer := filepath.Join(t.TempDir(), "state-dir")
	writePointer(t, pointer, "   \n")
	if got := adoptStateDirPointerAt(pointer, ""); got != defs.MicrunStateDir {
		t.Fatalf("adopt(blank) = %q, want default", got)
	}

	writePointer(t, pointer, "not/an/absolute/path\n")
	if got := adoptStateDirPointerAt(pointer, ""); got != defs.MicrunStateDir {
		t.Fatalf("adopt(relative) = %q, want default", got)
	}

	writePointer(t, pointer, defs.MicrunStateDir+"\n")
	if got := adoptStateDirPointerAt(pointer, ""); got != defs.MicrunStateDir {
		t.Fatalf("adopt(pointer-to-default) = %q, want default", got)
	}
}

// The regression for scan 2.5 end-to-end: binding a custom state dir via
// configureRuntimePaths (what the first Create does with a CRI ConfigPath)
// must leave a pointer a restarted shim can adopt with only host config in
// hand. Skipped when the default state root is not writable (non-root
// sandbox); the At()-variant tests cover the mechanics regardless.
func TestConfigureRuntimePathsLeavesAdoptablePointer(t *testing.T) {
	pointer := stateDirPointerPath()
	if err := os.MkdirAll(filepath.Dir(pointer), 0o755); err != nil {
		t.Skipf("default state root not writable in this environment: %v", err)
	}
	restored := func() { _ = os.Remove(pointer) }
	restored()
	t.Cleanup(restored)

	custom := t.TempDir()
	if err := configureRuntimePaths(nil, custom); err != nil {
		t.Fatalf("configureRuntimePaths(custom) returned error: %v", err)
	}

	if got, ok := readStateDirPointerAt(pointer); !ok || got != custom {
		t.Fatalf("pointer after binding custom dir = (%q, %v), want (%q, true)", got, ok, custom)
	}
	// A restarted shim with host config leaving state_dir default adopts it.
	if got := adoptStateDirPointer(""); got != custom {
		t.Fatalf("adopt after binding = %q, want %q", got, custom)
	}
}
