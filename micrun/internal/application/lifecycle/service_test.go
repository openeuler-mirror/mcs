package lifecycle

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	attachapp "micrun/internal/application/attach"
	"micrun/internal/application/exitstatus"
	"micrun/internal/ports"
	ann "micrun/internal/support/annotations"
	er "micrun/internal/support/errors"

	"github.com/containerd/containerd/api/types/task"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type fakeLifecycleIOManager struct {
	startCalled bool
	startErr    error
	stopCalled  bool
}

func (f *fakeLifecycleIOManager) Start() error                                    { f.startCalled = true; return f.startErr }
func (f *fakeLifecycleIOManager) Stop()                                           { f.stopCalled = true }
func (f *fakeLifecycleIOManager) StopWithoutClosingFIFOs()                        {}
func (f *fakeLifecycleIOManager) Restart() error                                  { return nil }
func (f *fakeLifecycleIOManager) RestartWithTTYs(io.WriteCloser, io.Reader) error { return nil }
func (f *fakeLifecycleIOManager) RestartWithSubscriber(io.WriteCloser, io.Reader, func(ports.IOEventStream)) error {
	return nil
}
func (f *fakeLifecycleIOManager) IsRunning() bool { return f.startCalled }
func (f *fakeLifecycleIOManager) EventStream() ports.IOEventStream {
	return &fakeLifecycleEventStream{}
}

type fakeLifecycleEventStream struct{}

func (f *fakeLifecycleEventStream) Current() bool { return true }

func (f *fakeLifecycleEventStream) SubscribeMany(eventTypes ...ports.IOEventType) ports.IOEventSubscriber {
	ch := make(chan ports.IOEvent)
	return ch
}

type fakeLifecycleIOFactory struct {
	manager ports.IOManager
	stream  ports.IOEventStream
}

func (f *fakeLifecycleIOFactory) NewSession(ctx context.Context, config ports.IOSessionConfig) (ports.IOManager, ports.IOEventStream, error) {
	return f.manager, f.stream, nil
}

func (f *fakeLifecycleIOFactory) IsValidFIFOPath(path string) bool { return path != "" }
func (f *fakeLifecycleIOFactory) GenerateFIFOPath(namespace, containerID, stream string) string {
	return "/generated/" + namespace + "/" + containerID + "/" + stream
}

type fakeLifecycleSandbox struct {
	startContainerID string
	stopped          bool
	deleted          bool
	stopContainerID  string
	stdin            io.WriteCloser
	stdout           io.Reader
	stderr           io.Reader
	ioErr            error
}

func (f *fakeLifecycleSandbox) SandboxID() string               { return "sandbox-test" }
func (f *fakeLifecycleSandbox) Start(ctx context.Context) error { return nil }
func (f *fakeLifecycleSandbox) StartContainer(ctx context.Context, id string) error {
	f.startContainerID = id
	return nil
}
func (f *fakeLifecycleSandbox) Stop(ctx context.Context, force bool) error {
	f.stopped = true
	return nil
}
func (f *fakeLifecycleSandbox) StopContainer(ctx context.Context, id string, force bool) error {
	f.stopContainerID = id
	return nil
}
func (f *fakeLifecycleSandbox) Delete(ctx context.Context) error {
	f.deleted = true
	return nil
}
func (f *fakeLifecycleSandbox) PauseContainer(ctx context.Context, id string) error  { return nil }
func (f *fakeLifecycleSandbox) ResumeContainer(ctx context.Context, id string) error { return nil }
func (f *fakeLifecycleSandbox) KillContainer(ctx context.Context, id string) error   { return nil }
func (f *fakeLifecycleSandbox) IOStream(ctx context.Context, containerID, taskID string) (io.WriteCloser, io.Reader, io.Reader, error) {
	if f.ioErr != nil {
		return nil, nil, nil, f.ioErr
	}
	stdin, stdout, stderr := f.stdin, f.stdout, f.stderr
	if stdin == nil {
		stdin = &lifecycleWriteCloser{}
	}
	if stdout == nil {
		stdout = strings.NewReader("hello")
	}
	if stderr == nil {
		stderr = strings.NewReader("hello")
	}
	return stdin, stdout, stderr, nil
}
func (f *fakeLifecycleSandbox) WinResize(ctx context.Context, containerID string, height, width uint32) error {
	return nil
}
func (f *fakeLifecycleSandbox) OpenTTYs(ctx context.Context, containerID string) (*os.File, *os.File, error) {
	return nil, nil, nil
}
func (f *fakeLifecycleSandbox) UpdateContainer(ctx context.Context, id string, resources specs.LinuxResources) error {
	return nil
}

func (f *fakeLifecycleSandbox) WaitContainerExit(ctx context.Context, containerID string) (int32, error) {
	// Block until canceled, mimicking the real sandbox which waits for the
	// guest domain to disappear.
	<-ctx.Done()
	return 0, ctx.Err()
}

type fakeLifecycleRuntime struct {
	mu             sync.Mutex
	namespace      string
	shimPID        uint32
	ctx            context.Context
	sandbox        ports.Sandbox
	reportedTask   ports.Task
	reportedStatus int
	reportedAt     time.Time
}

func (f *fakeLifecycleRuntime) Lock()                              { f.mu.Lock() }
func (f *fakeLifecycleRuntime) Unlock()                            { f.mu.Unlock() }
func (f *fakeLifecycleRuntime) Namespace() string                  { return f.namespace }
func (f *fakeLifecycleRuntime) BackgroundContext() context.Context { return f.ctx }
func (f *fakeLifecycleRuntime) ShimPID() uint32                    { return f.shimPID }
func (f *fakeLifecycleRuntime) Sandbox() ports.Sandbox             { return f.sandbox }
func (f *fakeLifecycleRuntime) SetSandbox(sandbox ports.Sandbox)   { f.sandbox = sandbox }
func (f *fakeLifecycleRuntime) QueryTaskStatus(ctx context.Context, id string) (task.Status, error) {
	return task.Status_UNKNOWN, nil
}
func (f *fakeLifecycleRuntime) MarkKilledByAPI() {}
func (f *fakeLifecycleRuntime) ReportTaskExit(task ports.Task, status int, exitedAt time.Time) {
	f.reportedTask = task
	f.reportedStatus = status
	f.reportedAt = exitedAt
}

