package shim

import (
	"path/filepath"
	"strings"
	"testing"

	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"

	"github.com/containerd/containerd/api/types"
)

func TestBuildCreatePlanWrapsSpecLoadErrors(t *testing.T) {
	_, err := buildCreatePlan(nil, ports.TaskCreateRequest{
		ID:     "plan-error",
		Bundle: filepath.Join(t.TempDir(), "missing"),
	})
	if err == nil {
		t.Fatal("buildCreatePlan returned nil error, want spec load failure")
	}
	if !strings.Contains(err.Error(), "load OCI spec") {
		t.Fatalf("buildCreatePlan error = %v, want spec load context", err)
	}
}

func TestExtractRootfsRequiresSingleMount(t *testing.T) {
	empty := extractRootfs(ports.TaskCreateRequest{})
	if empty.Source != "" || empty.Type != "" || len(empty.Options) != 0 {
		t.Fatalf("empty rootfs = %+v, want zero value", empty)
	}

	multiple := extractRootfs(ports.TaskCreateRequest{Rootfs: []*types.Mount{
		{Source: "a"},
		{Source: "b"},
	}})
	if multiple.Source != "" {
		t.Fatalf("multiple rootfs source = %q, want empty", multiple.Source)
	}

	single := extractRootfs(ports.TaskCreateRequest{Rootfs: []*types.Mount{{
		Source:  "root",
		Type:    "bind",
		Options: []string{"rw"},
	}}})
	if single.Source != "root" || single.Type != "bind" || len(single.Options) != 1 || single.Options[0] != "rw" {
		t.Fatalf("single rootfs = %+v, want source/type/options copied", single)
	}
}

func TestCreatePlanAnnotationsAllowsMissingSpec(t *testing.T) {
	if got := (*createPlan)(nil).annotations(); got != nil {
		t.Fatalf("nil plan annotations = %v, want nil", got)
	}
	if got := (&createPlan{}).annotations(); got != nil {
		t.Fatalf("missing spec annotations = %v, want nil", got)
	}
}

func TestCreatePlanLoadSpecRejectsEmptyID(t *testing.T) {
	plan := newCreatePlan(ports.TaskCreateRequest{Bundle: t.TempDir()})
	err := plan.loadSpec()
	if err == nil || !strings.Contains(err.Error(), "load OCI spec") {
		t.Fatalf("loadSpec error = %v, want wrapped spec error", err)
	}
}

func TestCreatePlanResolveContainerTypeRejectsMissingSpec(t *testing.T) {
	plan := &createPlan{request: ports.TaskCreateRequest{ID: "container-type"}}
	err := plan.resolveContainerType()
	if err == nil || !strings.Contains(err.Error(), "resolve container type") {
		t.Fatalf("resolveContainerType error = %v, want context", err)
	}
}

func TestNewContainerUsesRuntimeShimPID(t *testing.T) {
	service := &shimService{namespace: "default", shimPid: 4242}

	container, err := newContainer(service, ports.TaskCreateRequest{
		ID: "container-pid",
	}, cntr.SingleContainer, nil, false)
	if err != nil {
		t.Fatalf("newContainer returned error: %v", err)
	}
	if container.PID() != 4242 {
		t.Fatalf("container PID = %d, want runtime shim PID", container.PID())
	}
}
