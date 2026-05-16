package task

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	attachapp "micrun/internal/application/attach"
	"micrun/internal/application/exitstatus"
	lifecycleapp "micrun/internal/application/lifecycle"
	"micrun/internal/ports"
	er "micrun/internal/support/errors"

	"github.com/containerd/containerd/api/types/task"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type fakeRuntime struct {
	mu            sync.Mutex
	namespace     string
	shimPID       uint32
	tasks         map[string]ports.Task
	created       ports.Task
	createErr     error
	sandbox       ports.Sandbox
	queryErr      error
	queryStat     task.Status
	killed        bool
	createStarted chan struct{}
	releaseCreate chan struct{}
	queryStarted  chan struct{}
	releaseQuery  chan struct{}
}

func (f *fakeRuntime) Lock()                              { f.mu.Lock() }
func (f *fakeRuntime) Unlock()                            { f.mu.Unlock() }
func (f *fakeRuntime) Namespace() string                  { return f.namespace }
func (f *fakeRuntime) BackgroundContext() context.Context { return context.Background() }
func (f *fakeRuntime) ShimPID() uint32                    { return f.shimPID }
func (f *fakeRuntime) LookupTask(id string) (ports.Task, bool) {
	t, ok := f.tasks[id]
	return t, ok
}
func (f *fakeRuntime) SaveTask(id string, taskHandle ports.Task) { f.tasks[id] = taskHandle }
func (f *fakeRuntime) DeleteTask(id string)                      { delete(f.tasks, id) }
func (f *fakeRuntime) CreateTask(ctx context.Context, r ports.TaskCreateRequest) (ports.Task, error) {
	if f.createStarted != nil {
		close(f.createStarted)
	}
	if f.releaseCreate != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.releaseCreate:
		}
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.created, nil
}
func (f *fakeRuntime) CleanupTask(ctx context.Context, taskHandle ports.Task) error { return nil }
func (f *fakeRuntime) Sandbox() ports.Sandbox                                       { return f.sandbox }
func (f *fakeRuntime) SetSandbox(sandbox ports.Sandbox)                             { f.sandbox = sandbox }
func (f *fakeRuntime) QueryTaskStatus(ctx context.Context, id string) (task.Status, error) {
	if f.queryStarted != nil {
		close(f.queryStarted)
	}
	if f.releaseQuery != nil {
		select {
		case <-ctx.Done():
			return task.Status_UNKNOWN, ctx.Err()
		case <-f.releaseQuery:
		}
	}
	if f.queryErr != nil {
		return task.Status_UNKNOWN, f.queryErr
	}
	if f.queryStat != 0 {
		return f.queryStat, nil
	}
	return task.Status_UNKNOWN, nil
}
func (f *fakeRuntime) MarkKilledByAPI()                                               { f.killed = true }
func (f *fakeRuntime) ReportTaskExit(task ports.Task, status int, exitedAt time.Time) {}

type fakeTask struct {
	id          string
	bundle      string
	pid         uint32
	status      task.Status
	terminal    bool
	stdin       string
	stdout      string
	stderr      string
	exitStatus  uint32
	exitTime    time.Time
	stdinPipe   io.WriteCloser
	stdinCloser chan struct{}
	exitCh      chan struct{}
	canSandbox  bool
	ioExited    bool
	ioManager   ports.IOManager
	attachInfo  *ports.AttachInfo
}

func (f *fakeTask) ID() string                             { return f.id }
func (f *fakeTask) Bundle() string                         { return f.bundle }
func (f *fakeTask) PID() uint32                            { return f.pid }
func (f *fakeTask) Status() task.Status                    { return f.status }
func (f *fakeTask) SetStatus(s task.Status)                { f.status = s }
func (f *fakeTask) Terminal() bool                         { return f.terminal }
func (f *fakeTask) StdinPath() string                      { return f.stdin }
func (f *fakeTask) StdoutPath() string                     { return f.stdout }
func (f *fakeTask) StderrPath() string                     { return f.stderr }
func (f *fakeTask) ExitStatus() uint32                     { return f.exitStatus }
func (f *fakeTask) ExitTime() time.Time                    { return f.exitTime }
func (f *fakeTask) SetExitInfo(status uint32, t time.Time) { f.exitStatus, f.exitTime = status, t }
func (f *fakeTask) StdinPipe() io.WriteCloser              { return f.stdinPipe }
func (f *fakeTask) StdinCloser() chan struct{}             { return f.stdinCloser }
func (f *fakeTask) ExitChan() chan struct{}                { return f.exitCh }
func (f *fakeTask) IOExit()                                { f.ioExited = true }
func (f *fakeTask) CanBeSandbox() bool                     { return f.canSandbox }
func (f *fakeTask) IsCriSandbox() bool                     { return false }
func (f *fakeTask) Annotations() map[string]string         { return nil }
func (f *fakeTask) IOManager() ports.IOManager             { return f.ioManager }
func (f *fakeTask) SetIOManager(manager ports.IOManager)   { f.ioManager = manager }
func (f *fakeTask) AttachInfo() *ports.AttachInfo          { return f.attachInfo }
func (f *fakeTask) SetAttachInfo(info *ports.AttachInfo)   { f.attachInfo = info }
func (f *fakeTask) SetStdinPipe(pipe io.WriteCloser)       { f.stdinPipe = pipe }
func (f *fakeTask) SetAttached(attached bool) bool         { return false }

type trackingWriteCloser struct {
	closed chan struct{}
}

type fakeSandbox struct {
	pauseErr      error
	resumeErr     error
	killErr       error
	stopErr       error
	deleteErr     error
	updateErr     error
	ttyIn         *os.File
	ttyOut        *os.File
	stopStarted   chan struct{}
	releaseStop   chan struct{}
	pauseStarted  chan struct{}
	releasePause  chan struct{}
	resumeStarted chan struct{}
	releaseResume chan struct{}
	killStarted   chan struct{}
	releaseKill   chan struct{}
	updateStarted chan struct{}
	releaseUpdate chan struct{}
}