type fakeLifecycleTask struct {
	id          string
	status      task.Status
	stdin       string
	stdout      string
	stderr      string
	stdinPipe   io.WriteCloser
	stdinCloser chan struct{}
	exitCh      chan struct{}
	ioManager   ports.IOManager
	attachInfo  *ports.AttachInfo
	ioExit      bool
	canSandbox  bool
	criSandbox  bool
	recovered   bool
	annotations map[string]string
	exitStatus  uint32
	exitTime    time.Time
}

func (f *fakeLifecycleTask) ID() string                   { return f.id }
func (f *fakeLifecycleTask) Bundle() string               { return "" }
func (f *fakeLifecycleTask) PID() uint32                  { return 0 }
func (f *fakeLifecycleTask) Status() task.Status          { return f.status }
func (f *fakeLifecycleTask) SetStatus(status task.Status) { f.status = status }
func (f *fakeLifecycleTask) Terminal() bool               { return false }
func (f *fakeLifecycleTask) StdinPath() string            { return f.stdin }
func (f *fakeLifecycleTask) StdoutPath() string           { return f.stdout }
func (f *fakeLifecycleTask) StderrPath() string           { return f.stderr }
func (f *fakeLifecycleTask) ExitStatus() uint32           { return f.exitStatus }
func (f *fakeLifecycleTask) ExitTime() time.Time          { return f.exitTime }
func (f *fakeLifecycleTask) SetExitInfo(status uint32, exitedAt time.Time) {
	f.exitStatus, f.exitTime = status, exitedAt
}
func (f *fakeLifecycleTask) StdinPipe() io.WriteCloser  { return f.stdinPipe }
func (f *fakeLifecycleTask) StdinCloser() chan struct{} { return f.stdinCloser }
func (f *fakeLifecycleTask) ExitChan() chan struct{}    { return f.exitCh }
func (f *fakeLifecycleTask) IOExit() {
	f.ioExit = true
	// Mirror shimContainer.ioExit: close via sync.Once equivalent.
	// Use recover to guard against double-close in test scenarios.
	defer func() { _ = recover() }()
	close(f.exitCh)
}
func (f *fakeLifecycleTask) CanBeSandbox() bool                   { return f.canSandbox }
func (f *fakeLifecycleTask) IsCriSandbox() bool                   { return f.criSandbox }
func (f *fakeLifecycleTask) IsRecovered() bool                    { return f.recovered }
func (f *fakeLifecycleTask) Annotations() map[string]string       { return f.annotations }
func (f *fakeLifecycleTask) IOManager() ports.IOManager           { return f.ioManager }
func (f *fakeLifecycleTask) SetIOManager(m ports.IOManager)       { f.ioManager = m }
func (f *fakeLifecycleTask) AttachInfo() *ports.AttachInfo        { return f.attachInfo }
func (f *fakeLifecycleTask) SetAttachInfo(info *ports.AttachInfo) { f.attachInfo = info }
func (f *fakeLifecycleTask) SetStdinPipe(pipe io.WriteCloser)     { f.stdinPipe = pipe }
func (f *fakeLifecycleTask) SetAttached(attached bool) bool       { return false }
func (f *fakeLifecycleTask) IsAttached() bool                     { return false }

type lifecycleWriteCloser struct {
	closed bool
}

