package runtimeconfig

import (
	"os"
	"path/filepath"
	"testing"

	"micrun/internal/adapters/config/oci"
	"micrun/internal/ports"
	ann "micrun/internal/support/annotations"
	defs "micrun/internal/support/definitions"

	crioption "github.com/containerd/containerd/pkg/runtimeoptions/v1"
	"github.com/containerd/typeurl/v2"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestHasRuntimeOptionsTreatsTypedNilAsAbsent(t *testing.T) {
	var raw *anypb.Any
	if hasRuntimeOptions(raw) {
		t.Fatal("expected typed nil Any to be treated as absent")
	}
}

func TestResolverResolveIgnoresTypedNilOptions(t *testing.T) {
	var raw *anypb.Any

	cfg, err := Resolver{}.Resolve(nil, ports.TaskCreateRequest{
		Options: raw,
	}, nil)
	if err != nil {
		t.Fatalf("Resolve returned unexpected error for typed nil options: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected Resolve to return a config")
	}
}

func TestResolveConfigPathCandidatePrecedence(t *testing.T) {
	options, err := typeurl.MarshalAny(&crioption.Options{ConfigPath: "/options/micrun.toml"})
	if err != nil {
		t.Fatalf("marshal runtime options: %v", err)
	}
	t.Setenv(defs.MicrunConfEnv, "/env/micrun.toml")

	candidate, err := resolveConfigPathCandidate(ports.TaskCreateRequest{Options: options}, map[string]string{
		ann.SandboxConfigPathKey: "/annotation/micrun.toml",
	})
	if err != nil {
		t.Fatalf("resolveConfigPathCandidate returned error: %v", err)
	}
	if candidate.Path != "/annotation/micrun.toml" || candidate.Source != configPathSourceAnnotation {
		t.Fatalf("candidate = %+v, want annotation path", candidate)
	}
}

func TestResolveConfigPathCandidateTrimsAnnotationPath(t *testing.T) {
	candidate, err := resolveConfigPathCandidate(ports.TaskCreateRequest{}, map[string]string{
		ann.SandboxConfigPathKey: "  /annotation/micrun.toml  ",
	})
	if err != nil {
		t.Fatalf("resolveConfigPathCandidate returned error: %v", err)
	}
	if candidate.Path != "/annotation/micrun.toml" || candidate.Source != configPathSourceAnnotation {
		t.Fatalf("candidate = %+v, want trimmed annotation path", candidate)
	}
}

func TestResolveConfigPathCandidateIgnoresBlankCandidates(t *testing.T) {
	t.Setenv(defs.MicrunConfEnv, "  ")

	candidate, err := resolveConfigPathCandidate(ports.TaskCreateRequest{}, map[string]string{
		ann.SandboxConfigPathKey: "  ",
	})
	if err != nil {
		t.Fatalf("resolveConfigPathCandidate returned error: %v", err)
	}
	if candidate.Found() {
		t.Fatalf("candidate = %+v, want not found for blank values", candidate)
	}
}

func TestResolveConfigPathCandidateUsesOptionsBeforeEnv(t *testing.T) {
	options, err := typeurl.MarshalAny(&crioption.Options{ConfigPath: "/options/micrun.toml"})
	if err != nil {
		t.Fatalf("marshal runtime options: %v", err)
	}
	t.Setenv(defs.MicrunConfEnv, "/env/micrun.toml")

	candidate, err := resolveConfigPathCandidate(ports.TaskCreateRequest{Options: options}, nil)
	if err != nil {
		t.Fatalf("resolveConfigPathCandidate returned error: %v", err)
	}
	if candidate.Path != "/options/micrun.toml" || candidate.Source != configPathSourceOptions {
		t.Fatalf("candidate = %+v, want options path", candidate)
	}
}

func TestResolveConfigPathCandidateUsesEnvFallback(t *testing.T) {
	t.Setenv(defs.MicrunConfEnv, "/env/micrun.toml")

	candidate, err := resolveConfigPathCandidate(ports.TaskCreateRequest{}, nil)
	if err != nil {
		t.Fatalf("resolveConfigPathCandidate returned error: %v", err)
	}
	if candidate.Path != "/env/micrun.toml" || candidate.Source != configPathSourceEnv {
		t.Fatalf("candidate = %+v, want env path", candidate)
	}
}

func TestFirstConfigPathCandidateUsesFirstNonBlankResolver(t *testing.T) {
	candidate, err := firstConfigPathCandidate([]configPathResolver{
		{
			Source: configPathSourceAnnotation,
			Resolve: func() (string, error) {
				return "  ", nil
			},
		},
		{
			Source: configPathSourceOptions,
			Resolve: func() (string, error) {
				return " /options/micrun.toml ", nil
			},
		},
		{
			Source: configPathSourceEnv,
			Resolve: func() (string, error) {
				t.Fatal("resolver should stop after first non-blank path")
				return "", nil
			},
		},
	})
	if err != nil {
		t.Fatalf("firstConfigPathCandidate returned error: %v", err)
	}
	if candidate.Path != "/options/micrun.toml" || candidate.Source != configPathSourceOptions {
		t.Fatalf("candidate = %+v, want trimmed options path", candidate)
	}
}

func TestFirstConfigPathCandidateSkipsNilResolvers(t *testing.T) {
	candidate, err := firstConfigPathCandidate([]configPathResolver{
		{Source: configPathSourceAnnotation},
		{
			Source: configPathSourceEnv,
			Resolve: func() (string, error) {
				return "/env/micrun.toml", nil
			},
		},
	})
	if err != nil {
		t.Fatalf("firstConfigPathCandidate returned error: %v", err)
	}
	if candidate.Path != "/env/micrun.toml" || candidate.Source != configPathSourceEnv {
		t.Fatalf("candidate = %+v, want env path", candidate)
	}
}

type legacyCRIGetter struct {
	configPath string
}

func (l legacyCRIGetter) GetConfigPath() string {
	return l.configPath
}

type legacyCRIStruct struct {
	ConfigPath string
}

type legacyCRIMethodAndStruct struct {
	ConfigPath string
	methodPath string
}

func (l legacyCRIMethodAndStruct) GetConfigPath() string {
	return l.methodPath
}

func TestConfigPathFromDecodedOptionsFromMethod(t *testing.T) {
	path, ok := configPathFromDecodedOptions(legacyCRIGetter{configPath: "/tmp/micrun.toml"})
	if !ok {
		t.Fatal("expected config path to be detected from method")
	}
	if path != "/tmp/micrun.toml" {
		t.Fatalf("unexpected path %q", path)
	}
}

func TestConfigPathFromDecodedOptionsFromStructField(t *testing.T) {
	path, ok := configPathFromDecodedOptions(&legacyCRIStruct{ConfigPath: "/tmp/micrun.toml"})
	if !ok {
		t.Fatal("expected config path to be detected from struct field")
	}
	if path != "/tmp/micrun.toml" {
		t.Fatalf("unexpected path %q", path)
	}
}

func TestConfigPathFromDecodedOptionsFallsBackToStructFieldWhenMethodBlank(t *testing.T) {
	path, ok := configPathFromDecodedOptions(legacyCRIMethodAndStruct{
		ConfigPath: "/field/micrun.toml",
		methodPath: " \t ",
	})
	if !ok {
		t.Fatal("expected config path to fall back to struct field")
	}
	if path != "/field/micrun.toml" {
		t.Fatalf("unexpected path %q", path)
	}
}

func TestConfigPathFromDecodedOptionsPrefersNonBlankMethodOverStructField(t *testing.T) {
	path, ok := configPathFromDecodedOptions(legacyCRIMethodAndStruct{
		ConfigPath: "/field/micrun.toml",
		methodPath: "/method/micrun.toml",
	})
	if !ok {
		t.Fatal("expected config path to be detected")
	}
	if path != "/method/micrun.toml" {
		t.Fatalf("unexpected path %q", path)
	}
}

func TestConfigPathFromDecodedOptionsIgnoresUnsupportedValue(t *testing.T) {
	if path, ok := configPathFromDecodedOptions(17); ok || path != "" {
		t.Fatalf("expected unsupported value to be ignored, got ok=%v path=%q", ok, path)
	}
}

func TestResolverResolveUsesInjectedHostProfileForConfigFallbacks(t *testing.T) {
	tmp := t.TempDir()
	conf := filepath.Join(tmp, "micrun.ini")
	content := []byte("[container_minmem]\ncontainer_minmem=bad\n[container_maxmem]\ncontainer_maxmem=9999\n")
	if err := os.WriteFile(conf, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resolver := NewResolver(oci.HostProfile{
		MemLowThreshold:  15,
		MemHighThreshold: 77,
	})

	cfg, err := resolver.Resolve(nil, ports.TaskCreateRequest{}, map[string]string{
		ann.SandboxConfigPathKey: conf,
	})
	if err != nil {
		t.Fatalf("Resolve returned unexpected error: %v", err)
	}

	if cfg.MinContainerMemMB != 15 {
		t.Fatalf("MinContainerMemMB = %d, want 15", cfg.MinContainerMemMB)
	}
	if cfg.MaxContainerMemMB != 77 {
		t.Fatalf("MaxContainerMemMB = %d, want 77", cfg.MaxContainerMemMB)
	}
}

func TestResolverResolveLoadsExplicitTomlConfig(t *testing.T) {
	tmp := t.TempDir()
	conf := filepath.Join(tmp, "micrun.toml")
	content := []byte(`
[container_minmem]
container_minmem = 64

[pause_image]
pause_image = "pause:test"
`)
	if err := os.WriteFile(conf, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resolver := NewResolver(oci.HostProfile{
		MemLowThreshold:  16,
		MemHighThreshold: 128,
	})
	cfg, err := resolver.Resolve(nil, ports.TaskCreateRequest{}, map[string]string{
		ann.SandboxConfigPathKey: conf,
	})
	if err != nil {
		t.Fatalf("Resolve returned unexpected error: %v", err)
	}
	if cfg.MinContainerMemMB != 64 {
		t.Fatalf("MinContainerMemMB = %d, want 64", cfg.MinContainerMemMB)
	}
	if cfg.PauseImage != "pause:test" {
		t.Fatalf("PauseImage = %q, want pause:test", cfg.PauseImage)
	}
}
