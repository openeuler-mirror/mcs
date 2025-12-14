package micantainer

import (
	"context"
	"io"
	"syscall"

	"github.com/opencontainers/runtime-spec/specs-go"
)

type Network interface {
	NetworkIsCreated() bool
	NetID() string
	NetworkCleanup(id string) error
}

type ContainerTraits interface {
	// RTOS may contains no the concept of PID, so we use a dummy value
	ID() string
	GetAnnotations() map[string]string
	GetPid() int
	Sandbox() SandboxTraits
	GetMemoryLimit() uint64
	Status() StateString
	State() *ContainerState
	GetClientCPU() string
	SaveState() error
	Signal(ctx context.Context, signal syscall.Signal) error
}

// some of which required by containerd
// just a list of interfaces, as reference
type SandboxTraits interface {
	// Identification and state methods
	SandboxID() string
	Annotation(key string) (string, error)
	GetAllContainers() []ContainerTraits
	GetNetNamespace() string
	NetnsHolderPID() int

	// Sandbox Lifecycle methods
	Start(ctx context.Context) error
	Stop(ctx context.Context, force bool) error
	Delete(ctx context.Context) error

	// Container management methods
	CreateContainer(ctx context.Context, config ContainerConfig) (ContainerTraits, error)
	DeleteContainer(ctx context.Context, id string) (ContainerTraits, error)
	StartContainer(ctx context.Context, id string) (ContainerTraits, error)
	StopContainer(ctx context.Context, id string, force bool) (ContainerTraits, error)
	KillContainer(ctx context.Context, id string) (ContainerTraits, error)
	StatusContainer(id string) (ContainerStatus, error)
	StatsContainer(ctx context.Context, id string) (ContainerStats, error)
	IOStream(containerID, taskID string) (io.WriteCloser, io.Reader, io.Reader, error)
	// Not supported well
	// TODO: aftet unified micran and micad, we can achive sending signals to RTOS clients
	PauseContainer(ctx context.Context, id string) error
	ResumeContainer(ctx context.Context, id string) error
	UpdateContainer(ctx context.Context, id string, resources specs.LinuxResources) error
	WaitContainerExit(ctx context.Context, id string) (int32, error)
	WinResize(ctx context.Context, containerID string, height, width uint32) error
}