func (l *lifecycleWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (l *lifecycleWriteCloser) Close() error {
	l.closed = true
	return nil
}

type lifecycleReadCloser struct {
	reader *strings.Reader
	closed bool
}

func (l *lifecycleReadCloser) Read(p []byte) (int, error) {
	return l.reader.Read(p)
}

func (l *lifecycleReadCloser) Close() error {
	l.closed = true
	return nil
}

type lifecycleCountingReadCloser struct {
	closeCount int
}

func (l *lifecycleCountingReadCloser) Read(p []byte) (int, error) {
	return 0, io.EOF
}

func (l *lifecycleCountingReadCloser) Close() error {
	l.closeCount++
	return nil
}

func TestServiceStartRequiresRuntime(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeLifecycleTask{id: "task-missing-runtime"}

	err := svc.Start(context.Background(), nil, taskHandle)
	if err == nil {
		t.Fatal("expected Start to require runtime")
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

func TestTaskIOStreamsCloseSharedOutputOnce(t *testing.T) {
	shared := &lifecycleCountingReadCloser{}
	streams := taskIOStreams{
		stdout: shared,
		stderr: shared,
	}

	streams.closeForTask("shared-output")

	if shared.closeCount != 1 {
		t.Fatalf("shared output closeCount = %d, want 1", shared.closeCount)
	}
}

func TestLifecycleEventContextFallsBackForNilRuntime(t *testing.T) {
	fallback, cancel := context.WithCancel(context.Background())

	got := lifecycleEventContext(fallback, nil)
	cancel()

	if err := got.Err(); err != nil {
		t.Fatalf("lifecycleEventContext fallback err = %v, want nil after request cancellation", err)
	}
}

func TestLifecycleEventContextFallsBackForTypedNilRuntime(t *testing.T) {
	var runtime *fakeLifecycleRuntime
	fallback, cancel := context.WithCancel(context.Background())

	got := lifecycleEventContext(fallback, runtime)
	cancel()

	if err := got.Err(); err != nil {
		t.Fatalf("lifecycleEventContext fallback err = %v, want nil after request cancellation", err)
	}
}

func TestLifecycleEventContextUsesRuntimeBackground(t *testing.T) {
	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	defer runtimeCancel()
	fallback, fallbackCancel := context.WithCancel(context.Background())
	defer fallbackCancel()
	runtime := &fakeLifecycleRuntime{ctx: runtimeCtx}

	got := lifecycleEventContext(fallback, runtime)

	if got != runtimeCtx {
		t.Fatalf("lifecycleEventContext = %v, want runtime background context", got)
	}
}

func TestServiceStartBootstrapsLifecycleForContainerTask(t *testing.T) {
	manager := &fakeLifecycleIOManager{}
	svc := NewService(&fakeLifecycleIOFactory{
		manager: manager,
		stream:  &fakeLifecycleEventStream{},
	})
	taskHandle := &fakeLifecycleTask{
		id:          "task-1",
		status:      task.Status_CREATED,
		stdin:       "/stdin",
		stdout:      "/stdout",
		stderr:      "/stderr",
		stdinCloser: make(chan struct{}),
		exitCh:      make(chan struct{}),
	}
	sandbox := &fakeLifecycleSandbox{}
	runtime := &fakeLifecycleRuntime{
		namespace: "default",
		shimPID:   100,
		ctx:       context.Background(),
		sandbox:   sandbox,
	}

	if err := svc.Start(context.Background(), runtime, taskHandle); err != nil {
		t.Fatalf("Start returned unexpected error: %v", err)
	}

	if sandbox.startContainerID != taskHandle.id {
		t.Fatalf("expected container %s to be started, got %s", taskHandle.id, sandbox.startContainerID)
	}
	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("expected task status RUNNING, got %s", taskHandle.status)
	}
	if taskHandle.stdinPipe == nil {
		t.Fatal("expected stdin pipe to be recorded")
	}
	if taskHandle.ioManager != manager {
		t.Fatal("expected IO manager to be installed")
	}
	if !manager.startCalled {
		t.Fatal("expected IO manager Start to be called")
	}
	if taskHandle.attachInfo == nil {
		t.Fatal("expected attach info to be saved")
	}

	close(taskHandle.exitCh)
}

func TestServiceStartAcceptsNilContextAndNilRuntimeBackground(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeLifecycleTask{
		id:          "task-nil-context",
		status:      task.Status_CREATED,
		stdinCloser: make(chan struct{}),
		exitCh:      make(chan struct{}),
	}
	runtime := &fakeLifecycleRuntime{
		sandbox: &fakeLifecycleSandbox{},
	}

	if err := svc.Start(nil, runtime, taskHandle); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
}

func TestServiceStartWatcherIgnoresRequestCancellation(t *testing.T) {
	manager := &fakeLifecycleIOManager{}
	svc := NewService(&fakeLifecycleIOFactory{
		manager: manager,
		stream:  &fakeLifecycleEventStream{},
	})
	taskHandle := &fakeLifecycleTask{
		id:          "task-start-cancel",
		status:      task.Status_CREATED,
		stdin:       "/stdin",
		stdout:      "/stdout",
		stderr:      "/stderr",
		stdinCloser: make(chan struct{}),
		exitCh:      make(chan struct{}),
	}
	sandbox := &fakeLifecycleSandbox{}
	runtime := &fakeLifecycleRuntime{
		sandbox: sandbox,
	}
	ctx, cancel := context.WithCancel(context.Background())

	if err := svc.Start(ctx, runtime, taskHandle); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	cancel()
	time.Sleep(50 * time.Millisecond)

	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("task status after request cancellation = %s, want running", taskHandle.status)
	}
	if sandbox.stopContainerID != "" {
		t.Fatalf("stopContainerID after request cancellation = %q, want empty", sandbox.stopContainerID)
	}

	close(taskHandle.exitCh)
}

func TestServiceStartFinalizesRunningTaskWhenIOSetupFails(t *testing.T) {
	// A State refresh can legitimately publish RUNNING during Start's
	// unlocked setupIO window (the guest domain is up). When IO setup then
	// fails, the failure path tears the domain down without an exit watcher,
	// so the task must be finalized instead of being stranded RUNNING.
	expectedErr := errors.New("io stream failed")
	svc := NewService(nil)
	taskHandle := &fakeLifecycleTask{
		id:          "task-io-failure-running",
		status:      task.Status_RUNNING,
		stdinCloser: make(chan struct{}),
		exitCh:      make(chan struct{}),
	}
	sandbox := &fakeLifecycleSandbox{
		ioErr: expectedErr,
	}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: sandbox,
	}

	err := svc.Start(context.Background(), runtime, taskHandle)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Start error = %v, want %v", err, expectedErr)
	}
	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("task status = %s, want %s (stranded RUNNING must be finalized)", taskHandle.status, task.Status_STOPPED)
	}
	if taskHandle.exitStatus != exitstatus.Interrupt() {
		t.Fatalf("exit status = %d, want %d (Interrupt)", taskHandle.exitStatus, exitstatus.Interrupt())
	}
	if taskHandle.exitTime.IsZero() {
		t.Fatal("exit time must be recorded when finalizing a stranded RUNNING task")
	}
}

