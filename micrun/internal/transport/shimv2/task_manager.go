package shim

import (
	"context"
	"errors"
	"fmt"

	apptask "micrun/internal/application/task"
	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"
	"micrun/internal/support/lockutil"
	"micrun/internal/support/validation"

	ptypes "github.com/containerd/containerd/protobuf/types"
	"google.golang.org/protobuf/proto"
)

var errTaskManagerDependenciesRequired = errors.New("task manager dependencies are required")

type taskRuntimeProcess interface {
	lockutil.Locker
	ShimPID() uint32
}

type taskRuntimeMetrics interface {
	lockutil.Locker
	EmptyMetrics() *ptypes.Any
	currentSandbox() (cntr.SandboxTraits, bool)
	hasShimTask(id string) bool
}

type taskRuntimeTaskPresence interface {
	lockutil.Locker
	hasShimTasks() bool
}

type taskRuntimeEvents interface {
	send(ev proto.Message)
}

type taskRuntimeShutdown interface {
	runShutdownEffects()
}

type taskManagerDeps struct {
	create       ports.TaskCreateRuntime
	start        ports.TaskStartRuntime
	delete       ports.TaskDeleteRuntime
	signal       ports.TaskSignalRuntime
	io           ports.TaskIORuntime
	wait         ports.TaskWaitRuntime
	query        ports.TaskQueryRuntime
	events       taskRuntimeEvents
	process      taskRuntimeProcess
	metrics      taskRuntimeMetrics
	taskPresence taskRuntimeTaskPresence
	shutdown     taskRuntimeShutdown
}

func taskManagerDepsFromShimService(s *shimService) taskManagerDeps {
	return taskManagerDeps{
		create:       s,
		start:        s,
		delete:       s,
		signal:       s,
		io:           s,
		wait:         s,
		query:        s,
		events:       s,
		process:      s,
		metrics:      s,
		taskPresence: s,
		shutdown:     s,
	}
}

func (d taskManagerDeps) validate() error {
	if err := validation.RequireAll("missing",
		validation.Required("task create runtime", d.create),
		validation.Required("task start runtime", d.start),
		validation.Required("task delete runtime", d.delete),
		validation.Required("task signal runtime", d.signal),
		validation.Required("task io runtime", d.io),
		validation.Required("task wait runtime", d.wait),
		validation.Required("task query runtime", d.query),
		validation.Required("task events", d.events),
		validation.Required("task process", d.process),
		validation.Required("task metrics", d.metrics),
		validation.Required("task presence", d.taskPresence),
		validation.Required("task shutdown", d.shutdown),
	); err != nil {
		return fmt.Errorf("%w: %w", errTaskManagerDependenciesRequired, err)
	}
	return nil
}

type taskApplication interface {
	Create(ctx context.Context, runtime ports.TaskCreateRuntime, in apptask.CreateInput) (*apptask.CreateOutput, error)
	Start(ctx context.Context, runtime ports.TaskStartRuntime, in apptask.StartInput) (*apptask.StartOutput, error)
	Delete(ctx context.Context, runtime ports.TaskDeleteRuntime, in apptask.DeleteInput) (*apptask.DeleteOutput, error)
	Pause(ctx context.Context, runtime ports.TaskSignalRuntime, in apptask.SignalInput) (apptask.SignalOutput, error)
	Resume(ctx context.Context, runtime ports.TaskSignalRuntime, in apptask.SignalInput) (apptask.SignalOutput, error)
	Kill(ctx context.Context, runtime ports.TaskSignalRuntime, in apptask.KillInput) error
	ResizePty(ctx context.Context, runtime ports.TaskIORuntime, in apptask.ResizePtyInput) error
	CloseIO(ctx context.Context, runtime ports.TaskIORuntime, in apptask.CloseIOInput) error
	Update(ctx context.Context, runtime ports.TaskIORuntime, in apptask.UpdateInput) error
	Wait(ctx context.Context, runtime ports.TaskWaitRuntime, in apptask.WaitInput) (*apptask.WaitOutput, error)
	State(ctx context.Context, runtime ports.TaskQueryRuntime, in apptask.StateInput) (*apptask.StateOutput, error)
}

// taskManager bridges the shim transport layer to the application-layer task
// orchestration service.
type taskManager struct {
	create       ports.TaskCreateRuntime
	start        ports.TaskStartRuntime
	delete       ports.TaskDeleteRuntime
	signal       ports.TaskSignalRuntime
	io           ports.TaskIORuntime
	wait         ports.TaskWaitRuntime
	query        ports.TaskQueryRuntime
	events       taskRuntimeEvents
	process      taskRuntimeProcess
	metrics      taskRuntimeMetrics
	taskPresence taskRuntimeTaskPresence
	shutdown     taskRuntimeShutdown
	service      taskApplication
}

func newTaskManager(deps taskManagerDeps, service taskApplication) (*taskManager, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	if validation.IsNil(service) {
		return nil, errTaskServiceRequired
	}
	return &taskManager{
		create:       deps.create,
		start:        deps.start,
		delete:       deps.delete,
		signal:       deps.signal,
		io:           deps.io,
		wait:         deps.wait,
		query:        deps.query,
		events:       deps.events,
		process:      deps.process,
		metrics:      deps.metrics,
		taskPresence: deps.taskPresence,
		shutdown:     deps.shutdown,
		service:      service,
	}, nil
}

func (s *shimService) getTaskManager() (*taskManager, error) {
	if s == nil || validation.IsNil(s.tm) {
		return nil, fmt.Errorf("task manager is not configured")
	}
	return s.tm, nil
}

func callTaskManager[T any](s *shimService, call func(*taskManager) (T, error)) (T, error) {
	manager, err := s.getTaskManager()
	if err != nil {
		var zero T
		return zero, err
	}
	return call(manager)
}
