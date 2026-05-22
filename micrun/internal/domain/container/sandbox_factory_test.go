package container

import (
	"context"
	"strings"
	"testing"
)

func TestCreateSandboxFromConfigReturnsCreateError(t *testing.T) {
	_, err := createSandboxFromConfig(context.Background(), &SandboxConfig{})
	if err == nil {
		t.Fatal("createSandboxFromConfig() expected error for invalid sandbox config, got nil")
	}
	if !strings.Contains(err.Error(), "invalid sandbox configuration") {
		t.Fatalf("createSandboxFromConfig() error = %v, want invalid config error", err)
	}
}

func TestNewSandboxRequiresValidDependencies(t *testing.T) {
	_, err := newSandbox(context.Background(), SandboxConfig{
		ID:           "sandbox-invalid-deps",
		Dependencies: &Dependencies{},
	})
	if err == nil {
		t.Fatal("newSandbox() expected dependency validation error, got nil")
	}
	if !strings.Contains(err.Error(), "missing required dependencies") {
		t.Fatalf("newSandbox() error = %v, want dependency validation error", err)
	}
}

func TestNewSandboxRequiresNonNilStateStore(t *testing.T) {
	deps := stubDeps()
	_, err := newSandbox(context.Background(), SandboxConfig{
		ID:           "sandbox-nil-store",
		Dependencies: deps,
	})
	if err == nil {
		t.Fatal("newSandbox() expected state store validation error, got nil")
	}
	if !strings.Contains(err.Error(), "non-nil StateStore") {
		t.Fatalf("newSandbox() error = %v, want state store validation error", err)
	}
}