func TestServiceStartDoesNotMarkRunningWhenIOSetupFails(t *testing.T) {
	expectedErr := errors.New("io stream failed")
	svc := NewService(nil)
	taskHandle := &fakeLifecycleTask{
		id:          "task-io-failure",
		status:      task.Status_CREATED,
		stdinCloser: make(chan struct{}),
		exitCh:      make(chan struct{}),
	}
	sandbox := &fakeLifecycleSandbox{
		ioErr: expectedErr,
	}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: sandbox,
	}

	err := svc.Start(context.Background(), runtime, taskHandle)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Start error = %v, want %v", err, expectedErr)
	}
	if taskHandle.status != task.Status_CREATED {
		t.Fatalf("task status = %s, want %s", taskHandle.status, task.Status_CREATED)
	}
	if sandbox.stopContainerID != taskHandle.id {
		t.Fatalf("stopped container = %q, want %q", sandbox.stopContainerID, taskHandle.id)
	}
}

func TestServiceStartDeletesSandboxWhenSandboxIOSetupFails(t *testing.T) {
	expectedErr := errors.New("io stream failed")
	svc := NewService(nil)
	taskHandle := &fakeLifecycleTask{
		id:          "sandbox-io-failure",
		status:      task.Status_CREATED,
		canSandbox:  true,
		stdinCloser: make(chan struct{}),
		exitCh:      make(chan struct{}),
	}
	sandbox := &fakeLifecycleSandbox{
		ioErr: expectedErr,
	}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: sandbox,
	}

	err := svc.Start(context.Background(), runtime, taskHandle)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Start error = %v, want %v", err, expectedErr)
	}
	if !sandbox.stopped {
		t.Fatal("expected sandbox to be stopped after IO setup failure")
	}
	if !sandbox.deleted {
		t.Fatal("expected sandbox to be deleted after IO setup failure")
	}
	if runtime.sandbox != nil {
		t.Fatal("expected runtime sandbox reference to be cleared")
	}
}

func TestServiceStartClearsRecordedStdinWhenAttachStartFails(t *testing.T) {
	expectedErr := errors.New("session start failed")
	manager := &fakeLifecycleIOManager{startErr: expectedErr}
	svc := NewService(&fakeLifecycleIOFactory{
		manager: manager,
		stream:  &fakeLifecycleEventStream{},
	})
	stdin := &lifecycleWriteCloser{}
	stdout := &lifecycleReadCloser{reader: strings.NewReader("")}
	stderr := &lifecycleReadCloser{reader: strings.NewReader("")}
	taskHandle := &fakeLifecycleTask{
		id:          "attach-start-failure",
		status:      task.Status_CREATED,
		stdin:       "/stdin",
		stdout:      "/stdout",
		stderr:      "/stderr",
		stdinCloser: make(chan struct{}),
		exitCh:      make(chan struct{}),
	}
	sandbox := &fakeLifecycleSandbox{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: sandbox,
	}

	err := svc.Start(context.Background(), runtime, taskHandle)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Start error = %v, want %v", err, expectedErr)
	}
	if taskHandle.stdinPipe != nil {
		t.Fatal("expected recorded stdin pipe to be cleared after start failure")
	}
	if !stdin.closed {
		t.Fatal("expected stdin pipe to be closed after start failure")
	}
	if !stdout.closed {
		t.Fatal("expected stdout stream to be closed after start failure")
	}
	if !stderr.closed {
		t.Fatal("expected stderr stream to be closed after start failure")
	}
	if sandbox.stopContainerID != taskHandle.id {
		t.Fatalf("stopped container = %q, want %q", sandbox.stopContainerID, taskHandle.id)
	}
}

func TestSetupIOWithoutAttachPathsSignalsTaskIOExit(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeLifecycleTask{
		id:          "headless-task",
		stdinCloser: make(chan struct{}),
		exitCh:      make(chan struct{}),
		canSandbox:  true,
	}
	runtime := &fakeLifecycleRuntime{}
	sandbox := &fakeLifecycleSandbox{
		stdin:  &lifecycleWriteCloser{},
		stdout: strings.NewReader(""),
		stderr: strings.NewReader(""),
	}

	err := svc.setupIO(&taskContext{
		Context: context.Background(),
		Runtime: runtime,
		Task:    taskHandle,
	}, sandbox)
	if err != nil {
		t.Fatalf("setupIO returned error: %v", err)
	}
	if taskHandle.stdinPipe != nil {
		t.Fatal("stdin pipe must not be recorded on the no-attach path (nothing closes it)")
	}
	if !sandbox.stdin.(*lifecycleWriteCloser).closed {
		t.Fatal("expected no-attach stdin stream to be closed")
	}
	if !taskHandle.ioExit {
		t.Fatal("expected IOExit to be signaled")
	}
	select {
	case <-taskHandle.stdinCloser:
	default:
		t.Fatal("expected stdinCloser to be closed")
	}
	select {
	case <-taskHandle.exitCh:
	default:
		t.Fatal("expected exit channel to be closed")
	}
}

func TestSetupIOWithoutAttachPathsKeepsCriPodContainerRunning(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeLifecycleTask{
		id:          "headless-pod-container",
		stdinCloser: make(chan struct{}),
		exitCh:      make(chan struct{}),
		canSandbox:  false,
	}
	runtime := &fakeLifecycleRuntime{}
	sandbox := &fakeLifecycleSandbox{
		stdin:  &lifecycleWriteCloser{},
		stdout: strings.NewReader(""),
		stderr: strings.NewReader(""),
	}

	err := svc.setupIO(&taskContext{
		Context: context.Background(),
		Runtime: runtime,
		Task:    taskHandle,
	}, sandbox)
	if err != nil {
		t.Fatalf("setupIO returned error: %v", err)
	}
	if taskHandle.stdinPipe != nil {
		t.Fatal("stdin pipe must not be recorded on the no-attach path (nothing closes it)")
	}
	if !sandbox.stdin.(*lifecycleWriteCloser).closed {
		t.Fatal("expected no-attach stdin stream to be closed")
	}
	if taskHandle.ioExit {
		t.Fatal("pod container without initial attach paths should stay attachable")
	}
	select {
	case <-taskHandle.stdinCloser:
	default:
		t.Fatal("expected stdinCloser to be closed")
	}
	select {
	case <-taskHandle.exitCh:
		t.Fatal("pod container exit channel should remain open")
	default:
	}
}