func (f *fakeSandbox) SandboxID() string                                   { return "sandbox-test" }
func (f *fakeSandbox) Start(ctx context.Context) error                     { return nil }
func (f *fakeSandbox) StartContainer(ctx context.Context, id string) error { return nil }
func (f *fakeSandbox) Stop(ctx context.Context, force bool) error {
	if f.stopStarted != nil {
		close(f.stopStarted)
	}
	if f.releaseStop != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.releaseStop:
		}
	}
	return f.stopErr
}
func (f *fakeSandbox) StopContainer(ctx context.Context, id string, force bool) error { return nil }
func (f *fakeSandbox) Delete(ctx context.Context) error                               { return f.deleteErr }
func (f *fakeSandbox) PauseContainer(ctx context.Context, id string) error {
	if f.pauseStarted != nil {
		close(f.pauseStarted)
	}
	if f.releasePause != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.releasePause:
		}
	}
	return f.pauseErr
}
func (f *fakeSandbox) ResumeContainer(ctx context.Context, id string) error {
	if f.resumeStarted != nil {
		close(f.resumeStarted)
	}
	if f.releaseResume != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.releaseResume:
		}
	}
	return f.resumeErr
}
func (f *fakeSandbox) KillContainer(ctx context.Context, id string) error {
	if f.killStarted != nil {
		close(f.killStarted)
	}
	if f.releaseKill != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.releaseKill:
		}
	}
	return f.killErr
}
func (f *fakeSandbox) IOStream(ctx context.Context, containerID, taskID string) (io.WriteCloser, io.Reader, io.Reader, error) {
	return nil, nil, nil, nil
}
func (f *fakeSandbox) WinResize(ctx context.Context, containerID string, height, width uint32) error {
	return nil
}
func (f *fakeSandbox) OpenTTYs(ctx context.Context, containerID string) (*os.File, *os.File, error) {
	return f.ttyIn, f.ttyOut, nil
}
func (f *fakeSandbox) UpdateContainer(ctx context.Context, id string, resources specs.LinuxResources) error {
	if f.updateStarted != nil {
		close(f.updateStarted)
	}
	if f.releaseUpdate != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.releaseUpdate:
		}
	}
	return f.updateErr
}

type fakeTaskIOManager struct {
	startCalled bool
}

func (f *fakeTaskIOManager) Start() error                                    { f.startCalled = true; return nil }
func (f *fakeTaskIOManager) Stop()                                           {}
func (f *fakeTaskIOManager) StopWithoutClosingFIFOs()                        {}
func (f *fakeTaskIOManager) Restart() error                                  { return nil }
func (f *fakeTaskIOManager) RestartWithTTYs(io.WriteCloser, io.Reader) error { return nil }
func (f *fakeTaskIOManager) IsRunning() bool                                 { return f.startCalled }
func (f *fakeTaskIOManager) EventStream() ports.IOEventStream                { return &fakeTaskEventStream{} }

type fakeTaskEventStream struct{}

func (f *fakeTaskEventStream) SubscribeMany(eventTypes ...ports.IOEventType) ports.IOEventSubscriber {
	return make(chan ports.IOEvent)
}

type fakeTaskIOFactory struct {
	manager ports.IOManager
	stream  ports.IOEventStream
}

func (f *fakeTaskIOFactory) NewSession(ctx context.Context, config ports.IOSessionConfig) (ports.IOManager, ports.IOEventStream, error) {
	return f.manager, f.stream, nil
}
func (f *fakeTaskIOFactory) IsValidFIFOPath(path string) bool { return path != "" }
func (f *fakeTaskIOFactory) GenerateFIFOPath(namespace, containerID, stream string) string {
	return "/generated/" + namespace + "/" + containerID + "/" + stream
}

func (t *trackingWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (t *trackingWriteCloser) Close() error {
	select {
	case <-t.closed:
	default:
		close(t.closed)
	}
	return nil
}

func TestServiceCreateStoresTaskAndPublishesEvent(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:       "task-1",
		bundle:   "/bundle",
		pid:      42,
		exitCh:   make(chan struct{}),
		stdin:    "/stdin",
		stdout:   "/stdout",
		stderr:   "/stderr",
		terminal: true,
	}
	runtime := &fakeRuntime{
		namespace: "default",
		shimPID:   99,
		tasks:     make(map[string]ports.Task),
		created:   taskHandle,
	}

	resp, err := svc.Create(context.Background(), runtime, CreateInput{
		Request: ports.TaskCreateRequest{
			ID:       taskHandle.id,
			Bundle:   taskHandle.bundle,
			Terminal: taskHandle.terminal,
			Stdin:    taskHandle.stdin,
			Stdout:   taskHandle.stdout,
			Stderr:   taskHandle.stderr,
		},
	})
	if err != nil {
		t.Fatalf("Create returned unexpected error: %v", err)
	}
	if resp.Pid != taskHandle.pid {
		t.Fatalf("unexpected pid: got %d want %d", resp.Pid, taskHandle.pid)
	}
	stored, ok := runtime.tasks[taskHandle.id]
	if !ok || stored == nil {
		t.Fatalf("task was not stored in runtime")
	}
	if stored.Status() != task.Status_CREATED {
		t.Fatalf("stored task status = %s, want %s", stored.Status(), task.Status_CREATED)
	}
}

func TestServiceCreateReleasesRuntimeLockDuringCreateTask(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-create-lock", pid: 42}
	createStarted := make(chan struct{})
	releaseCreate := make(chan struct{})
	runtime := &fakeRuntime{
		tasks:         make(map[string]ports.Task),
		created:       taskHandle,
		createStarted: createStarted,
		releaseCreate: releaseCreate,
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.Create(context.Background(), runtime, CreateInput{
			Request: ports.TaskCreateRequest{ID: taskHandle.id},
		})
		done <- err
	}()

	select {
	case <-createStarted:
	case <-time.After(time.Second):
		t.Fatal("Create did not reach runtime CreateTask")
	}

	lockReleased := make(chan struct{})
	go func() {
		runtime.Lock()
		runtime.Unlock() //nolint:staticcheck // SA2001: intentional lock availability check
		close(lockReleased)
	}()

	select {
	case <-lockReleased:
	case <-time.After(time.Second):
		t.Fatal("runtime lock remained held during CreateTask")
	}

	close(releaseCreate)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Create returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Create did not return after CreateTask completed")
	}
}

func TestServiceCreateInvalidIDPreservesValidationReason(t *testing.T) {
	svc := NewService(nil)
	runtime := &fakeRuntime{tasks: make(map[string]ports.Task)}

	_, err := svc.Create(context.Background(), runtime, CreateInput{
		Request: ports.TaskCreateRequest{ID: "-invalid"},
	})

	if !errors.Is(err, er.InvalidCID) {
		t.Fatalf("Create invalid ID error = %v, want InvalidCID", err)
	}
	if !strings.Contains(err.Error(), "invalid format") {
		t.Fatalf("Create invalid ID error = %v, want validation reason", err)
	}
}

func TestServiceCreateRejectsTypedNilRuntime(t *testing.T) {
	svc := NewService(nil)
	var runtime *fakeRuntime

	_, err := svc.Create(context.Background(), runtime, CreateInput{
		Request: ports.TaskCreateRequest{ID: "task-typed-nil"},
	})
	if err == nil || !strings.Contains(err.Error(), "task runtime is required") {
		t.Fatalf("Create error = %v, want required runtime error", err)
	}
}

