package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	cdtypes "github.com/containerd/containerd/api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTravelDir(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "traveldir_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	dir1 := filepath.Join(tempDir, "dir1")
	dir2 := filepath.Join(dir1, "dir2")
	err = os.MkdirAll(dir2, 0755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(dir1, "file1.txt"), []byte("file1"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir2, "file2.txt"), []byte("file2"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tempDir, "file3.txt"), []byte("file3"), 0o644)
	require.NoError(t, err)

	err = TravelDir(tempDir)
	assert.NoError(t, err)

	tree, err := DirectoryTree(tempDir)
	require.NoError(t, err)
	if !strings.Contains(tree, "├── dir1/") {
		t.Fatalf("DirectoryTree() = %q, want top-level directory prefix", tree)
	}
	if !strings.Contains(tree, "│   ├── file1.txt") {
		t.Fatalf("DirectoryTree() = %q, want nested file prefix", tree)
	}

	err = TravelDir(filepath.Join(tempDir, "non_existent_dir"))
	assert.Error(t, err)

	symlinkPath := filepath.Join(dir2, "loop_link")
	err = os.Symlink(".", symlinkPath)
	require.NoError(t, err)

	err = TravelDir(tempDir)
	assert.NoError(t, err, "TravelDir should not get stuck in a symlink loop")
}

func TestRemoveContainerCacheDirAt(t *testing.T) {
	tempDir := t.TempDir()
	id := "container-1"
	cacheDir := filepath.Join(tempDir, id)
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "artifact"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	if err := RemoveContainerCacheDirAt(tempDir, id); err != nil {
		t.Fatalf("RemoveContainerCacheDirAt() error: %v", err)
	}

	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatalf("cache directory should be removed, err=%v", err)
	}
}

func TestSyncDir(t *testing.T) {
	if err := SyncDir(t.TempDir()); err != nil {
		t.Fatalf("SyncDir() error: %v", err)
	}
}

func TestSyncDirReturnsOpenError(t *testing.T) {
	err := SyncDir(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestCleanAbsolutePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "..", "state")
	got, err := CleanAbsolutePath(root)
	if err != nil {
		t.Fatalf("CleanAbsolutePath() error: %v", err)
	}
	if want := filepath.Clean(root); got != want {
		t.Fatalf("CleanAbsolutePath() = %q, want %q", got, want)
	}
}

func TestCleanAbsolutePathRejectsInvalidInput(t *testing.T) {
	tests := []string{"", " /tmp", "/tmp ", "/tmp/state\x00dir", "relative/path"}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			if _, err := CleanAbsolutePath(path); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestEnsureDirCleansAbsolutePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "..", "runtime")
	if err := EnsureDir(path, 0o755); err != nil {
		t.Fatalf("EnsureDir() error: %v", err)
	}
	if info, err := os.Stat(filepath.Clean(path)); err != nil || !info.IsDir() {
		t.Fatalf("expected clean directory to exist, info=%v err=%v", info, err)
	}
}

func TestSyncDirRejectsRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	err := SyncDir(path)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("SyncDir() error = %v, want not a directory", err)
	}
}

func TestRemoveContainerCacheDirRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		root string
		id   string
	}{
		{
			name: "empty root",
			root: "",
			id:   "container-1",
		},
		{
			name: "whitespace padded root",
			root: " /tmp",
			id:   "container-1",
		},
		{
			name: "relative root",
			root: "tmp/cache",
			id:   "container-1",
		},
		{
			name: "empty id",
			root: "/tmp",
			id:   "",
		},
		{
			name: "whitespace padded id",
			root: "/tmp",
			id:   "container-1 ",
		},
		{
			name: "path traversal",
			root: "/tmp",
			id:   "../container",
		},
		{
			name: "backslash separator",
			root: "/tmp",
			id:   `parent\container`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := RemoveContainerCacheDirAt(tc.root, tc.id); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestMountDirsRejectsInvalidRequestBeforeMount(t *testing.T) {
	regularFile := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(regularFile, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	tests := []struct {
		name    string
		mounts  []*cdtypes.Mount
		dest    string
		wantErr string
	}{
		{
			name:    "empty destination",
			mounts:  []*cdtypes.Mount{{}},
			dest:    "",
			wantErr: "path cannot be empty",
		},
		{
			name:    "relative destination",
			mounts:  []*cdtypes.Mount{{}},
			dest:    "relative/rootfs",
			wantErr: "path must be absolute",
		},
		{
			name:    "nil mount",
			mounts:  []*cdtypes.Mount{nil},
			dest:    t.TempDir(),
			wantErr: "mount 0 is nil",
		},
		{
			name:    "destination is regular file",
			mounts:  []*cdtypes.Mount{{}},
			dest:    regularFile,
			wantErr: "not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := MountDirs(tt.mounts, tt.dest)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("MountDirs() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
