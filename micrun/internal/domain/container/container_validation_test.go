package container

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateMicaContainerReportsUnsupportedOS(t *testing.T) {
	firmware := writeValidationFile(t, "firmware.elf")
	c := &Container{config: &ContainerConfig{
		OS:           "linux",
		ImageAbsPath: firmware,
	}}

	err := c.validateMicaContainer()
	if err == nil {
		t.Fatal("validateMicaContainer() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported guest os") {
		t.Fatalf("validateMicaContainer() error = %v, want unsupported os reason", err)
	}
	if !strings.Contains(err.Error(), "uniproton") || !strings.Contains(err.Error(), "zephyr") {
		t.Fatalf("validateMicaContainer() error = %v, want supported os list", err)
	}
}

func TestValidateMicaContainerReportsMissingFirmware(t *testing.T) {
	c := &Container{config: &ContainerConfig{
		OS:           "uniproton",
		ImageAbsPath: filepath.Join(t.TempDir(), "missing.elf"),
	}}

	err := c.validateMicaContainer()
	if err == nil {
		t.Fatal("validateMicaContainer() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid firmware path") {
		t.Fatalf("validateMicaContainer() error = %v, want firmware reason", err)
	}
}

func TestValidateMicaContainerReportsMissingXenImage(t *testing.T) {
	firmware := writeValidationFile(t, "firmware.elf")
	c := &Container{config: &ContainerConfig{
		OS:           "uniproton",
		ImageAbsPath: firmware,
		PedestalType: PedestalXen,
		PedestalConf: filepath.Join(t.TempDir(), "missing.bin"),
	}}

	err := c.validateMicaContainer()
	if err == nil {
		t.Fatal("validateMicaContainer() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid xen pedestal image") {
		t.Fatalf("validateMicaContainer() error = %v, want xen image reason", err)
	}
}

func TestValidateMicaContainerAllowsInfraWithoutAssets(t *testing.T) {
	c := &Container{config: &ContainerConfig{IsInfra: true}}
	if err := c.validateMicaContainer(); err != nil {
		t.Fatalf("validateMicaContainer() error = %v, want nil", err)
	}
}

func writeValidationFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("file"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
