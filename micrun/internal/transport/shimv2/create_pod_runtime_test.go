package shim

import (
	"testing"

	cntr "micrun/internal/domain/container"
	ann "micrun/internal/support/annotations"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type annotationSandbox struct {
	cntr.SandboxTraits
	annotations map[string]string
}

func (s *annotationSandbox) GetAnnotations() map[string]string {
	return s.annotations
}

func TestMergeSandboxMicrunAnnotationsPreservesContainerOverrides(t *testing.T) {
	ociSpec := &specs.Spec{Annotations: map[string]string{
		ann.AutoCloseTimeout: "60s",
	}}
	sandbox := &annotationSandbox{annotations: map[string]string{
		ann.AutoCloseTimeout: "0",
		ann.RuntimeDebug:     " true ",
		"other":              "ignored",
	}}

	mergeSandboxMicrunAnnotations(ociSpec, sandbox)

	if got := ociSpec.Annotations[ann.AutoCloseTimeout]; got != "60s" {
		t.Fatalf("container override = %q, want 60s", got)
	}
	if got := ociSpec.Annotations[ann.RuntimeDebug]; got != "true" {
		t.Fatalf("sandbox MicRun annotation = %q, want true", got)
	}
	if _, ok := ociSpec.Annotations["other"]; ok {
		t.Fatal("non-MicRun annotation should not be merged")
	}
}