func TestServiceCreateValidatesCreatedTaskHandle(t *testing.T) {
	svc := NewService(nil)

	t.Run("nil handle", func(t *testing.T) {
		runtime := &fakeRuntime{
			tasks: make(map[string]ports.Task),
		}
		_, err := svc.Create(context.Background(), runtime, CreateInput{
			Request: ports.TaskCreateRequest{ID: "task-nil"},
		})
		if err == nil || !strings.Contains(err.Error(), "task handle") {
			t.Fatalf("Create error = %v, want task handle error", err)
		}
	})

	t.Run("typed nil handle", func(t *testing.T) {
		var typedNilTaskHandle *fakeTask
		runtime := &fakeRuntime{
			tasks:   make(map[string]ports.Task),
			created: typedNilTaskHandle,
		}
		_, err := svc.Create(context.Background(), runtime, CreateInput{
			Request: ports.TaskCreateRequest{ID: "task-typed-nil"},
		})
		if err == nil || !strings.Contains(err.Error(), "task handle") {
			t.Fatalf("Create error = %v, want task handle error", err)
		}
	})

	t.Run("id mismatch", func(t *testing.T) {
		runtime := &fakeRuntime{
			tasks:   make(map[string]ports.Task),
			created: &fakeTask{id: "other-task"},
		}
		_, err := svc.Create(context.Background(), runtime, CreateInput{
			Request: ports.TaskCreateRequest{ID: "task-request"},
		})
		if err == nil || !strings.Contains(err.Error(), "id mismatch") {
			t.Fatalf("Create error = %v, want id mismatch error", err)
		}
		if len(runtime.tasks) != 0 {
			t.Fatal("mismatched task should not be saved")
		}
	})
}

func TestServiceCreateHonorsCanceledContextBeforeFactory(t *testing.T) {
	svc := NewService(nil)
	createStarted := make(chan struct{})
	runtime := &fakeRuntime{
		tasks:         map[string]ports.Task{},
		created:       &fakeTask{id: "task-create-canceled"},
		createStarted: createStarted,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Create(ctx, runtime, CreateInput{
		Request: ports.TaskCreateRequest{
			ID: "task-create-canceled",
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Create canceled error = %v, want context.Canceled", err)
	}
	select {
	case <-createStarted:
		t.Fatal("Create called runtime factory after context cancellation")
	default:
	}
}

func TestServiceStateRefreshesRuntimeStatus(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-1", pid: 1234, status: task.Status_RUNNING}
	runtime := &fakeRuntime{
		shimPID:   99,
		tasks:     map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox:   &fakeSandbox{},
		queryStat: task.Status_PAUSED,
	}

	out, err := svc.State(context.Background(), runtime, StateInput{ID: taskHandle.id})
	if err != nil {
		t.Fatalf("State returned unexpected error: %v", err)
	}
	if out.Status != task.Status_PAUSED {
		t.Fatalf("State status = %s, want %s", out.Status, task.Status_PAUSED)
	}
	if taskHandle.status != task.Status_PAUSED {
		t.Fatalf("task status = %s, want %s", taskHandle.status, task.Status_PAUSED)
	}
	if out.Pid != taskHandle.pid {
		t.Fatalf("State pid = %d, want task pid %d", out.Pid, taskHandle.pid)
	}
}

func TestServiceStateHonorsCanceledContextBeforeRefresh(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-state-canceled", status: task.Status_RUNNING}
	queryStarted := make(chan struct{})
	runtime := &fakeRuntime{
		tasks:        map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox:      &fakeSandbox{},
		queryStarted: queryStarted,
		queryStat:    task.Status_PAUSED,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.State(ctx, runtime, StateInput{ID: taskHandle.id})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("State canceled error = %v, want context.Canceled", err)
	}
	select {
	case <-queryStarted:
		t.Fatal("State queried runtime status after context cancellation")
	default:
	}
}

func TestServiceStateCancelsDuringStatusRefresh(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-state-refresh-canceled", status: task.Status_RUNNING}
	queryStarted := make(chan struct{})
	runtime := &fakeRuntime{
		tasks:        map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox:      &fakeSandbox{},
		queryStarted: queryStarted,
		releaseQuery: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)

	go func() {
		_, err := svc.State(ctx, runtime, StateInput{ID: taskHandle.id})
		result <- err
	}()

	<-queryStarted
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("State canceled error = %v, want context.Canceled", err)
	}
}

func TestServiceStateReleasesRuntimeLockDuringStatusQuery(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-state-lock", status: task.Status_RUNNING}
	queryStarted := make(chan struct{})
	releaseQuery := make(chan struct{})
	runtime := &fakeRuntime{
		tasks:        map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox:      &fakeSandbox{},
		queryStat:    task.Status_PAUSED,
		queryStarted: queryStarted,
		releaseQuery: releaseQuery,
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.State(context.Background(), runtime, StateInput{ID: taskHandle.id})
		done <- err
	}()

	select {
	case <-queryStarted:
	case <-time.After(time.Second):
		t.Fatal("State did not reach runtime status query")
	}

	lockReleased := make(chan struct{})
	go func() {
		runtime.Lock()
		runtime.Unlock() //nolint:staticcheck // SA2001: intentional lock availability check
		close(lockReleased)
	}()

	select {
	case <-lockReleased:
	case <-time.After(time.Second):
		t.Fatal("runtime lock remained held during status query")
	}

	close(releaseQuery)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("State returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("State did not return after status query completed")
	}
}

func TestServiceStateKeepsStoppedStatus(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-1", status: task.Status_STOPPED}
	runtime := &fakeRuntime{
		tasks:     map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox:   &fakeSandbox{},
		queryStat: task.Status_RUNNING,
	}

	out, err := svc.State(context.Background(), runtime, StateInput{ID: taskHandle.id})
	if err != nil {
		t.Fatalf("State returned unexpected error: %v", err)
	}
	if out.Status != task.Status_STOPPED {
		t.Fatalf("State status = %s, want %s", out.Status, task.Status_STOPPED)
	}
}

func TestServiceStateKeepsCachedStatusWithoutSandbox(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-1", status: task.Status_CREATED}
	runtime := &fakeRuntime{
		shimPID:   99,
		tasks:     map[string]ports.Task{taskHandle.id: taskHandle},
		queryStat: task.Status_RUNNING,
	}

	out, err := svc.State(context.Background(), runtime, StateInput{ID: taskHandle.id})
	if err != nil {
		t.Fatalf("State returned unexpected error: %v", err)
	}
	if out.Status != task.Status_CREATED {
		t.Fatalf("State status = %s, want %s", out.Status, task.Status_CREATED)
	}
	if out.Pid != runtime.shimPID {
		t.Fatalf("State pid = %d, want shim pid %d", out.Pid, runtime.shimPID)
	}
}

func TestServiceStateRejectsExecID(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-1", status: task.Status_RUNNING}
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
	}

	_, err := svc.State(context.Background(), runtime, StateInput{ID: taskHandle.id, ExecID: "exec-1"})
	if !errors.Is(err, er.FlexibleTaskUnsupported) {
		t.Fatalf("State error = %v, want %v", err, er.FlexibleTaskUnsupported)
	}
}

func TestServiceStartRejectsExecID(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-1", status: task.Status_CREATED}
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
	}

	_, err := svc.Start(context.Background(), runtime, StartInput{ID: taskHandle.id, ExecID: "exec-1"})
	if !errors.Is(err, er.FlexibleTaskUnsupported) {
		t.Fatalf("Start error = %v, want %v", err, er.FlexibleTaskUnsupported)
	}
	if taskHandle.status != task.Status_CREATED {
		t.Fatalf("task status = %s, want %s", taskHandle.status, task.Status_CREATED)
	}
}

