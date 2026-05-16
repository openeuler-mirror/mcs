package recovery

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"micrun/internal/ports"

	"github.com/containerd/containerd/api/types/task"
	"github.com/opencontainers/runtime-spec/specs-go"
)

type fakeRecoveryRuntime struct {
	namespace string
	runtimeID string
	sandbox   ports.Sandbox
	tasks     map[string]ports.Task
}

func (f *fakeRecoveryRuntime) Namespace() string                         { return f.namespace }
func (f *fakeRecoveryRuntime) RuntimeID() string                         { return f.runtimeID }
func (f *fakeRecoveryRuntime) SaveTask(id string, taskHandle ports.Task) { f.tasks[id] = taskHandle }
func (f *fakeRecoveryRuntime) SetSandbox(sandbox ports.Sandbox)          { f.sandbox = sandbox }

type fakeRecoveryBackend struct {
	cleanupCalled bool
	sandbox       ports.Sandbox
	tasks         []ports.RecoveredTask
	err           error
}

func (f *fakeRecoveryBackend) CleanupOrphans(ctx context.Context, namespace string) error {
	f.cleanupCalled = true
	return nil
}

func (f *fakeRecoveryBackend) Restore(ctx context.Context, id string) (ports.Sandbox, []ports.RecoveredTask, error) {
	return f.sandbox, f.tasks, f.err
}

type fakeRecoveredTask struct {
	id     string
	status task.Status
}

func (f *fakeRecoveredTask) ID() string                                    { return f.id }
func (f *fakeRecoveredTask) Bundle() string                                { return "" }
func (f *fakeRecoveredTask) PID() uint32                                   { return 0 }
func (f *fakeRecoveredTask) Status() task.Status                           { return f.status }
func (f *fakeRecoveredTask) SetStatus(status task.Status)                  { f.status = status }
func (f *fakeRecoveredTask) Terminal() bool                                { return false }
func (f *fakeRecoveredTask) StdinPath() string                             { return "" }
func (f *fakeRecoveredTask) StdoutPath() string                            { return "" }
func (f *fakeRecoveredTask) StderrPath() string                            { return "" }
func (f *fakeRecoveredTask) ExitStatus() uint32                            { return 0 }
func (f *fakeRecoveredTask) ExitTime() (t time.Time)                       { return }
func (f *fakeRecoveredTask) SetExitInfo(status uint32, exitedAt time.Time) {}
func (f *fakeRecoveredTask) StdinPipe() io.WriteCloser                     { return nil }
func (f *fakeRecoveredTask) StdinCloser() chan struct{}                    { return make(chan struct{}) }
func (f *fakeRecoveredTask) ExitChan() chan struct{}                       { return make(chan struct{}) }
func (f *fakeRecoveredTask) IOExit()                                       {}
func (f *fakeRecoveredTask) CanBeSandbox() bool                            { return false }
func (f *fakeRecoveredTask) IsCriSandbox() bool                            { return false }
func (f *fakeRecoveredTask) Annotations() map[string]string                { return nil }
func (f *fakeRecoveredTask) IOManager() ports.IOManager                    { return nil }
func (f *fakeRecoveredTask) SetIOManager(ports.IOManager)                  {}
func (f *fakeRecoveredTask) AttachInfo() *ports.AttachInfo                 { return nil }
func (f *fakeRecoveredTask) SetAttachInfo(*ports.AttachInfo)               {}
func (f *fakeRecoveredTask) SetStdinPipe(io.WriteCloser)                   {}
func (f *fakeRecoveredTask) SetAttached(attached bool) bool                { return false }

type fakeSandbox struct{}

func (fakeSandbox) SandboxID() string                                 { return "sb" }
func (fakeSandbox) Start(context.Context) error                       { return nil }
func (fakeSandbox) StartContainer(context.Context, string) error      { return nil }
func (fakeSandbox) Stop(context.Context, bool) error                  { return nil }
func (fakeSandbox) StopContainer(context.Context, string, bool) error { return nil }
func (fakeSandbox) Delete(context.Context) error                      { return nil }
func (fakeSandbox) PauseContainer(context.Context, string) error      { return nil }
func (fakeSandbox) ResumeContainer(context.Context, string) error     { return nil }
func (fakeSandbox) KillContainer(context.Context, string) error       { return nil }
func (fakeSandbox) IOStream(context.Context, string, string) (io.WriteCloser, io.Reader, io.Reader, error) {
	return nil, nil, nil, nil
}
func (fakeSandbox) WinResize(context.Context, string, uint32, uint32) error { return nil }
func (fakeSandbox) OpenTTYs(context.Context, string) (*os.File, *os.File, error) {
	return nil, nil, nil
}
func (fakeSandbox) UpdateContainer(context.Context, string, specs.LinuxResources) error { return nil }

