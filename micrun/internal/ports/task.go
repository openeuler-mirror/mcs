package ports

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/typeurl/v2"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// IOManager abstracts an attachable stdio session owned by a task.
type IOManager interface {
	Start() error
	Stop()
	StopWithoutClosingFIFOs()
	Restart() error
	RestartWithTTYs(ttyIn io.WriteCloser, ttyOut io.Reader) error
	IsRunning() bool
	EventStream() IOEventStream
}

// AttachInfo stores the state required to restart or reattach a task IO session.
type AttachInfo struct {
	Stdin    string
	Stdout   string
	Stderr   string
	Terminal bool
	TTYIn    io.WriteCloser
	TTYOut   io.Reader
	TTYErr   io.Reader
}

// Sandbox abstracts the sandbox lifecycle and task-facing capabilities needed by
// the application layer.
type Sandbox interface {
	SandboxID() string
	Start(ctx context.Context) error
	StartContainer(ctx context.Context, containerID string) error
	Stop(ctx context.Context, force bool) error
	StopContainer(ctx context.Context, containerID string, force bool) error
	Delete(ctx context.Context) error
	PauseContainer(ctx context.Context, containerID string) error
	ResumeContainer(ctx context.Context, containerID string) error
	KillContainer(ctx context.Context, containerID string) error
	IOStream(ctx context.Context, containerID, taskID string) (stdin io.WriteCloser, stdout, stderr io.Reader, err error)
	WinResize(ctx context.Context, containerID string, height, width uint32) error
	OpenTTYs(ctx context.Context, containerID string) (stdin, stdout *os.File, err error)
	UpdateContainer(ctx context.Context, containerID string, resources specs.LinuxResources) error
}

// Task exposes the task-level state and lifecycle hooks needed by the
// application-layer task service.
type Task interface {
	ID() string
	Bundle() string
	PID() uint32
	Status() task.Status
	SetStatus(task.Status)
	Terminal() bool
	StdinPath() string
	StdoutPath() string
	StderrPath() string
	ExitStatus() uint32
	ExitTime() time.Time
	SetExitInfo(status uint32, exitedAt time.Time)
	StdinPipe() io.WriteCloser
	StdinCloser() chan struct{}
	ExitChan() chan struct{}
	IOExit()
	CanBeSandbox() bool
	IsCriSandbox() bool
	Annotations() map[string]string
	IOManager() IOManager
	SetIOManager(IOManager)
	AttachInfo() *AttachInfo
	SetAttachInfo(*AttachInfo)
	SetStdinPipe(io.WriteCloser)
	SetAttached(attached bool) (previous bool)
}

// TaskCreateRequest is the transport-independent task creation input consumed by
// the runtime-facing port.
type TaskCreateRequest struct {
	ID       string
	Bundle   string
	Rootfs   []*types.Mount
	Stdin    string
	Stdout   string
	Stderr   string
	Terminal bool
	Options  typeurl.Any
}

// TaskLocker provides mutex access for task operations.
type TaskLocker interface {
	Lock()
	Unlock()
}

// TaskIdentity provides runtime identity information.
type TaskIdentity interface {
	Namespace() string
	BackgroundContext() context.Context
	ShimPID() uint32
}

// TaskStore provides task lookup and persistence.
type TaskStore interface {
	LookupTask(id string) (Task, bool)
	SaveTask(id string, task Task)
	DeleteTask(id string)
}

// TaskFactory provides task creation and cleanup.
type TaskFactory interface {
	CreateTask(ctx context.Context, r TaskCreateRequest) (Task, error)
	CleanupTask(ctx context.Context, task Task) error
}

// TaskSandboxAccess provides sandbox access for task operations.
type TaskSandboxAccess interface {
	Sandbox() Sandbox
	SetSandbox(Sandbox)
}

// TaskStatusOps provides task status query and mutation.
type TaskStatusOps interface {
	QueryTaskStatus(ctx context.Context, id string) (task.Status, error)
	MarkKilledByAPI()
	ReportTaskExit(task Task, status int, exitedAt time.Time)
}

// TaskAttachRuntime captures only the runtime-facing capabilities required by
// attach-session orchestration.
type TaskAttachRuntime interface {
	TaskLocker
	TaskIdentity
	TaskSandboxAccess
}

// TaskLifecycleRuntime captures only the runtime-facing capabilities required
// by lifecycle start/exit orchestration.
type TaskLifecycleRuntime interface {
	TaskAttachRuntime
	TaskStatusOps
}

// TaskCreateRuntime captures capabilities needed to create and register a task.
type TaskCreateRuntime interface {
	TaskLocker
	TaskIdentity
	TaskStore
	TaskFactory
}

// TaskStartRuntime captures capabilities needed by task start and reattach.
type TaskStartRuntime interface {
	TaskLocker
	TaskIdentity
	TaskStore
	TaskSandboxAccess
	TaskStatusOps
}

// TaskDeleteRuntime captures capabilities needed to delete a task.
type TaskDeleteRuntime interface {
	TaskLocker
	TaskIdentity
	TaskStore
	TaskFactory
	TaskSandboxAccess
}

// TaskQueryRuntime captures capabilities needed by task state queries.
type TaskQueryRuntime interface {
	TaskLocker
	TaskIdentity
	TaskStore
	TaskSandboxAccess
	TaskStatusOps
}

// TaskWaitRuntime captures capabilities needed by task wait.
type TaskWaitRuntime interface {
	TaskLocker
	TaskStore
}

// TaskSignalRuntime captures capabilities needed by pause/resume/kill.
type TaskSignalRuntime interface {
	TaskLocker
	TaskStore
	TaskSandboxAccess
	TaskStatusOps
}

// TaskIORuntime captures capabilities needed by resize/close/update IO paths.
type TaskIORuntime interface {
	TaskLocker
	TaskIdentity
	TaskStore
	TaskSandboxAccess
}

// TaskRuntime composes all runtime-facing capabilities needed by the
// application-layer task service. The shim transport layer implements this.
// Prefer accepting the smallest sub-interface that satisfies your needs.
type TaskRuntime interface {
	TaskLocker
	TaskIdentity
	TaskStore
	TaskFactory
	TaskSandboxAccess
	TaskStatusOps
}