func TestServiceResizePtyRejectsExecID(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-1", status: task.Status_RUNNING}
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
	}

	err := svc.ResizePty(context.Background(), runtime, ResizePtyInput{ID: taskHandle.id, ExecID: "exec-1", Height: 24, Width: 80})
	if !errors.Is(err, er.FlexibleTaskUnsupported) {
		t.Fatalf("ResizePty error = %v, want %v", err, er.FlexibleTaskUnsupported)
	}
}

func TestServiceResizePtyDoesNotHoldRuntimeLockWhileRestartingAttach(t *testing.T) {
	ttyIn, ttyOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("create tty pipe: %v", err)
	}
	defer ttyIn.Close()
	defer ttyOut.Close()

	manager := &fakeTaskIOManager{}
	svc := NewService(&fakeTaskIOFactory{
		manager: manager,
		stream:  &fakeTaskEventStream{},
	})
	taskHandle := &fakeTask{
		id:         "resize-attach",
		status:     task.Status_RUNNING,
		attachInfo: &ports.AttachInfo{Terminal: true},
	}
	runtime := &fakeRuntime{
		tasks:   map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{ttyIn: ttyOut, ttyOut: ttyIn},
	}

	done := make(chan error, 1)
	go func() {
		done <- svc.ResizePty(context.Background(), runtime, ResizePtyInput{ID: taskHandle.id, Height: 24, Width: 80})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ResizePty returned unexpected error: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ResizePty appears to be blocked by runtime lock re-entry")
	}
	if !manager.startCalled {
		t.Fatal("expected IO manager to be started during resize attach restart")
	}
}

func TestServiceMethodsRequireRuntime(t *testing.T) {
	svc := NewService(nil)
	ctx := context.Background()
	var typedNilRuntime *fakeRuntime
	cases := []struct {
		name string
		call func() error
	}{
		{name: "Create", call: func() error {
			_, err := svc.Create(ctx, nil, CreateInput{})
			return err
		}},
		{name: "Create typed nil runtime", call: func() error {
			_, err := svc.Create(ctx, typedNilRuntime, CreateInput{})
			return err
		}},
		{name: "Start", call: func() error {
			_, err := svc.Start(ctx, nil, StartInput{})
			return err
		}},
		{name: "Start typed nil runtime", call: func() error {
			_, err := svc.Start(ctx, typedNilRuntime, StartInput{})
			return err
		}},
		{name: "State", call: func() error {
			_, err := svc.State(ctx, nil, StateInput{})
			return err
		}},
		{name: "State typed nil runtime", call: func() error {
			_, err := svc.State(ctx, typedNilRuntime, StateInput{})
			return err
		}},
		{name: "Wait", call: func() error {
			_, err := svc.Wait(ctx, nil, WaitInput{})
			return err
		}},
		{name: "Wait typed nil runtime", call: func() error {
			_, err := svc.Wait(ctx, typedNilRuntime, WaitInput{})
			return err
		}},
		{name: "Pause", call: func() error {
			_, err := svc.Pause(ctx, nil, SignalInput{})
			return err
		}},
		{name: "Pause typed nil runtime", call: func() error {
			_, err := svc.Pause(ctx, typedNilRuntime, SignalInput{})
			return err
		}},
		{name: "Resume", call: func() error {
			_, err := svc.Resume(ctx, nil, SignalInput{})
			return err
		}},
		{name: "Resume typed nil runtime", call: func() error {
			_, err := svc.Resume(ctx, typedNilRuntime, SignalInput{})
			return err
		}},
		{name: "Kill", call: func() error {
			return svc.Kill(ctx, nil, KillInput{})
		}},
		{name: "Kill typed nil runtime", call: func() error {
			return svc.Kill(ctx, typedNilRuntime, KillInput{})
		}},
		{name: "Delete", call: func() error {
			_, err := svc.Delete(ctx, nil, DeleteInput{})
			return err
		}},
		{name: "Delete typed nil runtime", call: func() error {
			_, err := svc.Delete(ctx, typedNilRuntime, DeleteInput{})
			return err
		}},
		{name: "ResizePty", call: func() error {
			return svc.ResizePty(ctx, nil, ResizePtyInput{})
		}},
		{name: "ResizePty typed nil runtime", call: func() error {
			return svc.ResizePty(ctx, typedNilRuntime, ResizePtyInput{})
		}},
		{name: "CloseIO", call: func() error {
			return svc.CloseIO(ctx, nil, CloseIOInput{})
		}},
		{name: "CloseIO typed nil runtime", call: func() error {
			return svc.CloseIO(ctx, typedNilRuntime, CloseIOInput{})
		}},
		{name: "Update", call: func() error {
			return svc.Update(ctx, nil, UpdateInput{})
		}},
		{name: "Update typed nil runtime", call: func() error {
			return svc.Update(ctx, typedNilRuntime, UpdateInput{})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatalf("%s should require runtime", tc.name)
			}
		})
	}
}

func TestNewServiceIgnoresNilOptions(t *testing.T) {
	defer func() {
		if got := recover(); got != nil {
			t.Fatalf("NewService panicked with nil option: %v", got)
		}
	}()

	if svc := NewService(nil, nil); svc == nil {
		t.Fatal("NewService returned nil")
	}
}

func TestServiceCloseIOReleasesRuntimeLockBeforeWaitingForStdinCloser(t *testing.T) {
	svc := NewService(nil)
	stdinClosed := make(chan struct{})
	stdinCloser := make(chan struct{})
	taskHandle := &fakeTask{
		id:          "closeio-test",
		status:      task.Status_RUNNING,
		stdinPipe:   &trackingWriteCloser{closed: stdinClosed},
		stdinCloser: stdinCloser,
		exitCh:      make(chan struct{}),
	}
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{
			taskHandle.id: taskHandle,
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = svc.CloseIO(context.Background(), runtime, CloseIOInput{
			ID:         taskHandle.id,
			CloseStdin: true,
		})
	}()

	select {
	case <-stdinClosed:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("CloseIO did not close stdin pipe")
	}

	lockAcquired := make(chan struct{})
	go func() {
		runtime.Lock()
		runtime.Unlock() //nolint:staticcheck // SA2001: intentional — verifies lock is free
		close(lockAcquired)
	}()

	select {
	case <-lockAcquired:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("runtime lock remained held while CloseIO was waiting on stdinCloser")
	}

	close(stdinCloser)

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("CloseIO did not return after stdinCloser closed")
	}
}

func TestServiceCloseIORejectsExecID(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-1", status: task.Status_RUNNING}
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
	}

	err := svc.CloseIO(context.Background(), runtime, CloseIOInput{ID: taskHandle.id, ExecID: "exec-1"})
	if !errors.Is(err, er.FlexibleTaskUnsupported) {
		t.Fatalf("CloseIO error = %v, want %v", err, er.FlexibleTaskUnsupported)
	}
}

