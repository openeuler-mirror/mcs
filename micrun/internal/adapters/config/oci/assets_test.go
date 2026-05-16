package oci

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"micrun/internal/adapters/hypervisor/pedestal"
	ann "micrun/internal/support/annotations"
	defs "micrun/internal/support/definitions"
)

func TestCacheRegularFileCopiesRegularFile(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	sourcePath := filepath.Join(dir, "zephyr.elf")
	if err := os.WriteFile(sourcePath, []byte("firmware"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	got, err := cacheRegularFile(cacheDir, sourcePath)
	if err != nil {
		t.Fatalf("cacheRegularFile returned error: %v", err)
	}
	want := filepath.Join(cacheDir, "zephyr.elf")
	if got != want {
		t.Fatalf("cacheRegularFile() = %q, want %q", got, want)
	}
	content, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if string(content) != "firmware" {
		t.Fatalf("cached content = %q, want firmware", string(content))
	}
}

func TestCacheRegularFilePromotesCompleteTempFile(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	sourcePath := filepath.Join(dir, "boot.elf")
	if err := os.WriteFile(sourcePath, []byte("firmware"), 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}

	got, err := cacheRegularFile(cacheDir, sourcePath)
	if err != nil {
		t.Fatalf("cacheRegularFile returned error: %v", err)
	}

	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat cached file: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("cached mode = %v, want 0755", info.Mode().Perm())
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary cache file was left behind: %s", entry.Name())
		}
	}
}

