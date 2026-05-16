package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"micrun/internal/ports"
)

func TestNewStore(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	if s == nil {
		t.Fatal("New() returned nil")
	}
	if s.root != root {
		t.Errorf("root = %q, want %q", s.root, root)
	}
}

func TestNewStoreTrimsRoot(t *testing.T) {
	root := t.TempDir()
	s := New(" \t" + root + " \n")
	if s.root != root {
		t.Errorf("root = %q, want trimmed %q", s.root, root)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ctx := context.Background()

	snapshot := &ports.RuntimeSnapshot{
		Namespace: "default",
		TaskID:    "task-123",
		Data:      []byte(`{"status":"running"}`),
	}

	if err := s.Save(ctx, snapshot); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := s.Load(ctx, "default", "task-123")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.Namespace != snapshot.Namespace {
		t.Errorf("Namespace = %q, want %q", loaded.Namespace, snapshot.Namespace)
	}
	if loaded.TaskID != snapshot.TaskID {
		t.Errorf("TaskID = %q, want %q", loaded.TaskID, snapshot.TaskID)
	}
	if string(loaded.Data) != string(snapshot.Data) {
		t.Errorf("Data = %q, want %q", string(loaded.Data), string(snapshot.Data))
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ctx := context.Background()

	snapshot := &ports.RuntimeSnapshot{
		Namespace: "ns",
		TaskID:    "tid",
		Data:      []byte("data"),
	}
	if err := s.Save(ctx, snapshot); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	expectedPath := filepath.Join(root, "ns", "tid", "runtime.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("file not created at %s", expectedPath)
	}
}

func TestLoadNonexistentReturnsError(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ctx := context.Background()

	_, err := s.Load(ctx, "missing", "no-task")
	if err == nil {
		t.Fatal("expected error for nonexistent snapshot")
	}
}

func TestSaveNilSnapshotReturnsError(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ctx := context.Background()

	if err := s.Save(ctx, nil); err == nil {
		t.Fatal("expected error for nil snapshot")
	}
}

func TestDeleteRemovesSnapshot(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ctx := context.Background()

	snapshot := &ports.RuntimeSnapshot{
		Namespace: "ns",
		TaskID:    "tid",
		Data:      []byte("data"),
	}
	if err := s.Save(ctx, snapshot); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if err := s.Delete(ctx, "ns", "tid"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	_, err := s.Load(ctx, "ns", "tid")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestDeleteNonexistentIsNoOp(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ctx := context.Background()

	if err := s.Delete(ctx, "ghost", "no-id"); err != nil {
		t.Fatalf("Delete() on nonexistent should not error: %v", err)
	}
}

func TestSnapshotPathFormat(t *testing.T) {
	s := New("/tmp/micrun")
	path := s.snapshotPath("default", "abc123")
	expected := filepath.Join("/tmp/micrun", "default", "abc123", "runtime.json")
	if path != expected {
		t.Errorf("snapshotPath = %q, want %q", path, expected)
	}
}

func TestStoreOperationsReturnCanceledContext(t *testing.T) {
	s := New(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.Load(ctx, "ns", "task"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load canceled error = %v, want context.Canceled", err)
	}
	if err := s.Save(ctx, &ports.RuntimeSnapshot{Namespace: "ns", TaskID: "task"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save canceled error = %v, want context.Canceled", err)
	}
	if err := s.Delete(ctx, "ns", "task"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete canceled error = %v, want context.Canceled", err)
	}
}

func TestStoreOperationsRejectInvalidRoot(t *testing.T) {
	for _, root := range []string{"", "relative/root"} {
		t.Run(root, func(t *testing.T) {
			s := New(root)
			ctx := context.Background()
			if _, err := s.Load(ctx, "ns", "task"); !errors.Is(err, ErrInvalidStateRoot) {
				t.Fatalf("Load invalid root err = %v, want ErrInvalidStateRoot", err)
			}
			if err := s.Save(ctx, &ports.RuntimeSnapshot{Namespace: "ns", TaskID: "task"}); !errors.Is(err, ErrInvalidStateRoot) {
				t.Fatalf("Save invalid root err = %v, want ErrInvalidStateRoot", err)
			}
			if err := s.Delete(ctx, "ns", "task"); !errors.Is(err, ErrInvalidStateRoot) {
				t.Fatalf("Delete invalid root err = %v, want ErrInvalidStateRoot", err)
			}
		})
	}
}

func TestLoadRejectsEmptyStateKey(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()

	_, err := s.Load(ctx, "", "task")
	if !errors.Is(err, ErrInvalidStateKey) {
		t.Fatalf("Load(empty namespace) err = %v, want ErrInvalidStateKey", err)
	}
	_, err = s.Load(ctx, "ns", "")
	if !errors.Is(err, ErrInvalidStateKey) {
		t.Fatalf("Load(empty taskID) err = %v, want ErrInvalidStateKey", err)
	}
}

func TestLoadRejectsInvalidPathStateKey(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()

	cases := []struct {
		namespace string
		taskID    string
	}{
		{"/ns", "task"},
		{"ns", "/task"},
		{"..", "task"},
		{"a/../b", "task"},
		{"ns", ".."},
	}
	for _, c := range cases {
		_, err := s.Load(ctx, c.namespace, c.taskID)
		if !errors.Is(err, ErrInvalidStateKey) {
			t.Fatalf("Load(%q, %q) err = %v, want ErrInvalidStateKey", c.namespace, c.taskID, err)
		}
	}
}

func TestLoadSupportsNestedPathKeys(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ctx := context.Background()

	if err := s.Save(ctx, &ports.RuntimeSnapshot{
		Namespace: "sandbox-a/container-a",
		TaskID:    "task-nested",
		Data:      []byte(`{"state":"running"}`),
	}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	_, err := s.Load(ctx, "sandbox-a/container-a", "task-nested")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
}

func TestLoadReturnsNormalizedSnapshotKeys(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ctx := context.Background()

	if err := s.Save(ctx, &ports.RuntimeSnapshot{
		Namespace: `runtime\container`,
		TaskID:    `sandbox-a\container-a`,
		Data:      []byte(`{"state":"running"}`),
	}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := s.Load(ctx, `runtime\container`, `sandbox-a\container-a`)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.Namespace != filepath.Join("runtime", "container") {
		t.Fatalf("Namespace = %q, want normalized path", loaded.Namespace)
	}
	if loaded.TaskID != filepath.Join("sandbox-a", "container-a") {
		t.Fatalf("TaskID = %q, want normalized path", loaded.TaskID)
	}
}

func TestSaveRejectsEmptyStateKey(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()

	err := s.Save(ctx, &ports.RuntimeSnapshot{Namespace: "", TaskID: "task"})
	if !errors.Is(err, ErrInvalidStateKey) {
		t.Fatalf("Save(empty namespace) err = %v, want ErrInvalidStateKey", err)
	}
	err = s.Save(ctx, &ports.RuntimeSnapshot{Namespace: "ns", TaskID: ""})
	if !errors.Is(err, ErrInvalidStateKey) {
		t.Fatalf("Save(empty taskID) err = %v, want ErrInvalidStateKey", err)
	}
}

func TestSaveRejectsInvalidPathStateKey(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()

	cases := []*ports.RuntimeSnapshot{
		{Namespace: "/sandbox", TaskID: "task", Data: []byte("data")},
		{Namespace: "ns", TaskID: "/task", Data: []byte("data")},
		{Namespace: "..", TaskID: "task", Data: []byte("data")},
		{Namespace: "a/../b", TaskID: "task", Data: []byte("data")},
		{Namespace: "ns", TaskID: "..", Data: []byte("data")},
	}
	for _, c := range cases {
		err := s.Save(ctx, c)
		if !errors.Is(err, ErrInvalidStateKey) {
			t.Fatalf("Save(%q, %q) err = %v, want ErrInvalidStateKey", c.Namespace, c.TaskID, err)
		}
	}
}

func TestDeleteRejectsEmptyStateKey(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()

	err := s.Delete(ctx, "", "task")
	if !errors.Is(err, ErrInvalidStateKey) {
		t.Fatalf("Delete(empty namespace) err = %v, want ErrInvalidStateKey", err)
	}
	err = s.Delete(ctx, "ns", "")
	if !errors.Is(err, ErrInvalidStateKey) {
		t.Fatalf("Delete(empty taskID) err = %v, want ErrInvalidStateKey", err)
	}
}

func TestDeleteRejectsInvalidPathStateKey(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()

	cases := [][2]string{
		{"/ns", "task"},
		{"ns", "/task"},
		{"..", "task"},
		{"a/../b", "task"},
		{"ns", ".."},
	}
	for _, c := range cases {
		err := s.Delete(ctx, c[0], c[1])
		if !errors.Is(err, ErrInvalidStateKey) {
			t.Fatalf("Delete(%q, %q) err = %v, want ErrInvalidStateKey", c[0], c[1], err)
		}
	}
}

func TestDeleteKeepsUnrelatedSnapshotFiles(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ctx := context.Background()

	taskDir := filepath.Join(root, "ns", "tid")
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "runtime.json"), []byte(`{"status":"running"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(runtime.json) error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile(keep.txt) error: %v", err)
	}

	if err := s.Delete(ctx, "ns", "tid"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(taskDir, "runtime.json")); !os.IsNotExist(err) {
		t.Fatalf("runtime.json should be removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(taskDir, "keep.txt")); err != nil {
		t.Fatalf("keep.txt should be kept, error=%v", err)
	}
}

func TestDeletePrunesEmptySnapshotDirectories(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ctx := context.Background()

	if err := s.Save(ctx, &ports.RuntimeSnapshot{
		Namespace: "sandbox-a/container-a",
		TaskID:    "task-a/exec-a",
		Data:      []byte("data"),
	}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if err := s.Delete(ctx, "sandbox-a/container-a", "task-a/exec-a"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "sandbox-a")); !os.IsNotExist(err) {
		t.Fatalf("empty namespace directories should be pruned, err=%v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("state root should remain: %v", err)
	}
}

func TestDeleteStopsPruningAtNonEmptyParent(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	ctx := context.Background()

	if err := s.Save(ctx, &ports.RuntimeSnapshot{
		Namespace: "sandbox-a/container-a",
		TaskID:    "task-a",
		Data:      []byte("data"),
	}); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	keepPath := filepath.Join(root, "sandbox-a", "keep.txt")
	if err := os.WriteFile(keepPath, []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile(keep.txt) error: %v", err)
	}

	if err := s.Delete(ctx, "sandbox-a/container-a", "task-a"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "sandbox-a", "container-a")); !os.IsNotExist(err) {
		t.Fatalf("empty container namespace should be pruned, err=%v", err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("non-empty parent should remain with keep file: %v", err)
	}
}

func TestWriteFileAtomicallyReplacesAndCleansTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime.json")

	if err := writeFileAtomically(path, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if err := writeFileAtomically(path, []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(data) != `{"v":2}` {
		t.Fatalf("runtime content = %q, want %q", string(data), `{"v":2}`)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("runtime mode = %v, want 0644", got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".runtime-") && strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temp file leak: %s", entry.Name())
		}
	}
}