func TestForceStopIfActiveToleratesMissingExitSignal(t *testing.T) {
	svc := NewService(nil)
	now := time.Date(2026, 4, 27, 1, 2, 3, 0, time.UTC)
	svc.now = func() time.Time { return now }
	taskHandle := &fakeTask{
		id:     "force-stop-missing-exit-signal",
		status: task.Status_RUNNING,
	}

	svc.forceStopIfActive(taskHandle)

	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("expected task status %s, got %s", task.Status_STOPPED, taskHandle.status)
	}
	if taskHandle.exitStatus != exitstatus.Interrupt() {
		t.Fatalf("expected exit status %d, got %d", exitstatus.Interrupt(), taskHandle.exitStatus)
	}
	if !taskHandle.exitTime.Equal(now) {
		t.Fatalf("expected exit time %s, got %s", now, taskHandle.exitTime)
	}
}

func TestNewServiceAcceptsClockOption(t *testing.T) {
	now := time.Date(2026, 4, 27, 9, 10, 11, 0, time.UTC)
	attach, err := attachapp.NewServiceChecked(nil, attachapp.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("attach service setup failed: %v", err)
	}
	lifecycle, err := lifecycleapp.NewServiceChecked(attach, lifecycleapp.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("lifecycle service setup failed: %v", err)
	}
	svc, err := NewServiceChecked(attach, lifecycle, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewServiceChecked returned error: %v", err)
	}

	if got := svc.clockNow(); !got.Equal(now) {
		t.Fatalf("clockNow() = %v, want %v", got, now)
	}
}

func TestNewServiceUsesInjectedApplicationServices(t *testing.T) {
	attach, err := attachapp.NewServiceChecked(nil)
	if err != nil {
		t.Fatalf("attach service setup failed: %v", err)
	}
	lifecycle, err := lifecycleapp.NewServiceChecked(attach)
	if err != nil {
		t.Fatalf("lifecycle service setup failed: %v", err)
	}

	svc, err := NewServiceChecked(attach, lifecycle)
	if err != nil {
		t.Fatalf("NewServiceChecked returned error: %v", err)
	}

	if svc.attach != attach {
		t.Fatal("NewService did not use injected attach service")
	}
	if svc.lifecycle != lifecycle {
		t.Fatal("NewService did not use injected lifecycle service")
	}
}

func TestNewServiceCheckedRejectsMismatchedApplicationServices(t *testing.T) {
	attach, err := attachapp.NewServiceChecked(nil)
	if err != nil {
		t.Fatalf("attach service setup failed: %v", err)
	}
	otherAttach, err := attachapp.NewServiceChecked(nil)
	if err != nil {
		t.Fatalf("other attach service setup failed: %v", err)
	}
	lifecycle, err := lifecycleapp.NewServiceChecked(otherAttach)
	if err != nil {
		t.Fatalf("lifecycle service setup failed: %v", err)
	}

	svc, err := NewServiceChecked(attach, lifecycle)
	if err == nil {
		t.Fatal("expected mismatched application services to fail")
	}
	if !errors.Is(err, ErrMismatchedApplicationServices) {
		t.Fatalf("NewServiceChecked error = %v, want inconsistent application services", err)
	}
	if svc != nil {
		t.Fatal("expected no service when application services are inconsistent")
	}
}

func TestNewServiceCheckedRejectsPartialApplicationServiceInjection(t *testing.T) {
	attach, err := attachapp.NewServiceChecked(nil)
	if err != nil {
		t.Fatalf("attach service setup failed: %v", err)
	}
	lifecycle, err := lifecycleapp.NewServiceChecked(attach)
	if err != nil {
		t.Fatalf("lifecycle service setup failed: %v", err)
	}

	svc, err := NewServiceChecked(nil, lifecycle)
	if err == nil {
		t.Fatal("expected partial application service injection to fail")
	}
	if !errors.Is(err, ErrApplicationServicesRequired) {
		t.Fatalf("NewServiceChecked error = %v, want incomplete application services sentinel", err)
	}
	if svc != nil {
		t.Fatal("expected no service when application service injection is incomplete")
	}
}

func TestForceStopIfActiveToleratesClosedExitSignal(t *testing.T) {
	attach, err := attachapp.NewServiceChecked(nil)
	if err != nil {
		t.Fatalf("attach service setup failed: %v", err)
	}
	lifecycle, err := lifecycleapp.NewServiceChecked(attach)
	if err != nil {
		t.Fatalf("lifecycle service setup failed: %v", err)
	}
	svc, err := NewServiceChecked(attach, lifecycle)
	if err != nil {
		t.Fatalf("NewServiceChecked returned error: %v", err)
	}
	exitCh := make(chan struct{})
	close(exitCh)
	taskHandle := &fakeTask{
		id:     "force-stop-closed-exit-signal",
		status: task.Status_RUNNING,
		exitCh: exitCh,
	}

	svc.forceStopIfActive(taskHandle)

	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("expected task status %s, got %s", task.Status_STOPPED, taskHandle.status)
	}
}

func TestServiceWaitRejectsMissingExitSignal(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:     "task-wait-missing-exit-signal",
		status: task.Status_RUNNING,
	}
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
	}

	_, err := svc.Wait(context.Background(), runtime, WaitInput{ID: taskHandle.id})
	if err == nil {
		t.Fatal("expected Wait to reject nil exit channel")
	}
}

func TestServiceWaitHonorsCanceledContextBeforeSnapshot(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:     "task-wait-canceled",
		status: task.Status_RUNNING,
	}
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Wait(ctx, runtime, WaitInput{ID: taskHandle.id})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait canceled error = %v, want context.Canceled", err)
	}
}

func TestServiceWaitAcceptsNilContext(t *testing.T) {
	svc := NewService(nil)
	exitCh := make(chan struct{})
	close(exitCh)
	taskHandle := &fakeTask{
		id:         "task-wait-nil-context",
		status:     task.Status_RUNNING,
		exitStatus: 7,
		exitCh:     exitCh,
	}
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
	}

	out, err := svc.Wait(nil, runtime, WaitInput{ID: taskHandle.id})
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if out.ExitStatus != taskHandle.exitStatus {
		t.Fatalf("Wait exit status = %d, want %d", out.ExitStatus, taskHandle.exitStatus)
	}
}

func TestServiceUpdateReturnsRuntimeUpdateError(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:     "task-update-error",
		status: task.Status_RUNNING,
		exitCh: make(chan struct{}),
	}
	expectedErr := errors.New("update failed")
	runtime := &fakeRuntime{
		tasks:   map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{updateErr: expectedErr},
	}

	err := svc.Update(context.Background(), runtime, UpdateInput{ID: taskHandle.id})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Update error = %v, want %v", err, expectedErr)
	}
}

func TestServiceUpdateMissingTaskReturnsNotFound(t *testing.T) {
	svc := NewService(nil)
	runtime := &fakeRuntime{
		tasks: make(map[string]ports.Task),
	}

	err := svc.Update(context.Background(), runtime, UpdateInput{ID: "missing"})
	if !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("Update error = %v, want %v", err, er.ContainerNotFound)
	}
}

