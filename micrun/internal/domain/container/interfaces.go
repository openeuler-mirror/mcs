package container

import (
	"context"
	"io"
	"os"
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
	StateSnapshot() (ContainerState, error)
	GetClientCPU() string
	SaveState() error
	Signal(ctx context.Context, signal syscall.Signal) error
}

// SandboxTraits defines the full contract for sandbox operations consumed by the transport layer.
type SandboxTraits interface {
	// Identification and state methods
	SandboxID() string
	Annotation(key string) (string, error)
	GetAllContainers() []ContainerTraits
	GetNetNamespace() string
	NetnsHolderPID() int
	GetState() StateString

	// Sandbox Lifecycle methods
	Start(ctx context.Context) error
	Stop(ctx context.Context, force bool) error
	Delete(ctx context.Context) error

	// Container management methods
	CreateContainer(ctx context.Context, config ContainerConfig) (ContainerTraits, error)
	DeleteContainer(ctx context.Context, containerID string) (ContainerTraits, error)
	StartContainer(ctx context.Context, containerID string) (ContainerTraits, error)
	StopContainer(ctx context.Context, containerID string, force bool) (ContainerTraits, error)
	KillContainer(ctx context.Context, containerID string) (ContainerTraits, error)
	StatusContainer(ctx context.Context, containerID string) (ContainerStatus, error)
	StatsContainer(ctx context.Context, containerID string) (ContainerStats, error)
	IOStream(ctx context.Context, containerID, taskID string) (io.WriteCloser, io.Reader, io.Reader, error)
	PauseContainer(ctx context.Context, containerID string) error
	ResumeContainer(ctx context.Context, containerID string) error
	UpdateContainer(ctx context.Context, containerID string, resources specs.LinuxResources) error
	WaitContainerExit(ctx context.Context, containerID string) (int32, error)
	WinResize(ctx context.Context, containerID string, height, width uint32) error
	OpenTTYs(ctx context.Context, containerID string) (stdin, stdout *os.File, err error)
}