func TestSignalTaskIOExitAcceptsNilTaskHandle(t *testing.T) {
	var typedNilTask *fakeLifecycleTask
	signalTaskIOExit(typedNilTask)
}

func TestSignalTaskIOExitSignalsValidTask(t *testing.T) {
	exitCh := make(chan struct{})
	taskHandle := &fakeLifecycleTask{
		exitCh: exitCh,
	}

	signalTaskIOExit(taskHandle)

	if !taskHandle.ioExit {
		t.Fatal("expected IOExit to be signaled")
	}
	select {
	case <-exitCh:
	default:
		t.Fatal("expected exit channel to be closed")
	}
}

func TestWaitForExitCancelSignalsExitWhenTaskIOExitIsNoop(t *testing.T) {
	svc := NewService(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	taskHandle := &fakeLifecycleTask{
		id:     "wait-cancel-noop-ioexit",
		status: task.Status_RUNNING,
		exitCh: make(chan struct{}),
	}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: &fakeLifecycleSandbox{},
	}

	done := make(chan int32, 1)
	go func() {
		done <- svc.waitForExit(&taskContext{
			Context: ctx,
			Runtime: runtime,
			Task:    taskHandle,
		})
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("waitForExit blocked after cancellation")
	}
	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("expected task status %s, got %s", task.Status_STOPPED, taskHandle.status)
	}
}

func TestWaitForExitCancelCleansSandboxTask(t *testing.T) {
	svc := NewService(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	taskHandle := &fakeLifecycleTask{
		id:         "wait-cancel-sandbox",
		status:     task.Status_RUNNING,
		exitCh:     make(chan struct{}),
		canSandbox: true,
	}
	sandbox := &fakeLifecycleSandbox{}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: sandbox,
	}

	done := make(chan int32, 1)
	go func() {
		done <- svc.waitForExit(&taskContext{
			Context: ctx,
			Runtime: runtime,
			Task:    taskHandle,
		})
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("waitForExit blocked after sandbox cancellation")
	}
	if !sandbox.stopped || !sandbox.deleted {
		t.Fatal("expected sandbox to be stopped and deleted")
	}
	if runtime.sandbox != nil {
		t.Fatal("expected runtime sandbox to be cleared")
	}
	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("expected task status %s, got %s", task.Status_STOPPED, taskHandle.status)
	}
}

func TestWaitForExitReportsRecordedExitInfo(t *testing.T) {
	svc := NewService(nil)
	recordedExit := time.Now().Add(-time.Minute)
	exitCh := make(chan struct{})
	close(exitCh)
	taskHandle := &fakeLifecycleTask{
		id:         "wait-recorded-exit",
		status:     task.Status_RUNNING,
		exitCh:     exitCh,
		exitStatus: 7,
		exitTime:   recordedExit,
	}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: &fakeLifecycleSandbox{},
	}

	status := svc.waitForExit(&taskContext{
		Context: context.Background(),
		Runtime: runtime,
		Task:    taskHandle,
	})

	if status != 7 {
		t.Fatalf("waitForExit status = %d, want 7", status)
	}
	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("task status = %s, want stopped", taskHandle.status)
	}
	if taskHandle.exitStatus != 7 {
		t.Fatalf("recorded task exit status = %d, want 7", taskHandle.exitStatus)
	}
	if !taskHandle.exitTime.Equal(recordedExit) {
		t.Fatalf("recorded task exit time = %v, want %v", taskHandle.exitTime, recordedExit)
	}
	if runtime.reportedTask != taskHandle || runtime.reportedStatus != 7 || !runtime.reportedAt.Equal(recordedExit) {
		t.Fatalf("reported exit = (%v, %d, %v), want task/status/time", runtime.reportedTask, runtime.reportedStatus, runtime.reportedAt)
	}
}

func TestWaitForExitStopsTaskIOSession(t *testing.T) {
	svc := NewService(nil)
	exitCh := make(chan struct{})
	close(exitCh)
	ioManager := &fakeLifecycleIOManager{}
	recordedExit := time.Now()
	taskHandle := &fakeLifecycleTask{
		id:         "wait-stop-io",
		status:     task.Status_RUNNING,
		exitCh:     exitCh,
		ioManager:  ioManager,
		exitStatus: 5,
		exitTime:   recordedExit,
	}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: &fakeLifecycleSandbox{},
	}

	svc.waitForExit(&taskContext{
		Context: context.Background(),
		Runtime: runtime,
		Task:    taskHandle,
	})

	// A terminal task must have its IO session torn down so containerd-side
	// FIFO copies reach EOF; without it task.Delete blocks in
	// ContainerIO.Wait and CRI never finishes the container teardown.
	if !ioManager.stopCalled {
		t.Fatal("expected task IO session to be stopped after exit")
	}
	if runtime.reportedTask != taskHandle {
		t.Fatal("expected exit to be reported after the IO session stop")
	}
}

func TestWaitForExitUsesInjectedClockWhenExitTimeMissing(t *testing.T) {
	svc := NewService(nil)
	now := time.Date(2026, 4, 27, 7, 8, 9, 0, time.UTC)
	svc.now = func() time.Time { return now }
	exitCh := make(chan struct{})
	close(exitCh)
	taskHandle := &fakeLifecycleTask{
		id:         "wait-clock-exit",
		status:     task.Status_RUNNING,
		exitCh:     exitCh,
		exitStatus: 3,
	}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: &fakeLifecycleSandbox{},
	}

	status := svc.waitForExit(&taskContext{
		Context: context.Background(),
		Runtime: runtime,
		Task:    taskHandle,
	})

	if status != 3 {
		t.Fatalf("waitForExit status = %d, want 3", status)
	}
	if !taskHandle.exitTime.Equal(now) {
		t.Fatalf("task exit time = %v, want %v", taskHandle.exitTime, now)
	}
	if !runtime.reportedAt.Equal(now) {
		t.Fatalf("reported exit time = %v, want %v", runtime.reportedAt, now)
	}
}