func TestServiceUpdateRequiresSandbox(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:     "task-update-no-sandbox",
		status: task.Status_RUNNING,
		exitCh: make(chan struct{}),
	}
	runtime := &fakeRuntime{
		tasks:   map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: nil,
	}

	err := svc.Update(context.Background(), runtime, UpdateInput{ID: taskHandle.id})
	if !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("Update error = %v, want %v", err, er.SandboxNotFound)
	}
}

func TestServiceUpdateRequiresTypedNilSandbox(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:     "task-update-typed-nil-sandbox",
		status: task.Status_RUNNING,
		exitCh: make(chan struct{}),
	}
	var typedNilSandbox *fakeSandbox
	runtime := &fakeRuntime{
		tasks:   map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: typedNilSandbox,
	}

	err := svc.Update(context.Background(), runtime, UpdateInput{ID: taskHandle.id})
	if !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("Update error = %v, want %v", err, er.SandboxNotFound)
	}
}

func TestServiceUpdateReleasesRuntimeLockDuringSandboxUpdate(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-update", status: task.Status_RUNNING}
	updateStarted := make(chan struct{})
	releaseUpdate := make(chan struct{})
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{
			updateStarted: updateStarted,
			releaseUpdate: releaseUpdate,
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = svc.Update(context.Background(), runtime, UpdateInput{ID: taskHandle.id})
	}()

	select {
	case <-updateStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Update did not reach sandbox update")
	}

	lockAcquired := make(chan struct{})
	go func() {
		runtime.Lock()
		runtime.Unlock() //nolint:staticcheck // SA2001: intentional lock availability check
		close(lockAcquired)
	}()

	select {
	case <-lockAcquired:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("runtime lock remained held during sandbox update")
	}

	close(releaseUpdate)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Update did not return after sandbox update completed")
	}
}

func TestServicePauseReleasesRuntimeLockDuringSandboxPause(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-pause-lock", status: task.Status_RUNNING}
	pauseStarted := make(chan struct{})
	releasePause := make(chan struct{})
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{
			pauseStarted: pauseStarted,
			releasePause: releasePause,
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.Pause(context.Background(), runtime, SignalInput{ID: taskHandle.id})
	}()

	select {
	case <-pauseStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Pause did not reach sandbox pause")
	}

	lockAcquired := make(chan struct{})
	go func() {
		runtime.Lock()
		if taskHandle.status != task.Status_PAUSING {
			t.Errorf("status during sandbox pause = %s, want PAUSING", taskHandle.status)
		}
		runtime.Unlock()
		close(lockAcquired)
	}()

	select {
	case <-lockAcquired:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("runtime lock remained held during sandbox pause")
	}

	close(releasePause)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Pause did not return after sandbox pause completed")
	}
	if taskHandle.status != task.Status_PAUSED {
		t.Fatalf("status after sandbox pause = %s, want PAUSED", taskHandle.status)
	}
}

func TestServiceResumeReleasesRuntimeLockDuringSandboxResume(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-resume-lock", status: task.Status_PAUSED}
	resumeStarted := make(chan struct{})
	releaseResume := make(chan struct{})
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{
			resumeStarted: resumeStarted,
			releaseResume: releaseResume,
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.Resume(context.Background(), runtime, SignalInput{ID: taskHandle.id})
	}()

	select {
	case <-resumeStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Resume did not reach sandbox resume")
	}

	lockAcquired := make(chan struct{})
	go func() {
		runtime.Lock()
		runtime.Unlock() //nolint:staticcheck // SA2001: intentional lock availability check
		close(lockAcquired)
	}()

	select {
	case <-lockAcquired:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("runtime lock remained held during sandbox resume")
	}

	close(releaseResume)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Resume did not return after sandbox resume completed")
	}
	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("status after sandbox resume = %s, want RUNNING", taskHandle.status)
	}
}

func TestServicePauseRefreshesTaskStatusAfterSandboxFailure(t *testing.T) {
	svc := NewService(nil)
	expectedErr := errors.New("pause failed")
	taskHandle := &fakeTask{
		id:     "task-pause",
		status: task.Status_RUNNING,
		exitCh: make(chan struct{}),
	}
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{
			pauseErr: expectedErr,
		},
		queryStat: task.Status_STOPPED,
	}

	out, err := svc.Pause(context.Background(), runtime, SignalInput{ID: taskHandle.id})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Pause error = %v, want %v", err, expectedErr)
	}
	if out.ContainerID != taskHandle.id {
		t.Fatalf("unexpected task id %q", out.ContainerID)
	}
	if out.EmitEvent {
		t.Fatal("Pause should not emit event on sandbox failure")
	}
	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("expected reconciled status %s, got %s", task.Status_STOPPED, taskHandle.status)
	}
}

func TestServicePauseRequiresSandboxBeforeMutatingStatus(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:     "task-pause-no-sandbox",
		status: task.Status_RUNNING,
		exitCh: make(chan struct{}),
	}
	runtime := &fakeRuntime{
		tasks:   map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: nil,
	}

	_, err := svc.Pause(context.Background(), runtime, SignalInput{ID: taskHandle.id})
	if !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("Pause error = %v, want SandboxNotFound", err)
	}
	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("task status = %s, want RUNNING", taskHandle.status)
	}
}

func TestServiceResumeRefreshesTaskStatusAfterSandboxFailure(t *testing.T) {
	svc := NewService(nil)
	expectedErr := errors.New("resume failed")
	taskHandle := &fakeTask{
		id:     "task-resume",
		status: task.Status_PAUSED,
		exitCh: make(chan struct{}),
	}
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{
			resumeErr: expectedErr,
		},
		queryStat: task.Status_STOPPED,
	}

	out, err := svc.Resume(context.Background(), runtime, SignalInput{ID: taskHandle.id})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Resume error = %v, want %v", err, expectedErr)
	}
	if out.ContainerID != taskHandle.id {
		t.Fatalf("unexpected task id %q", out.ContainerID)
	}
	if out.EmitEvent {
		t.Fatal("Resume should not emit event on sandbox failure")
	}
	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("expected reconciled status %s, got %s", task.Status_STOPPED, taskHandle.status)
	}
}

func TestServicePauseHonorsCanceledContextBeforeMutation(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-pause-canceled", status: task.Status_RUNNING}
	pauseStarted := make(chan struct{})
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{
			pauseStarted: pauseStarted,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Pause(ctx, runtime, SignalInput{ID: taskHandle.id})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Pause canceled error = %v, want context.Canceled", err)
	}
	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("task status = %s, want %s", taskHandle.status, task.Status_RUNNING)
	}
	select {
	case <-pauseStarted:
		t.Fatal("Pause called sandbox after context cancellation")
	default:
	}
}

