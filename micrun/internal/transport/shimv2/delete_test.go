package shim

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	cntr "micrun/internal/domain/container"
	er "micrun/internal/support/errors"

	"github.com/containerd/containerd/api/types/task"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func TestDeleteContainerIgnoresWrappedContainerNotFound(t *testing.T) {
	sandbox := &deleteSandboxStub{
		stopErr:   fmt.Errorf("stop container: %w", er.ContainerNotFound),
		deleteErr: fmt.Errorf("delete container: %w", er.ContainerNotFound),
	}
	service := &shimService{
		containers: make(map[string]*shimContainer),
		sandbox:    sandbox,
	}
	container := &shimContainer{
		id:     "container1",
		cType:  cntr.PodContainer,
		status: task.Status_RUNNING,
	}
	service.containers[container.id] = container

	if err := deleteContainer(context.Background(), service, container); err != nil {
		t.Fatalf("deleteContainer() error = %v", err)
	}
	if _, ok := service.containers[container.id]; ok {
		t.Fatalf("container was not removed from shim state")
	}
	if sandbox.stops != 1 {
		t.Fatalf("StopContainer calls = %d, want 1", sandbox.stops)
	}
	if sandbox.deletes != 1 {
		t.Fatalf("DeleteContainer calls = %d, want 1", sandbox.deletes)
	}
}

func TestDeleteContainerSkipsTypedNilSandbox(t *testing.T) {
	var sandbox *deleteSandboxStub
	service := &shimService{
		containers: make(map[string]*shimContainer),
		sandbox:    sandbox,
	}
	container := &shimContainer{
		id:     "container1",
		cType:  cntr.PodContainer,
		status: task.Status_RUNNING,
	}
	service.containers[container.id] = container

	if err := deleteContainer(context.Background(), service, container); err != nil {
		t.Fatalf("deleteContainer() error = %v", err)
	}
	if _, ok := service.containers[container.id]; ok {
		t.Fatal("container was not removed from shim state")
	}
}

func TestDeleteContainerHandlesNilRuntime(t *testing.T) {
	container := &shimContainer{
		id:     "container1",
		cType:  cntr.PodContainer,
		status: task.Status_RUNNING,
	}

	if err := deleteContainer(context.Background(), nil, container); err != nil {
		t.Fatalf("deleteContainer() error = %v", err)
	}
}

type deleteSandboxStub struct {
	stopErr   error
	deleteErr error
	stops     int
	deletes   int
}

func (s *deleteSandboxStub) SandboxID() string { return "sandbox1" }
func (s *deleteSandboxStub) Annotation(string) (string, error) {
	return "", nil
}
func (s *deleteSandboxStub) GetAllContainers() []cntr.ContainerTraits { return nil }
func (s *deleteSandboxStub) GetNetNamespace() string                  { return "" }
func (s *deleteSandboxStub) NetnsHolderPID() int                      { return 0 }
func (s *deleteSandboxStub) GetState() cntr.StateString               { return cntr.StateRunning }
func (s *deleteSandboxStub) Start(context.Context) error              { return nil }
func (s *deleteSandboxStub) Stop(context.Context, bool) error         { return nil }
func (s *deleteSandboxStub) Delete(context.Context) error             { return nil }
func (s *deleteSandboxStub) CreateContainer(context.Context, cntr.ContainerConfig) (cntr.ContainerTraits, error) {
	return nil, nil
}
func (s *deleteSandboxStub) DeleteContainer(context.Context, string) (cntr.ContainerTraits, error) {
	s.deletes++
	return nil, s.deleteErr
}
func (s *deleteSandboxStub) StartContainer(context.Context, string) (cntr.ContainerTraits, error) {
	return nil, nil
}
func (s *deleteSandboxStub) StopContainer(context.Context, string, bool) (cntr.ContainerTraits, error) {
	s.stops++
	return nil, s.stopErr
}
func (s *deleteSandboxStub) KillContainer(context.Context, string) (cntr.ContainerTraits, error) {
	return nil, nil
}
func (s *deleteSandboxStub) StatusContainer(context.Context, string) (cntr.ContainerStatus, error) {
	return cntr.ContainerStatus{}, nil
}
func (s *deleteSandboxStub) StatsContainer(context.Context, string) (cntr.ContainerStats, error) {
	return cntr.ContainerStats{}, nil
}
func (s *deleteSandboxStub) IOStream(context.Context, string, string) (io.WriteCloser, io.Reader, io.Reader, error) {
	return nil, nil, nil, nil
}
func (s *deleteSandboxStub) PauseContainer(context.Context, string) error  { return nil }
func (s *deleteSandboxStub) ResumeContainer(context.Context, string) error { return nil }
func (s *deleteSandboxStub) UpdateContainer(context.Context, string, specs.LinuxResources) error {
	return nil
}
func (s *deleteSandboxStub) WaitContainerExit(context.Context, string) (int32, error) {
	return 0, nil
}
func (s *deleteSandboxStub) WinResize(context.Context, string, uint32, uint32) error {
	return nil
}
func (s *deleteSandboxStub) OpenTTYs(context.Context, string) (*os.File, *os.File, error) {
	return nil, nil, nil
}
