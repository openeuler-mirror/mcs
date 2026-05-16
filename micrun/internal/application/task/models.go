package task

import (
	"time"

	"micrun/internal/ports"

	"github.com/containerd/containerd/api/types/task"
	"github.com/opencontainers/runtime-spec/specs-go"
)

type StateInput struct {
	ID     string
	ExecID string
}

type StateOutput struct {
	ID         string
	Bundle     string
	Pid        uint32
	Status     task.Status
	Stdin      string
	Stdout     string
	Stderr     string
	Terminal   bool
	ExitStatus uint32
	ExitedAt   time.Time
	ExecID     string
}

type CreateInput struct {
	Request ports.TaskCreateRequest
}

type CreateOutput struct {
	Pid uint32
}

type StartInput struct {
	ID     string
	ExecID string
}

type StartOutput struct {
	ContainerID string
	ExecID      string
	Pid         uint32
}

type DeleteInput struct {
	ID     string
	ExecID string
}

type DeleteOutput struct {
	ContainerID string
	ExitStatus  uint32
	ExitedAt    time.Time
	Pid         uint32
	NotFound    bool
}

type SignalInput struct {
	ID string
}

type SignalOutput struct {
	ContainerID string
	EmitEvent   bool
}

type KillInput struct {
	ID     string
	ExecID string
	Signal uint32
}

type ResizePtyInput struct {
	ID     string
	ExecID string
	Height uint32
	Width  uint32
}

type CloseIOInput struct {
	ID         string
	ExecID     string
	CloseStdin bool
}

type WaitInput struct {
	ID     string
	ExecID string
}

type WaitOutput struct {
	ExitStatus uint32
	ExitedAt   time.Time
}

type UpdateInput struct {
	ID        string
	Resources specs.LinuxResources
}