func TestServiceKillSandboxTaskMarksKilledAndClearsSandbox(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:         "task-kill-sandbox",
		status:     task.Status_RUNNING,
		exitCh:     make(chan struct{}),
		canSandbox: true,
	}
	runtime := &fakeRuntime{
		tasks:   map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{},
	}

	err := svc.Kill(context.Background(), runtime, KillInput{ID: taskHandle.id, Signal: 9})
	if err != nil {
		t.Fatalf("Kill returned unexpected error: %v", err)
	}
	if !runtime.killed {
		t.Fatal("expected runtime to be marked as killed by API")
	}
	if runtime.sandbox != nil {
		t.Fatal("expected sandbox to be cleared after sandbox kill")
	}
	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("expected task status %s, got %s", task.Status_STOPPED, taskHandle.status)
	}
	if !taskHandle.ioExited {
		t.Fatal("expected IOExit to be called for sandbox kill")
	}
}

func TestServiceKillSandboxTaskReturnsStopError(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:         "task-kill-sandbox-stop-error",
		status:     task.Status_RUNNING,
		exitCh:     make(chan struct{}),
		canSandbox: true,
	}
	expectedErr := errors.New("stop failed")
	sandbox := &fakeSandbox{stopErr: expectedErr}
	runtime := &fakeRuntime{
		tasks:   map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: sandbox,
	}

	err := svc.Kill(context.Background(), runtime, KillInput{ID: taskHandle.id, Signal: 9})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Kill error = %v, want %v", err, expectedErr)
	}
	if runtime.sandbox != sandbox {
		t.Fatal("sandbox should remain available for retry after stop failure")
	}
	if runtime.killed {
		t.Fatal("runtime should not be marked killed when sandbox stop fails")
	}
	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("task status = %s, want %s", taskHandle.status, task.Status_RUNNING)
	}
}

func TestServiceKillRejectsExecID(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "task-1", status: task.Status_RUNNING}
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
	}

	err := svc.Kill(context.Background(), runtime, KillInput{ID: taskHandle.id, ExecID: "exec-1", Signal: 9})
	if !errors.Is(err, er.FlexibleTaskUnsupported) {
		t.Fatalf("Kill error = %v, want %v", err, er.FlexibleTaskUnsupported)
	}
	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("task status = %s, want %s", taskHandle.status, task.Status_RUNNING)
	}
}

func TestServiceDeleteRejectsExecIDBeforeStoppingTask(t *testing.T) {
	svc := NewService(nil)
	exitCh := make(chan struct{})
	taskHandle := &fakeTask{id: "task-1", status: task.Status_RUNNING, exitCh: exitCh}
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
	}

	_, err := svc.Delete(context.Background(), runtime, DeleteInput{ID: taskHandle.id, ExecID: "exec-1"})
	if !errors.Is(err, er.FlexibleTaskUnsupported) {
		t.Fatalf("Delete error = %v, want %v", err, er.FlexibleTaskUnsupported)
	}
	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("task status = %s, want %s", taskHandle.status, task.Status_RUNNING)
	}
	select {
	case <-exitCh:
		t.Fatal("exec delete should not close the main task exit signal")
	default:
	}
}

func TestServiceDeleteSandboxTaskReturnsStopError(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:         "task-delete-sandbox-stop-error",
		status:     task.Status_STOPPED,
		exitCh:     make(chan struct{}),
		canSandbox: true,
	}
	expectedErr := errors.New("stop failed")
	sandbox := &fakeSandbox{stopErr: expectedErr}
	runtime := &fakeRuntime{
		tasks:   map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: sandbox,
	}

	_, err := svc.Delete(context.Background(), runtime, DeleteInput{ID: taskHandle.id})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Delete error = %v, want %v", err, expectedErr)
	}
	if runtime.sandbox != sandbox {
		t.Fatal("sandbox should remain available for retry after delete stop failure")
	}
}

func TestServiceDeleteReleasesRuntimeLockDuringSandboxStop(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:         "sandbox-delete-lock",
		status:     task.Status_STOPPED,
		canSandbox: true,
		exitCh:     make(chan struct{}),
	}
	stopStarted := make(chan struct{})
	releaseStop := make(chan struct{})
	runtime := &fakeRuntime{
		shimPID: 1,
		tasks: map[string]ports.Task{
			taskHandle.id: taskHandle,
		},
		sandbox: &fakeSandbox{
			stopStarted: stopStarted,
			releaseStop: releaseStop,
		},
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.Delete(context.Background(), runtime, DeleteInput{ID: taskHandle.id})
		done <- err
	}()

	select {
	case <-stopStarted:
	case <-time.After(time.Second):
		t.Fatal("Delete did not reach sandbox stop")
	}

	lockReleased := make(chan struct{})
	go func() {
		runtime.Lock()
		runtime.Unlock() //nolint:staticcheck // SA2001: intentional lock availability check
		close(lockReleased)
	}()

	select {
	case <-lockReleased:
	case <-time.After(time.Second):
		t.Fatal("runtime lock remained held during sandbox stop")
	}

	close(releaseStop)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Delete returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Delete did not return after sandbox stop completed")
	}
}

func TestServiceDeleteRemovesTaskFromRuntimeOnSuccess(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:         "task-delete-success",
		status:     task.Status_STOPPED,
		exitStatus: 7,
		exitTime:   time.Now(),
		exitCh:     make(chan struct{}),
	}
	runtime := &fakeRuntime{
		shimPID: 99,
		tasks:   map[string]ports.Task{taskHandle.id: taskHandle},
	}

	out, err := svc.Delete(context.Background(), runtime, DeleteInput{ID: taskHandle.id})
	if err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if _, ok := runtime.tasks[taskHandle.id]; ok {
		t.Fatal("deleted task remained in runtime registry")
	}
	if out.ContainerID != taskHandle.id || out.ExitStatus != taskHandle.exitStatus || out.Pid != runtime.shimPID {
		t.Fatalf("Delete output = %+v, want id/status/shim pid", out)
	}
}

func TestServiceDeleteMissingTaskUsesInjectedClock(t *testing.T) {
	svc := NewService(nil)
	now := time.Date(2026, 4, 27, 4, 5, 6, 0, time.UTC)
	svc.now = func() time.Time { return now }
	runtime := &fakeRuntime{
		shimPID: 99,
		tasks:   map[string]ports.Task{},
	}

	out, err := svc.Delete(context.Background(), runtime, DeleteInput{ID: "missing"})
	if err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if !out.NotFound || out.ContainerID != "missing" || out.Pid != runtime.shimPID {
		t.Fatalf("Delete missing output = %+v, want not-found id and shim pid", out)
	}
	if !out.ExitedAt.Equal(now) {
		t.Fatalf("Delete missing exit time = %s, want %s", out.ExitedAt, now)
	}
}

