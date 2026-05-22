package container

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	defs "micrun/internal/support/definitions"

	"golang.org/x/sys/unix"
)

func TestSanitizeRPMSGClientName(t *testing.T) {
	got := sanitizeName("abc-DEF_012:/weird")
	if got != "abc-DEF_012__weird" {
		t.Fatalf("unexpected sanitized name: %q", got)
	}
}

func TestCandidateTTYs(t *testing.T) {
	paths := candidateTTYs("id:with/weird")
	if len(paths) != 3 {
		t.Fatalf("unexpected number of paths: %d", len(paths))
	}
	if paths[0] != "/dev/ttyRPMSG_id_with_weird_0" {
		t.Fatalf("unexpected first path: %q", paths[0])
	}
}

func TestBuildCandidateTTYsSkipsDuplicateAndEmptyRoots(t *testing.T) {
	paths := buildCandidateTTYs("demo", []string{"/dev", "", "/dev", "/run/micrun"})
	if len(paths) != 2 {
		t.Fatalf("unexpected number of paths: %d", len(paths))
	}
	if paths[0] != "/dev/ttyRPMSG_demo_0" {
		t.Fatalf("unexpected first path: %q", paths[0])
	}
	if paths[1] != "/run/micrun/ttyRPMSG_demo_0" {
		t.Fatalf("unexpected second path: %q", paths[1])
	}
}

func TestBuildCandidateTTYsRejectsEmptySanitizedName(t *testing.T) {
	if paths := buildCandidateTTYs("", defaultTTYDiscoveryRoots()); paths != nil {
		t.Fatalf("unexpected paths for empty id: %v", paths)
	}
}

func TestDefaultRPMSGTTYRootsUsesConfiguredStateDir(t *testing.T) {
	paths := DefaultRPMSGTTYRoots("  /custom/micrun  ")
	if len(paths) != 3 {
		t.Fatalf("unexpected number of paths: %d", len(paths))
	}
	if paths[0] != "/dev" || paths[1] != "/custom/micrun" || paths[2] != "/tmp/mica" {
		t.Fatalf("unexpected TTY discovery roots: %v", paths)
	}

	paths = DefaultRPMSGTTYRoots(" ")
	if paths[1] != defs.MicrunStateDir {
		t.Fatalf("blank state dir root = %q, want %q", paths[1], defs.MicrunStateDir)
	}

	paths = DefaultRPMSGTTYRoots("relative-state")
	if paths[1] != defs.MicrunStateDir {
		t.Fatalf("relative state dir root = %q, want %q", paths[1], defs.MicrunStateDir)
	}
}

func TestDialTTYReturnsCanceledContextBeforeWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, _, err := dialTTY(ctx, "demo")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dialTTY error = %v, want context.Canceled", err)
	}
}

func TestIsRetryableRPMSGOpenError(t *testing.T) {
	if !retryableOpenError(os.ErrNotExist) {
		t.Fatalf("expected not-exist to be retryable")
	}
	if !retryableOpenError(unix.ENXIO) {
		t.Fatalf("expected ENXIO to be retryable")
	}
	if retryableOpenError(errors.New("boom")) {
		t.Fatalf("unexpected retryable error")
	}
}

func TestRawRPMSGTermios(t *testing.T) {
	var termios unix.Termios
	termios.Iflag = unix.IGNBRK | unix.BRKINT | unix.PARMRK |
		unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL |
		unix.IXON | unix.IXANY | unix.IXOFF
	termios.Oflag = unix.OPOST | unix.ONLCR | unix.OCRNL | unix.OLCUC
	termios.Cflag = unix.CSIZE | unix.PARENB | unix.PARODD | unix.CSTOPB | unix.HUPCL
	termios.Lflag = unix.ICANON | unix.ECHO | unix.ECHOE | unix.ECHOK |
		unix.ECHOCTL | unix.ECHOKE | unix.ECHONL | unix.ISIG | unix.IEXTEN |
		unix.NOFLSH | unix.TOSTOP
	for i := range termios.Cc {
		termios.Cc[i] = 9
	}

	got := rawRPMSGTermios(termios)
	if got.Iflag != 0 {
		t.Fatalf("iflag = 0x%x, want 0", got.Iflag)
	}
	if got.Oflag != 0 {
		t.Fatalf("oflag = 0x%x, want 0", got.Oflag)
	}
	if got.Cflag&unix.CSIZE != unix.CS8 {
		t.Fatalf("cflag character size = 0x%x, want CS8", got.Cflag&unix.CSIZE)
	}
	if got.Cflag&unix.CREAD == 0 || got.Cflag&unix.CLOCAL == 0 {
		t.Fatalf("cflag missing CREAD/CLOCAL: 0x%x", got.Cflag)
	}
	if got.Cflag&unix.HUPCL == 0 {
		t.Fatalf("cflag should preserve unrelated flags: 0x%x", got.Cflag)
	}
	if got.Cflag&(unix.PARENB|unix.PARODD|unix.CSTOPB) != 0 {
		t.Fatalf("cflag preserved parity/stop bits: 0x%x", got.Cflag)
	}
	if got.Lflag != 0 {
		t.Fatalf("lflag = 0x%x, want 0", got.Lflag)
	}
	if got.Cc[unix.VMIN] != 1 {
		t.Fatalf("VMIN = %d, want 1", got.Cc[unix.VMIN])
	}
	if got.Cc[unix.VTIME] != 0 {
		t.Fatalf("VTIME = %d, want 0", got.Cc[unix.VTIME])
	}
	for i, cc := range got.Cc {
		if i == unix.VMIN || i == unix.VTIME {
			continue
		}
		if cc != 0 {
			t.Fatalf("cc[%d] = %d, want 0", i, cc)
		}
	}
}

func TestResolveSymlinkTarget(t *testing.T) {
	linkPath := filepath.Join(t.TempDir(), "link")
	absoluteTarget := filepath.Join(t.TempDir(), "tty")
	if got := resolveSymlinkTarget(linkPath, absoluteTarget); got != absoluteTarget {
		t.Fatalf("absolute target = %q, want %q", got, absoluteTarget)
	}

	want := filepath.Join(filepath.Dir(linkPath), "tty")
	if got := resolveSymlinkTarget(linkPath, "tty"); got != want {
		t.Fatalf("relative target = %q, want %q", got, want)
	}
}

func TestCleanupStaleSymlinkKeepsRelativeExistingTarget(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target")
	if err := os.WriteFile(targetPath, []byte("ok"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	linkPath := filepath.Join(dir, "link")
	if err := os.Symlink("target", linkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	}()

	cleanupStaleSymlink(linkPath)
	if _, err := os.Lstat(linkPath); err != nil {
		t.Fatalf("expected symlink to remain: %v", err)
	}
}

func TestCleanupStaleSymlinkRemovesRelativeMissingTarget(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, "link")
	if err := os.Symlink("missing", linkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	cleanupStaleSymlink(linkPath)
	if _, err := os.Lstat(linkPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stale symlink to be removed, got %v", err)
	}
}