func TestServiceRecoverRestoresSandboxAndTasks(t *testing.T) {
	svc := NewService()
	runtime := &fakeRecoveryRuntime{
		namespace: "default",
		runtimeID: "task-1",
		tasks:     make(map[string]ports.Task),
	}
	backend := &fakeRecoveryBackend{
		sandbox: fakeSandbox{},
		tasks: []ports.RecoveredTask{
			{ID: "task-1", IsRunning: true},
			{ID: "task-2", IsRunning: false},
		},
	}

	err := svc.Recover(context.Background(), runtime, backend, func(spec ports.RecoveredTask) ports.Task {
		return &fakeRecoveredTask{id: spec.ID}
	})
	if err != nil {
		t.Fatalf("Recover returned unexpected error: %v", err)
	}
	if !backend.cleanupCalled {
		t.Fatal("expected CleanupOrphans to be called")
	}
	if runtime.sandbox == nil {
		t.Fatal("expected sandbox to be restored")
	}
	if got := runtime.tasks["task-1"].Status(); got != task.Status_RUNNING {
		t.Fatalf("task-1 status = %s, want %s", got, task.Status_RUNNING)
	}
	if got := runtime.tasks["task-2"].Status(); got != task.Status_CREATED {
		t.Fatalf("task-2 status = %s, want %s", got, task.Status_CREATED)
	}
}

func TestServiceRecoverWithNilBackend(t *testing.T) {
	svc := NewService()
	runtime := &fakeRecoveryRuntime{
		namespace: "default",
		runtimeID: "task-1",
		tasks:     make(map[string]ports.Task),
	}

	err := svc.Recover(context.Background(), runtime, nil, func(spec ports.RecoveredTask) ports.Task {
		return &fakeRecoveredTask{id: spec.ID}
	})
	if err != nil {
		t.Fatalf("Recover with nil backend should not error, got: %v", err)
	}
	if runtime.sandbox != nil {
		t.Fatal("expected no sandbox with nil backend")
	}
}

func TestServiceRecoverWithTypedNilBackend(t *testing.T) {
	svc := NewService()
	runtime := &fakeRecoveryRuntime{
		namespace: "default",
		runtimeID: "task-1",
		tasks:     make(map[string]ports.Task),
	}
	var backend *fakeRecoveryBackend

	err := svc.Recover(context.Background(), runtime, backend, func(spec ports.RecoveredTask) ports.Task {
		return &fakeRecoveredTask{id: spec.ID}
	})
	if err != nil {
		t.Fatalf("Recover with typed nil backend should not error, got: %v", err)
	}
	if runtime.sandbox != nil {
		t.Fatal("expected no sandbox with typed nil backend")
	}
}

