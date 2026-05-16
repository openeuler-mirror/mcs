package shim

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"
	er "micrun/internal/support/errors"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type typedNilSandboxTraits struct {
	cntr.SandboxTraits
}

type fakeRuntimeSandbox struct {
	id string
}

func (f *fakeRuntimeSandbox) SandboxID() string { return f.id }
func (f *fakeRuntimeSandbox) Start(context.Context) error {
	return nil
}
func (f *fakeRuntimeSandbox) StartContainer(context.Context, string) error {
	return nil
}
func (f *fakeRuntimeSandbox) Stop(context.Context, bool) error {
	return nil
}
func (f *fakeRuntimeSandbox) StopContainer(context.Context, string, bool) error {
	return nil
}
func (f *fakeRuntimeSandbox) Delete(context.Context) error {
	return nil
}
func (f *fakeRuntimeSandbox) PauseContainer(context.Context, string) error {
	return nil
}
func (f *fakeRuntimeSandbox) ResumeContainer(context.Context, string) error {
	return nil
}
func (f *fakeRuntimeSandbox) KillContainer(context.Context, string) error {
	return nil
}
func (f *fakeRuntimeSandbox) IOStream(context.Context, string, string) (io.WriteCloser, io.Reader, io.Reader, error) {
	return nil, nil, nil, nil
}
func (f *fakeRuntimeSandbox) WinResize(context.Context, string, uint32, uint32) error {
	return nil
}
func (f *fakeRuntimeSandbox) OpenTTYs(context.Context, string) (*os.File, *os.File, error) {
	return nil, nil, nil
}
func (f *fakeRuntimeSandbox) UpdateContainer(context.Context, string, specs.LinuxResources) error {
	return nil
}

func TestRuntimeSandboxRejectsMissingSandboxTraits(t *testing.T) {
	var sandbox runtimeSandbox
	assertRuntimeSandboxUnavailable(t, sandbox)
}

func TestRuntimeSandboxRejectsTypedNilSandboxTraits(t *testing.T) {
	var traits *typedNilSandboxTraits
	assertRuntimeSandboxUnavailable(t, runtimeSandbox{SandboxTraits: traits})
}

func assertRuntimeSandboxUnavailable(t *testing.T, sandbox runtimeSandbox) {
	t.Helper()
	ctx := context.Background()

	checks := []struct {
		name string
		run  func() error
	}{
		{name: "start", run: func() error { return sandbox.StartContainer(ctx, "container1") }},
		{name: "stop", run: func() error { return sandbox.StopContainer(ctx, "container1", false) }},
		{name: "kill", run: func() error { return sandbox.KillContainer(ctx, "container1") }},
		{name: "io", run: func() error {
			_, _, _, err := sandbox.IOStream(ctx, "container1", "container1")
			return err
		}},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); !errors.Is(err, er.SandboxNotFound) {
				t.Fatalf("%s error = %v, want SandboxNotFound", check.name, err)
			}
		})
	}
}

func TestShimServiceSaveTaskIgnoresNilTasks(t *testing.T) {
	service := &shimService{containers: make(map[string]*shimContainer)}

	service.SaveTask("plain-nil", nil)
	var taskHandle *shimContainer
	service.SaveTask("typed-nil", taskHandle)

	if len(service.containers) != 0 {
		t.Fatalf("SaveTask stored nil tasks: %#v", service.containers)
	}
}

func TestShimServiceSaveTaskStoresShimContainersOnly(t *testing.T) {
	service := &shimService{containers: make(map[string]*shimContainer)}
	container := &shimContainer{id: "task1"}

	service.SaveTask("task1", container)

	if got, ok := service.containers["task1"]; !ok || got != container {
		t.Fatalf("SaveTask stored %#v, want shim container", got)
	}
}

func TestShimServiceSandboxHandlesNilAndTypedNilInputs(t *testing.T) {
	service := &shimService{}

	service.SetSandbox(nil)
	if service.Sandbox() != nil || service.sandbox != nil {
		t.Fatal("SetSandbox(nil) should clear sandbox")
	}

	var sandbox *fakeRuntimeSandbox
	service.SetSandbox(sandbox)
	if service.Sandbox() != nil || service.sandbox != nil {
		t.Fatal("SetSandbox(typed nil) should clear sandbox")
	}
}

var _ ports.Task = (*shimContainer)(nil)
var _ ports.Sandbox = (*fakeRuntimeSandbox)(nil)
var _ cntr.SandboxTraits = (*typedNilSandboxTraits)(nil)
