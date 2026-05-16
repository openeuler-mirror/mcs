package oci

import (
	cntr "micrun/internal/domain/container"
	ann "micrun/internal/support/annotations"
	defs "micrun/internal/support/definitions"
	"testing"

	ctrAnnotations "github.com/containerd/containerd/pkg/cri/annotations"
	"github.com/opencontainers/runtime-spec/specs-go"
)

func TestCheckInfraUsesRequestedContainerTypeAndCRIAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		ct          cntr.ContainerType
		annotations map[string]string
		want        bool
	}{
		{
			name: "requested pod sandbox without annotations",
			ct:   cntr.PodSandbox,
			want: true,
		},
		{
			name: "cri sandbox annotation",
			ct:   cntr.SingleContainer,
			annotations: map[string]string{
				ctrAnnotations.ContainerType: ctrAnnotations.ContainerTypeSandbox,
			},
			want: true,
		},
		{
			name: "padded cri sandbox annotation",
			ct:   cntr.SingleContainer,
			annotations: map[string]string{
				ctrAnnotations.ContainerType: " " + ctrAnnotations.ContainerTypeSandbox + " ",
			},
			want: true,
		},
		{
			name: "pod container is not infra",
			ct:   cntr.PodContainer,
			annotations: map[string]string{
				ctrAnnotations.ContainerType: ctrAnnotations.ContainerTypeContainer,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkInfra(tt.ct, specs.Spec{Annotations: tt.annotations})
			if got != tt.want {
				t.Fatalf("checkInfra() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnnotationHelpersRejectNilSpec(t *testing.T) {
	if _, err := GetContainerType(nil); err == nil {
		t.Fatal("GetContainerType(nil) returned nil error, want failure")
	}
	if _, err := GetSandboxID(nil); err == nil {
		t.Fatal("GetSandboxID(nil) returned nil error, want failure")
	}
}

func TestGetContainerTypeTrimsAnnotationValue(t *testing.T) {
	got, err := GetContainerType(&specs.Spec{Annotations: map[string]string{
		ctrAnnotations.ContainerType: " " + ctrAnnotations.ContainerTypeContainer + " ",
	}})
	if err != nil {
		t.Fatalf("GetContainerType returned error: %v", err)
	}
	if got != cntr.PodContainer {
		t.Fatalf("GetContainerType = %v, want PodContainer", got)
	}
}

func TestGetContainerTypeIgnoresBlankAnnotationValue(t *testing.T) {
	got, err := GetContainerType(&specs.Spec{Annotations: map[string]string{
		ctrAnnotations.ContainerType: " \t ",
	}})
	if err != nil {
		t.Fatalf("GetContainerType returned error: %v", err)
	}
	if got != cntr.SingleContainer {
		t.Fatalf("GetContainerType = %v, want SingleContainer", got)
	}
}

func TestGetSandboxIDTrimsAndSkipsBlankValues(t *testing.T) {
	got, err := GetSandboxID(&specs.Spec{Annotations: map[string]string{
		ctrAnnotations.SandboxID: "  sandbox-a  ",
	}})
	if err != nil {
		t.Fatalf("GetSandboxID returned error: %v", err)
	}
	if got != "sandbox-a" {
		t.Fatalf("GetSandboxID = %q, want sandbox-a", got)
	}

	_, err = GetSandboxID(&specs.Spec{Annotations: map[string]string{
		ctrAnnotations.SandboxID: " \t ",
	}})
	if err == nil {
		t.Fatal("GetSandboxID returned nil error for blank sandbox ID")
	}
}

func TestGetOSInfoAllowsNilGetter(t *testing.T) {
	if got := getOSInfo(nil); got != defs.DefaultOS {
		t.Fatalf("getOSInfo(nil) = %q, want %s", got, defs.DefaultOS)
	}
}

func TestApplySandboxBoolAnnotationAllowsNilConfig(t *testing.T) {
	applySandboxBoolAnnotation(nil, ann.RuntimeHugePageEnable, "true", func(cfg *cntr.SandboxConfig, value bool) {
		t.Fatal("applier should not be called for nil config")
	})
}

func TestApplySandboxAnnotationsUsesKnownRuntimeKeys(t *testing.T) {
	cfg := &cntr.SandboxConfig{}
	applySandboxAnnotations(specs.Spec{Annotations: map[string]string{
		ann.RuntimeEnableVCPUsPinning: " true ",
		ann.RuntimeStaticResource:     "false",
		ann.RuntimeHugePageEnable:     "true",
		ann.RuntimeDebug:              "true",
	}}, cfg)

	if !cfg.EnableVCPUsPinning {
		t.Fatal("EnableVCPUsPinning = false, want true")
	}
	if cfg.StaticResourceMgmt {
		t.Fatal("StaticResourceMgmt = true, want false")
	}
	if !cfg.HugePageSupport {
		t.Fatal("HugePageSupport = false, want true")
	}
	if got := cfg.Annotations[ann.RuntimeEnableVCPUsPinning]; got != "true" {
		t.Fatalf("trimmed annotation = %q, want true", got)
	}
	if got := cfg.Annotations[ann.RuntimeDebug]; got != "true" {
		t.Fatalf("RuntimeDebug annotation = %q, want true", got)
	}
}

func TestApplySandboxAnnotationsCopiesMicrunContainerKeys(t *testing.T) {
	cfg := &cntr.SandboxConfig{}
	applySandboxAnnotations(specs.Spec{Annotations: map[string]string{
		ann.AutoCloseTimeout: " 0 ",
		"other":              "ignored",
	}}, cfg)

	if got := cfg.Annotations[ann.AutoCloseTimeout]; got != "0" {
		t.Fatalf("container annotation = %q, want 0", got)
	}
	if _, ok := cfg.Annotations["other"]; ok {
		t.Fatal("non-MicRun annotation should not be copied")
	}
}

func TestApplySandboxAnnotationsSupportsVCPUBindingAlias(t *testing.T) {
	cfg := &cntr.SandboxConfig{}
	applySandboxAnnotations(specs.Spec{Annotations: map[string]string{
		ann.VCPUBinding: "true",
	}}, cfg)

	if !cfg.EnableVCPUsPinning {
		t.Fatal("EnableVCPUsPinning = false, want true")
	}
	if got := cfg.Annotations[ann.VCPUBinding]; got != "true" {
		t.Fatalf("copied annotation = %q, want true", got)
	}
}

func TestApplySandboxAnnotationsCopiesInvalidKnownToggleWithoutApplying(t *testing.T) {
	cfg := &cntr.SandboxConfig{HugePageSupport: true}
	applySandboxAnnotations(specs.Spec{Annotations: map[string]string{
		ann.RuntimeHugePageEnable: "invalid",
	}}, cfg)

	if !cfg.HugePageSupport {
		t.Fatal("HugePageSupport changed on invalid bool")
	}
	if got := cfg.Annotations[ann.RuntimeHugePageEnable]; got != "invalid" {
		t.Fatalf("copied annotation = %q, want invalid", got)
	}
}