func TestServiceKillSIGINTStopsTaskWithInterruptStatus(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:     "task-kill-sigint",
		status: task.Status_RUNNING,
		exitCh: make(chan struct{}),
	}
	runtime := &fakeRuntime{
		tasks:   map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{},
	}

	err := svc.Kill(context.Background(), runtime, KillInput{ID: taskHandle.id, Signal: 2})
	if err != nil {
		t.Fatalf("Kill returned unexpected error: %v", err)
	}
	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("expected task status %s, got %s", task.Status_STOPPED, taskHandle.status)
	}
	if taskHandle.exitStatus != exitstatus.Interrupt() {
		t.Fatalf("expected SIGINT exit status %d, got %d", exitstatus.Interrupt(), taskHandle.exitStatus)
	}
	if taskHandle.exitTime.IsZero() {
		t.Fatal("expected exit time to be set")
	}
	if !runtime.killed {
		t.Fatal("expected runtime to be marked as killed by API")
	}
	if !taskHandle.ioExited {
		t.Fatal("expected IOExit to be called for SIGINT")
	}
}

func TestServiceKillReleasesRuntimeLockDuringSandboxKill(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:     "task-kill-lock",
		status: task.Status_RUNNING,
		exitCh: make(chan struct{}),
	}
	killStarted := make(chan struct{})
	releaseKill := make(chan struct{})
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{
			killStarted: killStarted,
			releaseKill: releaseKill,
		},
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = svc.Kill(context.Background(), runtime, KillInput{ID: taskHandle.id, Signal: signalKill})
	}()

	select {
	case <-killStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Kill did not reach sandbox kill")
	}

	lockAcquired := make(chan struct{})
	go func() {
		runtime.Lock()
		runtime.Unlock() //nolint:staticcheck // SA2001: intentional lock availability check
		close(lockAcquired)
	}()

	select {
	case <-lockAcquired:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("runtime lock remained held during sandbox kill")
	}

	close(releaseKill)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Kill did not return after sandbox kill completed")
	}
	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("status after sandbox kill = %s, want STOPPED", taskHandle.status)
	}
}

func TestServiceKillStopAndContinueSignalsUpdateStatus(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:     "signal-task",
		status: task.Status_RUNNING,
		exitCh: make(chan struct{}),
	}
	runtime := &fakeRuntime{
		tasks:   map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{},
	}

	if err := svc.Kill(context.Background(), runtime, KillInput{ID: taskHandle.id, Signal: signalStop}); err != nil {
		t.Fatalf("SIGSTOP Kill returned error: %v", err)
	}
	if taskHandle.status != task.Status_PAUSED {
		t.Fatalf("status after SIGSTOP = %s, want PAUSED", taskHandle.status)
	}

	if err := svc.Kill(context.Background(), runtime, KillInput{ID: taskHandle.id, Signal: signalContinue}); err != nil {
		t.Fatalf("SIGCONT Kill returned error: %v", err)
	}
	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("status after SIGCONT = %s, want RUNNING", taskHandle.status)
	}
}

func TestServiceKillStopSignalMarksPausingDuringSandboxPause(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "signal-pause-lock", status: task.Status_RUNNING}
	pauseStarted := make(chan struct{})
	releasePause := make(chan struct{})
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{
			pauseStarted: pauseStarted,
			releasePause: releasePause,
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- svc.Kill(context.Background(), runtime, KillInput{ID: taskHandle.id, Signal: signalStop})
	}()

	select {
	case <-pauseStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("SIGSTOP did not reach sandbox pause")
	}
	runtime.Lock()
	statusDuringPause := taskHandle.status
	runtime.Unlock()
	if statusDuringPause != task.Status_PAUSING {
		t.Fatalf("status during SIGSTOP pause = %s, want PAUSING", statusDuringPause)
	}

	close(releasePause)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SIGSTOP Kill returned error: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("SIGSTOP did not return after sandbox pause completed")
	}
	if taskHandle.status != task.Status_PAUSED {
		t.Fatalf("status after SIGSTOP = %s, want PAUSED", taskHandle.status)
	}
}

func TestServiceKillStopSignalNoopsWhenAlreadyPaused(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "signal-already-paused", status: task.Status_PAUSED}
	pauseStarted := make(chan struct{})
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{
			pauseStarted: pauseStarted,
		},
	}

	if err := svc.Kill(context.Background(), runtime, KillInput{ID: taskHandle.id, Signal: signalStop}); err != nil {
		t.Fatalf("SIGSTOP Kill returned error: %v", err)
	}
	if taskHandle.status != task.Status_PAUSED {
		t.Fatalf("status after repeated SIGSTOP = %s, want PAUSED", taskHandle.status)
	}
	select {
	case <-pauseStarted:
		t.Fatal("SIGSTOP should not call sandbox pause for an already paused task")
	default:
	}
}

func TestServiceKillContinueSignalRequiresSandboxWhenPaused(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "signal-continue-no-sandbox", status: task.Status_PAUSED}
	runtime := &fakeRuntime{
		tasks:   map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: nil,
	}

	err := svc.Kill(context.Background(), runtime, KillInput{ID: taskHandle.id, Signal: signalContinue})
	if !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("SIGCONT Kill error = %v, want SandboxNotFound", err)
	}
	if taskHandle.status != task.Status_PAUSED {
		t.Fatalf("status after failed SIGCONT = %s, want PAUSED", taskHandle.status)
	}
}

func TestServiceKillContinueSignalNoopsWhenAlreadyRunningWithoutSandbox(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "signal-continue-running", status: task.Status_RUNNING}
	runtime := &fakeRuntime{
		tasks:   map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: nil,
	}

	if err := svc.Kill(context.Background(), runtime, KillInput{ID: taskHandle.id, Signal: signalContinue}); err != nil {
		t.Fatalf("SIGCONT Kill returned error: %v", err)
	}
	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("status after repeated SIGCONT = %s, want RUNNING", taskHandle.status)
	}
}

func TestServiceKillContinueSignalNoopsWhenStoppedWithoutSandbox(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{id: "signal-continue-stopped", status: task.Status_STOPPED}
	runtime := &fakeRuntime{
		tasks:   map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: nil,
	}

	if err := svc.Kill(context.Background(), runtime, KillInput{ID: taskHandle.id, Signal: signalContinue}); err != nil {
		t.Fatalf("SIGCONT Kill returned error: %v", err)
	}
	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("status after stopped SIGCONT = %s, want STOPPED", taskHandle.status)
	}
}

func TestServiceKillRegularTaskReconcilesStatusOnFailure(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:     "task-kill-regular",
		status: task.Status_RUNNING,
		exitCh: make(chan struct{}),
	}
	runtime := &fakeRuntime{
		tasks: map[string]ports.Task{taskHandle.id: taskHandle},
		sandbox: &fakeSandbox{
			killErr: errors.New("kill failed"),
		},
		queryStat: task.Status_PAUSED,
	}

	err := svc.Kill(context.Background(), runtime, KillInput{ID: taskHandle.id, Signal: 9})
	if err == nil {
		t.Fatal("expected Kill to return sandbox error")
	}
	if taskHandle.status != task.Status_PAUSED {
		t.Fatalf("expected reconciled status %s, got %s", task.Status_PAUSED, taskHandle.status)
	}
	if runtime.killed {
		t.Fatal("runtime should not be marked killed when kill fails")
	}
}