func TestServiceRecoverHonorsCanceledContextBeforeCleanup(t *testing.T) {
	svc := NewService()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runtime := &fakeRecoveryRuntime{
		namespace: "default",
		runtimeID: "task-1",
		tasks:     make(map[string]ports.Task),
	}
	backend := &fakeRecoveryBackend{}

	err := svc.Recover(ctx, runtime, backend, func(spec ports.RecoveredTask) ports.Task {
		return &fakeRecoveredTask{id: spec.ID}
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Recover error = %v, want context.Canceled", err)
	}
	if backend.cleanupCalled {
		t.Fatal("CleanupOrphans should not run after context cancellation")
	}
}

func TestServiceRecoverRequiresRuntime(t *testing.T) {
	svc := NewService()

	err := svc.Recover(context.Background(), nil, &fakeRecoveryBackend{}, func(spec ports.RecoveredTask) ports.Task {
		return &fakeRecoveredTask{id: spec.ID}
	})
	if err == nil {
		t.Fatal("expected Recover to require runtime")
	}
}

func TestServiceRecoverRequiresTypedNilRuntime(t *testing.T) {
	svc := NewService()
	var runtime *fakeRecoveryRuntime

	err := svc.Recover(context.Background(), runtime, &fakeRecoveryBackend{}, func(spec ports.RecoveredTask) ports.Task {
		return &fakeRecoveredTask{id: spec.ID}
	})
	if err == nil {
		t.Fatal("expected Recover to require runtime")
	}
}

func TestServiceRecoverWithRestoreError(t *testing.T) {
	svc := NewService()
	runtime := &fakeRecoveryRuntime{
		namespace: "default",
		runtimeID: "task-1",
		tasks:     make(map[string]ports.Task),
	}
	backend := &fakeRecoveryBackend{
		err: os.ErrNotExist,
	}

	err := svc.Recover(context.Background(), runtime, backend, func(spec ports.RecoveredTask) ports.Task {
		return &fakeRecoveredTask{id: spec.ID}
	})
	if !os.IsNotExist(err) {
		t.Fatalf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestServiceRecoverRequiresTaskFactory(t *testing.T) {
	svc := NewService()
	runtime := &fakeRecoveryRuntime{
		namespace: "default",
		runtimeID: "task-1",
		tasks:     make(map[string]ports.Task),
	}
	backend := &fakeRecoveryBackend{
		sandbox: fakeSandbox{},
	}

	err := svc.Recover(context.Background(), runtime, backend, nil)
	if err == nil {
		t.Fatal("expected Recover to require task factory")
	}
}

func TestServiceRecoverWithEmptyTasks(t *testing.T) {
	svc := NewService()
	runtime := &fakeRecoveryRuntime{
		namespace: "default",
		runtimeID: "task-1",
		tasks:     make(map[string]ports.Task),
	}
	backend := &fakeRecoveryBackend{
		sandbox: fakeSandbox{},
		tasks:   []ports.RecoveredTask{},
	}

	err := svc.Recover(context.Background(), runtime, backend, func(spec ports.RecoveredTask) ports.Task {
		return &fakeRecoveredTask{id: spec.ID}
	})
	if err != nil {
		t.Fatalf("Recover with empty tasks should not error, got: %v", err)
	}
	if runtime.sandbox == nil {
		t.Fatal("expected sandbox to be restored even with no tasks")
	}
	if len(runtime.tasks) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(runtime.tasks))
	}
}

func TestServiceRecoverStopsSavingTasksAfterContextCancellation(t *testing.T) {
	svc := NewService()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime := &fakeRecoveryRuntime{
		namespace: "default",
		runtimeID: "task-1",
		tasks:     make(map[string]ports.Task),
	}
	backend := &fakeRecoveryBackend{
		sandbox: fakeSandbox{},
		tasks: []ports.RecoveredTask{
			{ID: "task-1", IsRunning: true},
			{ID: "task-2", IsRunning: true},
		},
	}

	err := svc.Recover(ctx, runtime, backend, func(spec ports.RecoveredTask) ports.Task {
		cancel()
		return &fakeRecoveredTask{id: spec.ID}
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Recover error = %v, want context.Canceled", err)
	}
	if _, ok := runtime.tasks["task-1"]; !ok {
		t.Fatal("expected first task to be saved before cancellation")
	}
	if _, ok := runtime.tasks["task-2"]; ok {
		t.Fatal("second task should not be saved after cancellation")
	}
}

func TestServiceRecoverSkipsNilTaskFactory(t *testing.T) {
	svc := NewService()
	runtime := &fakeRecoveryRuntime{
		namespace: "default",
		runtimeID: "task-1",
		tasks:     make(map[string]ports.Task),
	}
	backend := &fakeRecoveryBackend{
		sandbox: fakeSandbox{},
		tasks: []ports.RecoveredTask{
			{ID: "task-1", IsRunning: true},
		},
	}

	err := svc.Recover(context.Background(), runtime, backend, func(spec ports.RecoveredTask) ports.Task {
		return nil
	})
	if err != nil {
		t.Fatalf("Recover should not error with nil task factory, got: %v", err)
	}
	if _, exists := runtime.tasks["task-1"]; exists {
		t.Fatal("expected no task saved when factory returns nil")
	}
}

func TestServiceRecoverSkipsInvalidTaskIDs(t *testing.T) {
	svc := NewService()
	runtime := &fakeRecoveryRuntime{
		namespace: "default",
		runtimeID: "task-1",
		tasks:     make(map[string]ports.Task),
	}
	backend := &fakeRecoveryBackend{
		sandbox: fakeSandbox{},
		tasks: []ports.RecoveredTask{
			{ID: "", IsRunning: true},
			{ID: " padded ", IsRunning: true},
			{ID: "nested/task", IsRunning: true},
			{ID: ".", IsRunning: true},
			{ID: "task-valid", IsRunning: true},
		},
	}

	err := svc.Recover(context.Background(), runtime, backend, func(spec ports.RecoveredTask) ports.Task {
		return &fakeRecoveredTask{id: spec.ID}
	})
	if err != nil {
		t.Fatalf("Recover returned unexpected error: %v", err)
	}
	if len(runtime.tasks) != 1 {
		t.Fatalf("saved tasks = %v, want only valid task", runtime.tasks)
	}
	if _, ok := runtime.tasks["task-valid"]; !ok {
		t.Fatalf("valid task was not saved: %v", runtime.tasks)
	}
}

func TestRestoreRecoveredTasksRequiresValidSpecBeforeFactory(t *testing.T) {
	runtime := &fakeRecoveryRuntime{
		namespace: "default",
		runtimeID: "task-1",
		tasks:     make(map[string]ports.Task),
	}
	factoryCalls := 0

	err := restoreRecoveredTasks(context.Background(), runtime, []ports.RecoveredTask{
		{ID: ""},
		{ID: "task-valid"},
	}, func(spec ports.RecoveredTask) ports.Task {
		factoryCalls++
		return &fakeRecoveredTask{id: spec.ID}
	})
	if err != nil {
		t.Fatalf("restoreRecoveredTasks returned error: %v", err)
	}
	if factoryCalls != 1 {
		t.Fatalf("factory calls = %d, want 1", factoryCalls)
	}
	if _, ok := runtime.tasks["task-valid"]; !ok {
		t.Fatalf("valid task was not saved: %v", runtime.tasks)
	}
}

func TestRecoveredTaskStatus(t *testing.T) {
	tests := []struct {
		name string
		spec ports.RecoveredTask
		want task.Status
	}{
		{
			name: "running task",
			spec: ports.RecoveredTask{IsRunning: true},
			want: task.Status_RUNNING,
		},
		{
			name: "created task",
			spec: ports.RecoveredTask{IsRunning: false},
			want: task.Status_CREATED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recoveredTaskStatus(tt.spec); got != tt.want {
				t.Fatalf("recoveredTaskStatus() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestRecoveredTaskHandleSetsMappedStatus(t *testing.T) {
	taskHandle := &fakeRecoveredTask{id: "task-1"}

	got := recoveredTaskHandle(ports.RecoveredTask{ID: "task-1", IsRunning: true}, func(spec ports.RecoveredTask) ports.Task {
		return taskHandle
	})

	if got != taskHandle {
		t.Fatal("expected recovered task handle to be returned")
	}
	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("task status = %s, want %s", taskHandle.status, task.Status_RUNNING)
	}
}

func TestRecoveredTaskHandleKeepsNilFactoryResult(t *testing.T) {
	t.Run("untyped nil", func(t *testing.T) {
		got := recoveredTaskHandle(ports.RecoveredTask{ID: "task-1"}, func(spec ports.RecoveredTask) ports.Task {
			return nil
		})
		if got != nil {
			t.Fatalf("recoveredTaskHandle() = %v, want nil", got)
		}
	})

	t.Run("typed nil", func(t *testing.T) {
		var typedNilTaskHandle *fakeRecoveredTask
		got := recoveredTaskHandle(ports.RecoveredTask{ID: "task-2"}, func(spec ports.RecoveredTask) ports.Task {
			return typedNilTaskHandle
		})
		if got != nil {
			t.Fatalf("recoveredTaskHandle() = %v, want nil", got)
		}
	})
}