func TestCacheRegularFileDoesNotTruncateAlreadyCachedSource(t *testing.T) {
	cacheDir := t.TempDir()
	sourcePath := filepath.Join(cacheDir, "zephyr.elf")
	if err := os.WriteFile(sourcePath, []byte("firmware"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	got, err := cacheRegularFile(cacheDir, sourcePath)
	if err != nil {
		t.Fatalf("cacheRegularFile returned error: %v", err)
	}
	if got != sourcePath {
		t.Fatalf("cacheRegularFile() = %q, want %q", got, sourcePath)
	}
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	if string(content) != "firmware" {
		t.Fatalf("source content = %q, want firmware", string(content))
	}
}

func TestCacheRegularFileDisambiguatesBasenameCollisions(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	firstSource := filepath.Join(dir, "first", "image.bin")
	secondSource := filepath.Join(dir, "second", "image.bin")
	if err := os.MkdirAll(filepath.Dir(firstSource), 0o755); err != nil {
		t.Fatalf("mkdir first source dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(secondSource), 0o755); err != nil {
		t.Fatalf("mkdir second source dir: %v", err)
	}
	if err := os.WriteFile(firstSource, []byte("pedestal"), 0o644); err != nil {
		t.Fatalf("write first source: %v", err)
	}
	if err := os.WriteFile(secondSource, []byte("firmware"), 0o644); err != nil {
		t.Fatalf("write second source: %v", err)
	}

	firstCached, err := cacheRegularFile(cacheDir, firstSource)
	if err != nil {
		t.Fatalf("cache first file: %v", err)
	}
	secondCached, err := cacheRegularFile(cacheDir, secondSource)
	if err != nil {
		t.Fatalf("cache second file: %v", err)
	}

	if firstCached == secondCached {
		t.Fatalf("expected distinct cache paths, got %q", firstCached)
	}
	firstContent, err := os.ReadFile(firstCached)
	if err != nil {
		t.Fatalf("read first cached file: %v", err)
	}
	if string(firstContent) != "pedestal" {
		t.Fatalf("first cached content = %q, want pedestal", string(firstContent))
	}
	secondContent, err := os.ReadFile(secondCached)
	if err != nil {
		t.Fatalf("read second cached file: %v", err)
	}
	if string(secondContent) != "firmware" {
		t.Fatalf("second cached content = %q, want firmware", string(secondContent))
	}
}

func TestCacheRegularFilePassesThroughUncacheablePaths(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}

	if got, err := cacheRegularFile(cacheDir, ""); err != nil || got != "" {
		t.Fatalf("empty path result = (%q, %v), want empty nil", got, err)
	}

	missing := filepath.Join(dir, "missing.elf")
	if got, err := cacheRegularFile(cacheDir, missing); err != nil || got != missing {
		t.Fatalf("missing path result = (%q, %v), want original nil", got, err)
	}

	if got, err := cacheRegularFile(cacheDir, dir); err != nil || got != dir {
		t.Fatalf("directory path result = (%q, %v), want original nil", got, err)
	}
}

func TestGetBundleImageFileRejectsTraversal(t *testing.T) {
	rootfs := filepath.Join(t.TempDir(), "rootfs")
	if err := os.MkdirAll(rootfs, 0o755); err != nil {
		t.Fatalf("mkdir rootfs: %v", err)
	}
	outside := filepath.Join(filepath.Dir(rootfs), "outside.elf")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	for _, input := range []string{"../outside.elf", "images/../outside.elf", "/../outside.elf"} {
		t.Run(input, func(t *testing.T) {
			if got := getBundleImageFile(rootfs, input); got != "" {
				t.Fatalf("getBundleImageFile(%q) = %q, want empty", input, got)
			}
		})
	}
}

func TestGetBundleImageFileRejectsSymlinkEscape(t *testing.T) {
	rootfs := filepath.Join(t.TempDir(), "rootfs")
	if err := os.MkdirAll(rootfs, 0o755); err != nil {
		t.Fatalf("mkdir rootfs: %v", err)
	}
	outside := filepath.Join(filepath.Dir(rootfs), "outside.elf")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(rootfs, "escape.elf")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	if got := getBundleImageFile(rootfs, "escape.elf"); got != "" {
		t.Fatalf("getBundleImageFile through symlink = %q, want empty", got)
	}
}

func TestGetBundleImageFileAcceptsAbsoluteRootfsRelativePath(t *testing.T) {
	rootfs := filepath.Join(t.TempDir(), "rootfs")
	if err := os.MkdirAll(filepath.Join(rootfs, "images"), 0o755); err != nil {
		t.Fatalf("mkdir rootfs images: %v", err)
	}
	firmware := filepath.Join(rootfs, "images", "app.elf")
	if err := os.WriteFile(firmware, []byte("firmware"), 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}

	if got := getBundleImageFile(rootfs, "/images/app.elf"); got != firmware {
		t.Fatalf("getBundleImageFile = %q, want %q", got, firmware)
	}
}

func TestPrepCacheUsesRuntimeStateDir(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	pedconf := filepath.Join(dir, "xen.img")
	firmware := filepath.Join(dir, "zephyr.elf")
	if err := os.WriteFile(pedconf, []byte("pedestal"), 0o644); err != nil {
		t.Fatalf("write pedestal: %v", err)
	}
	if err := os.WriteFile(firmware, []byte("firmware"), 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}

	gotPedconf, gotFirmware, err := prepCache("container-a", pedconf, firmware, HostProfile{Type: pedestal.Xen}, stateDir)
	if err != nil {
		t.Fatalf("prepCache returned error: %v", err)
	}

	cacheDir := filepath.Join(stateDir, "containers", "container-a")
	if filepath.Dir(gotPedconf) != cacheDir {
		t.Fatalf("pedestal cache dir = %q, want %q", filepath.Dir(gotPedconf), cacheDir)
	}
	if filepath.Dir(gotFirmware) != cacheDir {
		t.Fatalf("firmware cache dir = %q, want %q", filepath.Dir(gotFirmware), cacheDir)
	}
	if _, err := os.Stat(gotPedconf); err != nil {
		t.Fatalf("expected cached pedestal: %v", err)
	}
	if _, err := os.Stat(gotFirmware); err != nil {
		t.Fatalf("expected cached firmware: %v", err)
	}
}

func TestContainerCacheRootUsesAbsoluteStateDirOrDefault(t *testing.T) {
	if got := ContainerCacheRoot(""); got != defs.DefaultMicaContainersRoot {
		t.Fatalf("ContainerCacheRoot(empty) = %q, want %q", got, defs.DefaultMicaContainersRoot)
	}
	if got := ContainerCacheRoot("  "); got != defs.DefaultMicaContainersRoot {
		t.Fatalf("ContainerCacheRoot(blank) = %q, want %q", got, defs.DefaultMicaContainersRoot)
	}

	stateDir := filepath.Join(t.TempDir(), "state")
	want := filepath.Join(stateDir, "containers")
	if got := ContainerCacheRoot(stateDir); got != want {
		t.Fatalf("ContainerCacheRoot(abs) = %q, want %q", got, want)
	}
}

func TestPrepCacheRejectsRelativeStateDir(t *testing.T) {
	dir := t.TempDir()
	firmware := filepath.Join(dir, "zephyr.elf")
	if err := os.WriteFile(firmware, []byte("firmware"), 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}

	_, _, err := prepCache("container-a", "", firmware, HostProfile{Type: pedestal.Xen}, "relative-state")
	if err == nil || !strings.Contains(err.Error(), "path must be absolute") {
		t.Fatalf("prepCache error = %v, want absolute cache root error", err)
	}
}

func TestVerifyContainerFirmwareHash(t *testing.T) {
	dir := t.TempDir()
	firmware := filepath.Join(dir, "firmware.elf")
	content := []byte("firmware")
	if err := os.WriteFile(firmware, content, 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}
	sum := sha256.Sum256(content)
	expected := "sha256:" + strings.ToUpper(hex.EncodeToString(sum[:]))

	if err := verifyContainerFirmwareHash(firmware, map[string]string{ann.FirmwareHash: expected}); err != nil {
		t.Fatalf("verifyContainerFirmwareHash returned error: %v", err)
	}
	if err := verifyContainerFirmwareHash(firmware, nil); err != nil {
		t.Fatalf("verifyContainerFirmwareHash without annotation returned error: %v", err)
	}
}

func TestVerifyContainerFirmwareHashRejectsMismatch(t *testing.T) {
	dir := t.TempDir()
	firmware := filepath.Join(dir, "firmware.elf")
	if err := os.WriteFile(firmware, []byte("firmware"), 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}
	wrong := strings.Repeat("0", sha256.Size*2)

	err := verifyContainerFirmwareHash(firmware, map[string]string{ann.FirmwareHash: wrong})
	if err == nil || !strings.Contains(err.Error(), "firmware sha256 mismatch") {
		t.Fatalf("verifyContainerFirmwareHash error = %v, want mismatch", err)
	}
}
