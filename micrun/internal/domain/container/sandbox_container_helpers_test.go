package container

import (
	"errors"
	"testing"

	er "micrun/internal/support/errors"
)

func TestContainerByIDValidatesInputs(t *testing.T) {
	var nilSandbox *Sandbox
	if _, err := nilSandbox.containerByID("container1"); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("nil sandbox containerByID error = %v, want SandboxNotFound", err)
	}

	sandbox := &Sandbox{containers: map[string]*Container{"nil": nil}}
	if _, err := sandbox.containerByID(""); !errors.Is(err, er.EmptyContainerID) {
		t.Fatalf("empty id containerByID error = %v, want EmptyContainerID", err)
	}
	if _, err := sandbox.containerByID("missing"); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("missing containerByID error = %v, want ContainerNotFound", err)
	}
	if _, err := sandbox.containerByID("nil"); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("nil containerByID error = %v, want ContainerNotFound", err)
	}
}

func TestAddContainerValidatesInputsAndInitializesMap(t *testing.T) {
	var nilSandbox *Sandbox
	if err := nilSandbox.addContainer(&Container{id: "container1"}); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("nil sandbox addContainer error = %v, want SandboxNotFound", err)
	}

	sandbox := &Sandbox{}
	if err := sandbox.addContainer(nil); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("nil container addContainer error = %v, want ContainerNotFound", err)
	}
	if err := sandbox.addContainer(&Container{}); !errors.Is(err, er.EmptyContainerID) {
		t.Fatalf("empty id addContainer error = %v, want EmptyContainerID", err)
	}

	container := &Container{id: "container1"}
	if err := sandbox.addContainer(container); err != nil {
		t.Fatalf("addContainer returned error: %v", err)
	}
	if sandbox.containers["container1"] != container {
		t.Fatal("addContainer did not initialize map and store container")
	}
}

func TestRemoveContainerUsesLookupValidation(t *testing.T) {
	var nilSandbox *Sandbox
	if err := nilSandbox.removeContainer("container1"); !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("nil sandbox removeContainer error = %v, want SandboxNotFound", err)
	}

	sandbox := &Sandbox{
		id: "sandbox1",
		containers: map[string]*Container{
			"nil":        nil,
			"container1": {id: "container1"},
		},
	}
	if err := sandbox.removeContainer(""); !errors.Is(err, er.EmptyContainerID) {
		t.Fatalf("empty id removeContainer error = %v, want EmptyContainerID", err)
	}
	if err := sandbox.removeContainer("nil"); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("nil container removeContainer error = %v, want ContainerNotFound", err)
	}
	if err := sandbox.removeContainer("container1"); err != nil {
		t.Fatalf("removeContainer returned error: %v", err)
	}
	if _, ok := sandbox.containers["container1"]; ok {
		t.Fatal("removeContainer did not delete container")
	}
}

func TestRemoveContainerResourcesAllowsNilSandboxAndEmptyID(t *testing.T) {
	var nilSandbox *Sandbox
	nilSandbox.removeContainerResources("container1")

	sandbox := &Sandbox{
		config: &SandboxConfig{
			ContainerConfigs: map[string]*ContainerConfig{
				"":           {ID: ""},
				"container1": {ID: "container1"},
			},
		},
	}
	sandbox.removeContainerResources("")
	if _, ok := sandbox.config.ContainerConfigs[""]; !ok {
		t.Fatal("empty id cleanup should not delete empty config key")
	}
}