func TestCompleteExitedTaskPreservesKillPrewrite(t *testing.T) {
	svc := NewService(nil)
	now := time.Date(2026, 4, 27, 7, 8, 9, 0, time.UTC)
	svc.now = func() time.Time { return now }
	prewriteAt := now.Add(-time.Second)
	exitCh := make(chan struct{})
	close(exitCh)
	// Kill pre-write: ExitInfo set, Status still RUNNING (not yet STOPPED).
	taskHandle := &fakeLifecycleTask{
		id:         "wait-preserve-kill-prewrite",
		status:     task.Status_RUNNING,
		exitCh:     exitCh,
		exitStatus: 137,
		exitTime:   prewriteAt,
	}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: &fakeLifecycleSandbox{},
	}

	status := svc.waitForExit(&taskContext{
		Context: context.Background(),
		Runtime: runtime,
		Task:    taskHandle,
	})

	if status != 137 {
		t.Fatalf("waitForExit status = %d, want 137 (kill pre-write)", status)
	}
	if taskHandle.exitStatus != 137 {
		t.Fatalf("task exit status = %d, want 137", taskHandle.exitStatus)
	}
	if !taskHandle.exitTime.Equal(prewriteAt) {
		t.Fatalf("task exit time = %v, want prewrite %v", taskHandle.exitTime, prewriteAt)
	}
	if runtime.reportedStatus != 137 {
		t.Fatalf("reported exit status = %d, want 137", runtime.reportedStatus)
	}
}

func TestNewServiceAcceptsClockOption(t *testing.T) {
	now := time.Date(2026, 4, 27, 8, 9, 10, 0, time.UTC)
	attach, err := attachapp.NewServiceChecked(nil, attachapp.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("attach service setup failed: %v", err)
	}
	svc, err := NewServiceChecked(attach, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewServiceChecked returned error: %v", err)
	}

	if got := svc.clockNow(); !got.Equal(now) {
		t.Fatalf("clockNow() = %v, want %v", got, now)
	}
}

func TestNewServiceUsesInjectedAttachService(t *testing.T) {
	attach, err := attachapp.NewServiceChecked(nil)
	if err != nil {
		t.Fatalf("attach service setup failed: %v", err)
	}
	svc, err := NewServiceChecked(attach)
	if err != nil {
		t.Fatalf("NewServiceChecked returned error: %v", err)
	}

	if svc.attach != attach {
		t.Fatal("NewService did not use injected attach service")
	}
}

func TestNewServiceCheckedRejectsNilAttachServiceInjection(t *testing.T) {
	svc, err := NewServiceChecked(nil)
	if err == nil {
		t.Fatal("expected nil attach service injection to fail")
	}
	if !errors.Is(err, ErrAttachServiceRequired) {
		t.Fatalf("NewServiceChecked error = %v, want incomplete attach service sentinel", err)
	}
	if svc != nil {
		t.Fatal("expected no service when attach service injection is incomplete")
	}
}

func TestWaitForExitRejectsMissingExitSignal(t *testing.T) {
	attach, err := attachapp.NewServiceChecked(nil)
	if err != nil {
		t.Fatalf("attach service setup failed: %v", err)
	}
	svc, err := NewServiceChecked(attach)
	if err != nil {
		t.Fatalf("NewServiceChecked returned error: %v", err)
	}
	taskHandle := &fakeLifecycleTask{
		id:     "wait-missing-exit-signal",
		status: task.Status_RUNNING,
	}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: &fakeLifecycleSandbox{},
	}

	status := svc.waitForExit(&taskContext{
		Context: context.Background(),
		Runtime: runtime,
		Task:    taskHandle,
	})
	if status != 0 {
		t.Fatalf("expected zero status for missing exit signal, got %d", status)
	}
	if taskHandle.status == task.Status_STOPPED {
		t.Fatal("task should not be marked stopped when wait did not observe an exit")
	}
}

func TestWaitForExitAutoCloseStopsTaskAndReportsExit(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	svc := NewService(nil, WithClock(func() time.Time { return now }))
	sandbox := &fakeLifecycleSandbox{}
	taskHandle := &fakeLifecycleTask{
		id:         "wait-auto-close-stop",
		status:     task.Status_RUNNING,
		exitCh:     make(chan struct{}),
		attachInfo: &ports.AttachInfo{Terminal: true},
		canSandbox: true,
		annotations: map[string]string{
			ann.AutoCloseTimeout: "1ns",
		},
	}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: sandbox,
	}

	status := svc.waitForExit(&taskContext{
		Context: context.Background(),
		Runtime: runtime,
		Task:    taskHandle,
	})

	if status != int32(exitstatus.Interrupt()) {
		t.Fatalf("wait status = %d, want %d (interrupt)", status, exitstatus.Interrupt())
	}
	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("task status = %v, want STOPPED", taskHandle.status)
	}
	if !taskHandle.ioExit {
		t.Fatal("expected auto-close to signal IO exit")
	}
	if !sandbox.stopped {
		t.Fatal("expected auto-close to stop sandbox task")
	}
	if !sandbox.deleted {
		t.Fatal("expected auto-close to delete single-container sandbox")
	}
	if runtime.sandbox != nil {
		t.Fatal("expected auto-close to clear single-container sandbox")
	}
	wantStatus := int(exitstatus.Interrupt())
	if runtime.reportedTask != taskHandle || runtime.reportedStatus != wantStatus || !runtime.reportedAt.Equal(now) {
		t.Fatalf("reported exit = task:%v status:%d at:%v, want task:%v status:%d at:%v",
			runtime.reportedTask, runtime.reportedStatus, runtime.reportedAt, taskHandle, wantStatus, now)
	}
	select {
	case <-taskHandle.exitCh:
	default:
		t.Fatal("expected auto-close to close task exit channel")
	}
}

