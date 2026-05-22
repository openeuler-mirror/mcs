package configstack

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectConfigFormat(t *testing.T) {
	cases := []struct {
		ext    string
		expect ConfigFormat
	}{
		{".ini", FormatINI},
		{".conf", FormatINI},
		{".toml", FormatTOML},
		{".json", FormatUnknown},
		{".yaml", FormatUnknown},
		{"", FormatUnknown},
	}
	for _, tc := range cases {
		name := "file" + tc.ext
		got := detectConfigFormat(name)
		if got != tc.expect {
			t.Errorf("detectConfigFormat(%q) = %v, want %v", name, got, tc.expect)
		}
	}
}

func TestFirstNonEmptyEnv(t *testing.T) {
	t.Run("first key wins", func(t *testing.T) {
		t.Setenv("TEST_KEY_A", "val-a")
		t.Setenv("TEST_KEY_B", "val-b")
		got := FirstNonEmptyEnv("TEST_KEY_A", "TEST_KEY_B")
		if got != "val-a" {
			t.Errorf("FirstNonEmptyEnv() = %q, want %q", got, "val-a")
		}
	})

	t.Run("all empty returns empty", func(t *testing.T) {
		got := FirstNonEmptyEnv("NONEXISTENT_KEY_XYZ_123")
		if got != "" {
			t.Errorf("FirstNonEmptyEnv() = %q, want empty", got)
		}
	})

	t.Run("second key when first empty", func(t *testing.T) {
		t.Setenv("TEST_KEY_B", "val-b")
		got := FirstNonEmptyEnv("NONEXISTENT_KEY_XYZ_123", "TEST_KEY_B")
		if got != "val-b" {
			t.Errorf("FirstNonEmptyEnv() = %q, want %q", got, "val-b")
		}
	})
}

func TestListMicrunConfigDirEmpty(t *testing.T) {
	files, err := listMicrunConfigDir("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected empty result for empty dir, got %d files", len(files))
	}
}

func TestListMicrunConfigDirNonexistent(t *testing.T) {
	files, err := listMicrunConfigDir("/nonexistent/path/xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent dir")
	}
	_ = files
}

func TestListMicrunConfigDirRejectsRelativePath(t *testing.T) {
	if _, err := listMicrunConfigDir("relative-configs"); err == nil {
		t.Fatal("expected error for relative config directory")
	}
}

func TestListMicrunConfigDirFiltersByExtension(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.ini"), []byte("[micrun]\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.conf"), []byte("[micrun]\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.toml"), []byte("[micrun]\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.json"), []byte("{}\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subdir"), 0755))

	files, err := listMicrunConfigDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("expected 3 files (ini, conf, toml), got %d: %v", len(files), files)
	}
	for _, f := range files {
		if f.Format == FormatUnknown {
			t.Errorf("file %q has unknown format", f.Path)
		}
	}
}

func TestListMicrunConfigDirSkipsNonRegularFiles(t *testing.T) {
	dir := t.TempDir()
	regularPath := filepath.Join(dir, "regular.ini")
	require.NoError(t, os.WriteFile(regularPath, []byte("[micrun]\n"), 0644))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "pipe.ini"), 0600))

	files, err := listMicrunConfigDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %+v, want only regular config", files)
	}
	if files[0].Path != regularPath {
		t.Fatalf("config path = %q, want %q", files[0].Path, regularPath)
	}
}

func TestListMicrunConfigDirTrimsPath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.ini"), []byte("[micrun]\n"), 0644))

	files, err := listMicrunConfigDir(" \t" + dir + " \n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0].Path != filepath.Join(dir, "app.ini") {
		t.Fatalf("files = %+v, want trimmed directory result", files)
	}
}

func TestMakeConfigFileValidINI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ini")
	require.NoError(t, os.WriteFile(path, []byte("[mica]\n"), 0644))

	f, err := makeConfigFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Format != FormatINI {
		t.Errorf("Format = %v, want FormatINI", f.Format)
	}
}

func TestMakeConfigFileTrimsPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ini")
	require.NoError(t, os.WriteFile(path, []byte("[mica]\n"), 0644))

	f, err := makeConfigFile(" \t" + path + " \n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Path != path {
		t.Fatalf("Path = %q, want trimmed %q", f.Path, path)
	}
}

func TestMakeConfigFileRejectsBlankPath(t *testing.T) {
	_, err := makeConfigFile(" \t ")
	if err == nil {
		t.Fatal("expected error for blank path")
	}
}

func TestMakeConfigFileRejectsRelativePath(t *testing.T) {
	_, err := makeConfigFile("relative.ini")
	if err == nil {
		t.Fatal("expected error for relative config path")
	}
}

func TestMakeConfigFileNonexistent(t *testing.T) {
	_, err := makeConfigFile("/nonexistent/file.ini")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want os.ErrNotExist", err)
	}
}

func TestMakeConfigFileDirectoryPath(t *testing.T) {
	dir := t.TempDir()
	_, err := makeConfigFile(dir)
	if err == nil {
		t.Fatal("expected error for directory path")
	}
}

func TestDiscoverMicrunConfigFilesWithEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "override.ini")
	require.NoError(t, os.WriteFile(path, []byte("[micrun]\n"), 0644))

	t.Setenv("MICRUN_CONF_FILE", path)
	defer os.Unsetenv("MICRUN_CONF_FILE")

	files, err := DiscoverMicrunConfigFiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != path {
		t.Errorf("Path = %q, want %q", files[0].Path, path)
	}
}

func TestDiscoverMicrunConfigFilesTrimsEnvPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "override.ini")
	require.NoError(t, os.WriteFile(path, []byte("[micrun]\n"), 0644))

	t.Setenv("MICRUN_CONF_FILE", "  "+path+"  ")

	files, err := DiscoverMicrunConfigFiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != path {
		t.Errorf("Path = %q, want trimmed %q", files[0].Path, path)
	}
}