func TestWaitForExitAutoCloseStopsPodContainerWithoutDeletingSandbox(t *testing.T) {
	svc := NewService(nil)
	sandbox := &fakeLifecycleSandbox{}
	taskHandle := &fakeLifecycleTask{
		id:     "wait-auto-close-pod-container",
		status: task.Status_RUNNING,
		exitCh: make(chan struct{}),
		annotations: map[string]string{
			ann.AutoCloseTimeout: "1ns",
		},
	}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: sandbox,
	}

	status := svc.waitForExit(&taskContext{
		Context: context.Background(),
		Runtime: runtime,
		Task:    taskHandle,
	})

	if status != int32(exitstatus.Interrupt()) {
		t.Fatalf("wait status = %d, want %d (interrupt)", status, exitstatus.Interrupt())
	}
	if sandbox.stopContainerID != taskHandle.id {
		t.Fatalf("stopped container = %q, want %q", sandbox.stopContainerID, taskHandle.id)
	}
	if sandbox.deleted {
		t.Fatal("pod container auto-close must not delete the sandbox")
	}
	if runtime.sandbox == nil {
		t.Fatal("pod container auto-close must keep the sandbox")
	}
	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("task status = %v, want STOPPED", taskHandle.status)
	}
	if !taskHandle.ioExit {
		t.Fatal("expected auto-close to signal IO exit")
	}
}

func TestCompleteTaskWithoutAttachSkipsRecoveredTask(t *testing.T) {
	recoveredTask := &fakeLifecycleTask{
		id:          "recovered-single",
		canSandbox:  true,
		recovered:   true,
		exitCh:      make(chan struct{}),
		stdinCloser: make(chan struct{}),
	}
	completeTaskWithoutAttach(recoveredTask)
	if recoveredTask.ioExit {
		t.Fatal("recovered task must not be force-completed: its domain was just started")
	}

	freshTask := &fakeLifecycleTask{
		id:          "fresh-single",
		canSandbox:  true,
		exitCh:      make(chan struct{}),
		stdinCloser: make(chan struct{}),
	}
	completeTaskWithoutAttach(freshTask)
	if !freshTask.ioExit {
		t.Fatal("fresh no-attach single container should be completed immediately")
	}
}

type notFoundExitSandbox struct {
	fakeLifecycleSandbox
	calls int32
}

func (f *notFoundExitSandbox) WaitContainerExit(ctx context.Context, containerID string) (int32, error) {
	atomic.AddInt32(&f.calls, 1)
	return 0, er.ContainerNotFound
}

func TestGuestExitSignalReportsExitOnContainerNotFound(t *testing.T) {
	sandbox := &notFoundExitSandbox{}
	runtime := &fakeLifecycleRuntime{sandbox: sandbox}
	taskHandle := &fakeLifecycleTask{id: "deleted-container", exitCh: make(chan struct{})}
	tc := newTaskContext(context.Background(), runtime, taskHandle)

	ch := guestExitSignal(tc)

	// The container is gone, which semantically IS an exit: the channel must
	// close so waitForExitSignal reports it — a silent return leaves the task
	// RUNNING and Wait hanging when the removal came from a sandbox-level Kill.
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("guest exit channel must close for a deleted container (removal IS an exit)")
	}
	if got := atomic.LoadInt32(&sandbox.calls); got != 1 {
		t.Fatalf("WaitContainerExit calls = %d, want 1 (no retry spin for a deleted container)", got)
	}
}

// TestMarkTaskRunningTransitionsFromCreated verifies the normal CREATED→RUNNING path.
func TestMarkTaskRunningTransitionsFromCreated(t *testing.T) {
	runtime := &fakeLifecycleRuntime{ctx: context.Background()}
	taskHandle := &fakeLifecycleTask{
		id:     "created-task",
		status: task.Status_CREATED,
		exitCh: make(chan struct{}),
	}

	if !markTaskRunning(runtime, taskHandle) {
		t.Fatal("markTaskRunning returned false for CREATED, want true")
	}
	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("status = %s, want %s", taskHandle.status, task.Status_RUNNING)
	}
}

// TestMarkTaskRunningAcceptsAlreadyRunning verifies that a task already set
// RUNNING by a concurrent State-RPC refresh (which legitimately observed the
// just-started domain) is treated as success — not as a Kill/Delete that the
// guard rejects. Without this, Start would tear down a working domain.
func TestMarkTaskRunningAcceptsAlreadyRunning(t *testing.T) {
	runtime := &fakeLifecycleRuntime{ctx: context.Background()}
	taskHandle := &fakeLifecycleTask{
		id:     "refreshed-task",
		status: task.Status_RUNNING, // set by refresh during Start's setupIO window
		exitCh: make(chan struct{}),
	}

	if !markTaskRunning(runtime, taskHandle) {
		t.Fatal("markTaskRunning returned false for RUNNING, want true (idempotent)")
	}
	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("status = %s, want %s (unchanged)", taskHandle.status, task.Status_RUNNING)
	}
}

// TestMarkTaskRunningRejectsTerminalStates verifies Kill/Delete (STOPPED) and
// Pause (PAUSED) during the start window still reject the RUNNING transition.
func TestMarkTaskRunningRejectsTerminalStates(t *testing.T) {
	for _, status := range []task.Status{task.Status_STOPPED, task.Status_PAUSED, task.Status_PAUSING} {
		runtime := &fakeLifecycleRuntime{ctx: context.Background()}
		taskHandle := &fakeLifecycleTask{
			id:     "terminal-task",
			status: status,
			exitCh: make(chan struct{}),
		}
		if markTaskRunning(runtime, taskHandle) {
			t.Fatalf("markTaskRunning returned true for %s, want false", status)
		}
		if taskHandle.status != status {
			t.Fatalf("status = %s, want %s (must not overwrite concurrent result)", taskHandle.status, status)
		}
	}
}

// --- Regression: exit watcher must tear down the guest (scan §5 item) ---
//
// The scan record's checklist requires: "exit watcher: 未 Destroy 不得终态化成功".
// completeExitedTask calls stopTaskAfterExit → stopLifecycleTask → sandbox
// Stop/Delete (or StopContainer for pod containers). If the sandbox teardown
// is skipped, the guest domain is orphaned while containerd believes the
// task is done. This test verifies the teardown is invoked for both the
// sandbox task path and the pod-container path.

func TestCompleteExitedTaskTearsDownGuestBeforeReturn(t *testing.T) {
	svc := NewService(nil)

	// Pod container path: StopContainer must be called.
	sandbox := &fakeLifecycleSandbox{}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: sandbox,
	}
	taskHandle := &fakeLifecycleTask{
		id:         "exit-teardown-pod",
		status:     task.Status_RUNNING,
		exitCh:     make(chan struct{}),
		canSandbox: false, // pod container, not sandbox
	}

	code := svc.completeExitedTask(&taskContext{
		Context: context.Background(),
		Runtime: runtime,
		Task:    taskHandle,
	}, exitWaitGuestExit)

	if code == 0 {
		t.Log("note: guest-exit fabricated non-zero status is expected")
	}
	if sandbox.stopContainerID == "" {
		t.Fatal("exit watcher did not call StopContainer for pod container: guest domain would be orphaned")
	}
}

func TestCompleteExitedTaskSandboxTaskStopsAndDeletesSandbox(t *testing.T) {
	svc := NewService(nil)

	sandbox := &fakeLifecycleSandbox{}
	runtime := &fakeLifecycleRuntime{
		ctx:     context.Background(),
		sandbox: sandbox,
	}
	taskHandle := &fakeLifecycleTask{
		id:         "exit-teardown-sandbox",
		status:     task.Status_RUNNING,
		exitCh:     make(chan struct{}),
		canSandbox: true, // sandbox-level task
	}

	_ = svc.completeExitedTask(&taskContext{
		Context: context.Background(),
		Runtime: runtime,
		Task:    taskHandle,
	}, exitWaitGuestExit)

	if !sandbox.stopped {
		t.Fatal("exit watcher did not stop sandbox: lingering Xen domain")
	}
	// Delete is containerd's follow-up RPC after TaskExit; the exit watcher
	// must destroy the guest (Stop) but not pre-empt the Delete lifecycle.
	if sandbox.deleted {
		t.Log("note: sandbox already deleted (acceptable but not required)")
	}
}

// attachableFakeTask extends fakeLifecycleTask with a flippable attach
// state and transition notifications, mirroring the shimContainer capability
// the auto-close watcher consumes.
type attachableFakeTask struct {
	fakeLifecycleTask
	attachedNow atomic.Bool
	changes     chan struct{}
}

func (a *attachableFakeTask) IsAttached() bool               { return a.attachedNow.Load() }
func (a *attachableFakeTask) AttachChanges() <-chan struct{} { return a.changes }
func (a *attachableFakeTask) detach() {
	if a.attachedNow.Swap(false) {
		select {
		case a.changes <- struct{}{}:
		default:
		}
	}
}

// TestWaitForExitAutoCloseGraceStartsAtDetach pins the documented semantics
// "close N seconds after the IO session disconnects": while attached the
// grace timer is suspended, and a detach starts (not merely samples) the
// countdown. The old expiry-time sampling derived the grace from cycle
// phase — a long attach ending just before expiry gave the client ~0s.
func TestWaitForExitAutoCloseGraceStartsAtDetach(t *testing.T) {
	svc := NewService(nil)
	base := &fakeLifecycleTask{
		id:         "auto-close-detach",
		status:     task.Status_RUNNING,
		exitCh:     make(chan struct{}),
		attachInfo: &ports.AttachInfo{Terminal: true},
		canSandbox: true,
		annotations: map[string]string{
			ann.AutoCloseTimeout: "60ms",
		},
	}
	// attached at watcher start: grace must be suspended
	taskHandle := &attachableFakeTask{fakeLifecycleTask: *base, changes: make(chan struct{}, 1)}
	taskHandle.attachedNow.Store(true)
	runtime := &fakeLifecycleRuntime{ctx: context.Background(), sandbox: &fakeLifecycleSandbox{}}

	done := make(chan int32, 1)
	go func() {
		done <- svc.waitForExit(&taskContext{Context: context.Background(), Runtime: runtime, Task: taskHandle})
	}()

	// Attached for 3.5 timer periods: the old sampling implementation had
	// reset the timer at least twice; detach now, ~5ms before the next
	// expiry would fall under the old semantics.
	time.Sleep(215 * time.Millisecond)
	taskHandle.detach()

	// 25ms after detach we must still be alive: the grace (60ms) restarted
	// at the detach moment — the old phase-dependent code would already have
	// killed the task here.
	time.Sleep(25 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("auto-close fired within the grace window: detach must restart the countdown")
	default:
	}

	// After the full grace the task must be stopped.
	select {
	case <-time.After(2 * time.Second):
		t.Fatal("auto-close did not fire after the grace window elapsed")
	case <-done:
	}
	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("task status = %v, want STOPPED", taskHandle.status)
	}
}
