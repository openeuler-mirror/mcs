package attach

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"micrun/internal/application/exitstatus"
	"micrun/internal/ports"
	er "micrun/internal/support/errors"

	"github.com/containerd/containerd/api/types/task"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type fakeIOManager struct {
	startCalled              bool
	stopCalled               bool
	stopWithoutClosingCalled bool
	restartCalled            bool
	restartWithTTYsCalled    bool
	isRunning                bool
	startErr                 error
	restartWithTTYsErr       error
	restartTTYIn             io.WriteCloser
	restartTTYOut            io.Reader
	onStart                  func()
	eventStream              ports.IOEventStream
	stopCh                   chan struct{}
}

func (f *fakeIOManager) Start() error {
	f.startCalled = true
	if f.onStart != nil {
		f.onStart()
	}
	return f.startErr
}
func (f *fakeIOManager) Stop() {
	f.stopCalled = true
	if f.stopCh != nil {
		select {
		case <-f.stopCh:
		default:
			close(f.stopCh)
		}
	}
}
func (f *fakeIOManager) StopWithoutClosingFIFOs() { f.stopWithoutClosingCalled = true }
func (f *fakeIOManager) Restart() error {
	f.restartCalled = true
	return nil
}
func (f *fakeIOManager) RestartWithTTYs(ttyIn io.WriteCloser, ttyOut io.Reader) error {
	f.restartWithTTYsCalled = true
	f.restartTTYIn = ttyIn
	f.restartTTYOut = ttyOut
	return f.restartWithTTYsErr
}
func (f *fakeIOManager) IsRunning() bool { return f.isRunning }
func (f *fakeIOManager) EventStream() ports.IOEventStream {
	if f.eventStream != nil {
		return f.eventStream
	}
	return closedEventStream{}
}

type lockProbingIOManager struct {
	fakeIOManager
	runtime *fakeRuntime
	t       *testing.T
}

func (m *lockProbingIOManager) Stop() {
	m.assertRuntimeLockAvailable("Stop")
	m.fakeIOManager.Stop()
}

func (m *lockProbingIOManager) StopWithoutClosingFIFOs() {
	m.assertRuntimeLockAvailable("StopWithoutClosingFIFOs")
	m.fakeIOManager.StopWithoutClosingFIFOs()
}

func (m *lockProbingIOManager) assertRuntimeLockAvailable(operation string) {
	m.t.Helper()
	acquired := make(chan struct{})
	go func() {
		m.runtime.Lock()
		m.runtime.Unlock()
		close(acquired)
	}()
	select {
	case <-acquired:
	case <-time.After(200 * time.Millisecond):
		m.t.Fatalf("runtime lock was held while IO manager %s executed", operation)
	}
}

type fakeRuntime struct {
	mu      sync.Mutex
	shimPID uint32
	sandbox ports.Sandbox
}

func (f *fakeRuntime) Lock()                              { f.mu.Lock() }
func (f *fakeRuntime) Unlock()                            { f.mu.Unlock() }
func (f *fakeRuntime) Namespace() string                  { return "default" }
func (f *fakeRuntime) BackgroundContext() context.Context { return context.Background() }
func (f *fakeRuntime) ShimPID() uint32                    { return f.shimPID }
func (f *fakeRuntime) Sandbox() ports.Sandbox             { return f.sandbox }
func (f *fakeRuntime) SetSandbox(sandbox ports.Sandbox)   { f.sandbox = sandbox }

type nilBackgroundRuntime struct {
	fakeRuntime
}

func (f *nilBackgroundRuntime) BackgroundContext() context.Context { return nil }

type fakeTask struct {
	id         string
	status     task.Status
	terminal   bool
	stdin      string
	stdout     string
	stderr     string
	exitStatus uint32
	exitTime   time.Time
	stdinPipe  io.WriteCloser
	stdinClose chan struct{}
	exitCh     chan struct{}
	ioMgr      ports.IOManager
	attachInfo *ports.AttachInfo
	attached   bool
	ioExit     bool
}

func (f *fakeTask) ID() string              { return f.id }
func (f *fakeTask) Bundle() string          { return "" }
func (f *fakeTask) PID() uint32             { return 0 }
func (f *fakeTask) Status() task.Status     { return f.status }
func (f *fakeTask) SetStatus(s task.Status) { f.status = s }
func (f *fakeTask) Terminal() bool          { return f.terminal }
func (f *fakeTask) StdinPath() string       { return f.stdin }
func (f *fakeTask) StdoutPath() string      { return f.stdout }
func (f *fakeTask) StderrPath() string      { return f.stderr }
func (f *fakeTask) ExitStatus() uint32      { return f.exitStatus }
func (f *fakeTask) ExitTime() time.Time     { return f.exitTime }
func (f *fakeTask) SetExitInfo(status uint32, exitedAt time.Time) {
	f.exitStatus, f.exitTime = status, exitedAt
}
func (f *fakeTask) StdinPipe() io.WriteCloser            { return f.stdinPipe }
func (f *fakeTask) StdinCloser() chan struct{}           { return f.stdinClose }
func (f *fakeTask) ExitChan() chan struct{}              { return f.exitCh }
func (f *fakeTask) IOExit()                              { f.ioExit = true }
func (f *fakeTask) CanBeSandbox() bool                   { return false }
func (f *fakeTask) IsCriSandbox() bool                   { return false }
func (f *fakeTask) Annotations() map[string]string       { return nil }
func (f *fakeTask) IOManager() ports.IOManager           { return f.ioMgr }
func (f *fakeTask) SetIOManager(m ports.IOManager)       { f.ioMgr = m }
func (f *fakeTask) AttachInfo() *ports.AttachInfo        { return f.attachInfo }
func (f *fakeTask) SetAttachInfo(info *ports.AttachInfo) { f.attachInfo = info }
func (f *fakeTask) SetStdinPipe(pipe io.WriteCloser)     { f.stdinPipe = pipe }
func (f *fakeTask) SetAttached(attached bool) (previous bool) {
	previous = f.attached
	f.attached = attached
	return previous
}

type attachTrackingWriteCloser struct {
	closed chan struct{}
}

func (w *attachTrackingWriteCloser) Write(p []byte) (int, error) {
	return len(p), nil
}

func (w *attachTrackingWriteCloser) Close() error {
	select {
	case <-w.closed:
	default:
		close(w.closed)
	}
	return nil
}

func TestServiceEntryPointsRequireInputs(t *testing.T) {
	svc := NewService(nil)
	ctx := context.Background()
	taskHandle := &fakeTask{id: "task-attach"}
	runtime := &fakeRuntime{}
	var typedNilRuntime *fakeRuntime
	var typedNilTask *fakeTask

	cases := []struct {
		name string
		call func() error
	}{
		{name: "CloseIO task", call: func() error {
			return svc.CloseIO(ctx, nil, false)
		}},
		{name: "StartInitialSession runtime", call: func() error {
			return svc.StartInitialSession(ctx, nil, taskHandle, nil, nil, nil)
		}},
		{name: "StartInitialSession task", call: func() error {
			return svc.StartInitialSession(ctx, runtime, nil, nil, nil, nil)
		}},
		{name: "StartInitialSession typed nil runtime", call: func() error {
			return svc.StartInitialSession(ctx, typedNilRuntime, taskHandle, nil, nil, nil)
		}},
		{name: "StartInitialSession typed nil task", call: func() error {
			return svc.StartInitialSession(ctx, runtime, typedNilTask, nil, nil, nil)
		}},
		{name: "EnsureAttach runtime", call: func() error {
			return svc.EnsureAttach(nil, taskHandle)
		}},
		{name: "EnsureAttach task", call: func() error {
			return svc.EnsureAttach(runtime, nil)
		}},
		{name: "EnsureAttach typed nil runtime", call: func() error {
			return svc.EnsureAttach(typedNilRuntime, taskHandle)
		}},
		{name: "EnsureAttach typed nil task", call: func() error {
			return svc.EnsureAttach(runtime, typedNilTask)
		}},
		{name: "PrepareResize runtime", call: func() error {
			return svc.PrepareResize(ctx, nil, taskHandle, 24, 80)
		}},
		{name: "PrepareResize task", call: func() error {
			return svc.PrepareResize(ctx, runtime, nil, 24, 80)
		}},
		{name: "PrepareResize typed nil runtime", call: func() error {
			return svc.PrepareResize(ctx, typedNilRuntime, taskHandle, 24, 80)
		}},
		{name: "PrepareResize typed nil task", call: func() error {
			return svc.PrepareResize(ctx, runtime, typedNilTask, 24, 80)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatalf("%s should reject missing input", tc.name)
			}
		})
	}
}

type fakeEventStream struct {
	subscribeCount int
	events         chan ports.IOEvent
}

func (f *fakeEventStream) SubscribeMany(eventTypes ...ports.IOEventType) ports.IOEventSubscriber {
	f.subscribeCount += len(eventTypes)
	if f.events != nil {
		return f.events
	}
	ch := make(chan ports.IOEvent)
	return ch
}

type closedEventStream struct{}

func (closedEventStream) SubscribeMany(eventTypes ...ports.IOEventType) ports.IOEventSubscriber {
	ch := make(chan ports.IOEvent)
	close(ch)
	return ch
}

type nilSubscriberEventStream struct{}

func (nilSubscriberEventStream) SubscribeMany(eventTypes ...ports.IOEventType) ports.IOEventSubscriber {
	return nil
}

type fakeIOFactory struct {
	manager    ports.IOManager
	stream     ports.IOEventStream
	lastConfig ports.IOSessionConfig
	lastCtx    context.Context
}

func (f *fakeIOFactory) NewSession(ctx context.Context, config ports.IOSessionConfig) (ports.IOManager, ports.IOEventStream, error) {
	f.lastConfig = config
	f.lastCtx = ctx
	return f.manager, f.stream, nil
}

func (f *fakeIOFactory) IsValidFIFOPath(path string) bool { return path != "" }

func (f *fakeIOFactory) GenerateFIFOPath(namespace, containerID, stream string) string {
	return "/generated/" + namespace + "/" + containerID + "/" + stream
}

type strictFIFOFactory struct {
	fakeIOFactory
}

func (f *strictFIFOFactory) IsValidFIFOPath(path string) bool {
	return len(path) >= len("/valid/") && path[:len("/valid/")] == "/valid/"
}

func TestStartManagedSessionUsesBackgroundWhenRuntimeContextIsNil(t *testing.T) {
	factory := &fakeIOFactory{
		manager: &fakeIOManager{},
		stream:  closedEventStream{},
	}
	service := NewService(factory)
	runtime := &nilBackgroundRuntime{}
	taskHandle := &fakeTask{id: "task1", exitCh: make(chan struct{})}

	err := service.startManagedSession(nil, runtime, taskHandle, ports.IOSessionConfig{ContainerID: "task1"}, &ports.AttachInfo{})
	if err != nil {
		t.Fatalf("startManagedSession returned error: %v", err)
	}
	if factory.lastCtx == nil {
		t.Fatal("expected factory to receive a non-nil context")
	}
	select {
	case <-factory.lastCtx.Done():
		t.Fatal("fallback session context should not be canceled")
	default:
	}
}

func TestStartManagedSessionDetachesFallbackContextFromRequestCancellation(t *testing.T) {
	factory := &fakeIOFactory{
		manager: &fakeIOManager{},
		stream:  closedEventStream{},
	}
	service := NewService(factory)
	runtime := &nilBackgroundRuntime{}
	taskHandle := &fakeTask{id: "task-detached-context", exitCh: make(chan struct{})}
	requestCtx, cancel := context.WithCancel(context.Background())

	err := service.startManagedSession(requestCtx, runtime, taskHandle, ports.IOSessionConfig{ContainerID: taskHandle.id}, &ports.AttachInfo{})
	if err != nil {
		t.Fatalf("startManagedSession returned error: %v", err)
	}
	cancel()

	select {
	case <-factory.lastCtx.Done():
		t.Fatal("session context should outlive request cancellation")
	default:
	}
}

func TestStartManagedSessionRejectsAlreadyCanceledRequest(t *testing.T) {
	factory := &fakeIOFactory{
		manager: &fakeIOManager{},
		stream:  closedEventStream{},
	}
	service := NewService(factory)
	runtime := &nilBackgroundRuntime{}
	taskHandle := &fakeTask{id: "task-canceled-request", exitCh: make(chan struct{})}
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err := service.startManagedSession(requestCtx, runtime, taskHandle, ports.IOSessionConfig{ContainerID: taskHandle.id}, &ports.AttachInfo{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("startManagedSession error = %v, want context.Canceled", err)
	}
	if factory.lastCtx != nil {
		t.Fatal("factory should not be called for an already canceled request")
	}
}

func TestStartManagedSessionPrefersRuntimeBackgroundContext(t *testing.T) {
	factory := &fakeIOFactory{
		manager: &fakeIOManager{},
		stream:  closedEventStream{},
	}
	service := NewService(factory)
	runtime := &fakeRuntime{}
	taskHandle := &fakeTask{id: "task-runtime-context", exitCh: make(chan struct{})}
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err := service.startManagedSession(requestCtx, runtime, taskHandle, ports.IOSessionConfig{ContainerID: taskHandle.id}, &ports.AttachInfo{})
	if err != nil {
		t.Fatalf("startManagedSession returned error: %v", err)
	}
	select {
	case <-factory.lastCtx.Done():
		t.Fatal("session context should use runtime background, not canceled request context")
	default:
	}
}

func TestHandleIOEventsAcceptsNilContext(t *testing.T) {
	events := make(chan ports.IOEvent)
	close(events)
	svc := NewService(nil)

	svc.handleIOEvents(nil, &fakeRuntime{}, &fakeTask{id: "nil-context-events"}, events)
}

func TestHandleIOEventsRejectsNilSubscriber(t *testing.T) {
	svc := NewService(nil)
	svc.handleIOEvents(context.Background(), &fakeRuntime{}, &fakeTask{id: "nil-subscriber"}, nil)
}

func TestIOEventPumpStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	events := make(chan ports.IOEvent)

	if _, ok := newIOEventPump(ctx, events).next(); ok {
		t.Fatal("expected canceled event pump to stop")
	}
}

func TestIOEventPumpReadsEvent(t *testing.T) {
	events := make(chan ports.IOEvent, 1)
	want := ports.IOEvent{Type: ports.IOEventDetach, ContainerID: "task1"}
	events <- want

	got, ok := newIOEventPump(context.Background(), events).next()
	if !ok {
		t.Fatal("expected event pump to read an event")
	}
	if got.Type != want.Type || got.ContainerID != want.ContainerID {
		t.Fatalf("event = %+v, want %+v", got, want)
	}
}

type fakeSandbox struct {
	ttyIn           *os.File
	ttyOut          *os.File
	openTTYsCalled  bool
	winResizeCalled bool
	height          uint32
	width           uint32
	openTTYsErr     error
}

func (f *fakeSandbox) SandboxID() string                                   { return "sandbox-test" }
func (f *fakeSandbox) Start(ctx context.Context) error                     { return nil }
func (f *fakeSandbox) StartContainer(ctx context.Context, id string) error { return nil }
func (f *fakeSandbox) Stop(ctx context.Context, force bool) error          { return nil }
func (f *fakeSandbox) StopContainer(ctx context.Context, id string, force bool) error {
	return nil
}
func (f *fakeSandbox) Delete(ctx context.Context) error                     { return nil }
func (f *fakeSandbox) PauseContainer(ctx context.Context, id string) error  { return nil }
func (f *fakeSandbox) ResumeContainer(ctx context.Context, id string) error { return nil }
func (f *fakeSandbox) KillContainer(ctx context.Context, id string) error   { return nil }
func (f *fakeSandbox) IOStream(ctx context.Context, containerID, taskID string) (io.WriteCloser, io.Reader, io.Reader, error) {
	return nil, nil, nil, nil
}
func (f *fakeSandbox) WinResize(ctx context.Context, containerID string, height, width uint32) error {
	f.winResizeCalled = true
	f.height = height
	f.width = width
	return nil
}
func (f *fakeSandbox) OpenTTYs(ctx context.Context, containerID string) (stdin, stdout *os.File, err error) {
	f.openTTYsCalled = true
	return f.ttyIn, f.ttyOut, f.openTTYsErr
}
func (f *fakeSandbox) UpdateContainer(ctx context.Context, id string, resources specs.LinuxResources) error {
	return nil
}

func newTempTTYFile(t *testing.T) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "tty-*")
	if err != nil {
		t.Fatalf("CreateTemp returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = file.Close()
	})
	return file
}

func TestEnsureAttachRestartsTerminalManagerWithFreshTTYs(t *testing.T) {
	ttyIn := newTempTTYFile(t)
	ttyOut := newTempTTYFile(t)
	manager := &fakeIOManager{}
	svc := NewService(&fakeIOFactory{})
	runtime := &fakeRuntime{sandbox: &fakeSandbox{ttyIn: ttyIn, ttyOut: ttyOut}}
	taskHandle := &fakeTask{
		id:       "task1",
		status:   task.Status_RUNNING,
		terminal: true,
		ioMgr:    manager,
		attachInfo: &ports.AttachInfo{
			Stdin:    "stdin",
			Stdout:   "stdout",
			Terminal: true,
		},
	}

	if err := svc.EnsureAttach(runtime, taskHandle); err != nil {
		t.Fatalf("EnsureAttach returned error: %v", err)
	}

	sandbox := runtime.sandbox.(*fakeSandbox)
	if !sandbox.openTTYsCalled {
		t.Fatal("expected terminal reattach to open fresh TTY handles")
	}
	if !manager.restartWithTTYsCalled {
		t.Fatal("expected RestartWithTTYs to be used for terminal reattach")
	}
	if manager.restartCalled {
		t.Fatal("terminal reattach should not use stale Restart path")
	}
	if manager.restartTTYIn != ttyIn || manager.restartTTYOut != ttyOut {
		t.Fatal("RestartWithTTYs did not receive fresh TTY handles")
	}
	if taskHandle.attachInfo.TTYIn != ttyIn || taskHandle.attachInfo.TTYOut != ttyOut || taskHandle.attachInfo.TTYErr != ttyOut {
		t.Fatal("attach info was not updated with fresh TTY handles")
	}
}

func TestEnsureAttachRestartsIOEventHandlerForExistingManager(t *testing.T) {
	stream := &fakeEventStream{events: make(chan ports.IOEvent, 1)}
	stopCh := make(chan struct{})
	manager := &fakeIOManager{eventStream: stream, stopCh: stopCh}
	svc := NewService(&fakeIOFactory{})
	taskHandle := &fakeTask{
		id:       "reattach-events",
		status:   task.Status_RUNNING,
		terminal: false,
		ioMgr:    manager,
		attached: true,
		attachInfo: &ports.AttachInfo{
			Stdin:  "stdin",
			Stdout: "stdout",
		},
	}

	if err := svc.EnsureAttach(&fakeRuntime{}, taskHandle); err != nil {
		t.Fatalf("EnsureAttach returned error: %v", err)
	}
	if !manager.restartCalled {
		t.Fatal("expected existing manager to restart")
	}
	if stream.subscribeCount == 0 {
		t.Fatal("expected restarted manager event stream to be subscribed")
	}

	stream.events <- ports.IOEvent{Type: ports.IOEventStdinClosed, ContainerID: taskHandle.id}

	select {
	case <-stopCh:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("restarted IO event handler did not stop manager on stdin close")
	}
	if taskHandle.attached {
		t.Fatal("expected restarted event handler to mark task detached")
	}
}

func TestEnsureAttachBootstrapsTerminalSessionWithFreshTTYs(t *testing.T) {
	ttyIn := newTempTTYFile(t)
	ttyOut := newTempTTYFile(t)
	manager := &fakeIOManager{}
	factory := &fakeIOFactory{
		manager: manager,
		stream:  closedEventStream{},
	}
	svc := NewService(factory)
	runtime := &fakeRuntime{sandbox: &fakeSandbox{ttyIn: ttyIn, ttyOut: ttyOut}}
	taskHandle := &fakeTask{
		id:       "task1",
		status:   task.Status_RUNNING,
		terminal: true,
		exitCh:   make(chan struct{}),
		attachInfo: &ports.AttachInfo{
			Stdin:    "stdin",
			Stdout:   "stdout",
			Terminal: true,
		},
	}

	if err := svc.EnsureAttach(runtime, taskHandle); err != nil {
		t.Fatalf("EnsureAttach returned error: %v", err)
	}

	sandbox := runtime.sandbox.(*fakeSandbox)
	if !sandbox.openTTYsCalled {
		t.Fatal("expected terminal bootstrap to open fresh TTY handles")
	}
	if factory.lastConfig.TTYIn != ttyIn || factory.lastConfig.TTYOut != ttyOut || factory.lastConfig.TTYErr != ttyOut {
		t.Fatal("bootstrap config did not receive fresh TTY handles")
	}
	if taskHandle.IOManager() != manager {
		t.Fatal("bootstrap did not install the IO manager")
	}
}

func TestHandleDetachPreservesSessionForReattach(t *testing.T) {
	svc := NewService(nil)
	ioMgr := &fakeIOManager{}
	taskHandle := &fakeTask{
		id:         "detach-test",
		ioMgr:      ioMgr,
		attached:   true,
		exitCh:     make(chan struct{}),
		stdinClose: make(chan struct{}),
	}

	svc.handleDetach(taskHandle)

	if taskHandle.attached {
		t.Fatal("expected task to be marked detached")
	}
	if !ioMgr.stopWithoutClosingCalled {
		t.Fatal("expected StopWithoutClosingFIFOs to be called")
	}
	if ioMgr.stopCalled {
		t.Fatal("did not expect full Stop on detach")
	}
}

func TestHandleDetachStopsManagerOutsideRuntimeLock(t *testing.T) {
	svc := NewService(nil)
	runtime := &fakeRuntime{}
	ioMgr := &lockProbingIOManager{runtime: runtime, t: t}
	taskHandle := &fakeTask{
		id:       "detach-lock-test",
		status:   task.Status_RUNNING,
		ioMgr:    ioMgr,
		attached: true,
	}

	svc.handleIOEvent(runtime, taskHandle, ports.IOEvent{
		Type:        ports.IOEventDetach,
		ContainerID: taskHandle.id,
	})

	if !ioMgr.stopWithoutClosingCalled {
		t.Fatal("expected StopWithoutClosingFIFOs to be called")
	}
	if taskHandle.attached {
		t.Fatal("expected task to be marked detached")
	}
}

func TestHandleIOEventIgnoresOtherContainers(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:     "container-a",
		status: task.Status_RUNNING,
		ioMgr:  &fakeIOManager{},
	}

	svc.handleIOEvent(&fakeRuntime{}, taskHandle, ports.IOEvent{
		Type:        ports.IOEventExitCommand,
		ContainerID: "container-b",
	})

	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("status = %v, want running", taskHandle.status)
	}
	if taskHandle.ioExit {
		t.Fatal("event for another container should not signal IO exit")
	}
}

func TestHandleIOEventIgnoresEmptyContainerID(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:     "container-a",
		status: task.Status_RUNNING,
		ioMgr:  &fakeIOManager{},
	}

	svc.handleIOEvent(&fakeRuntime{}, taskHandle, ports.IOEvent{
		Type: ports.IOEventExitCommand,
	})

	if taskHandle.status != task.Status_RUNNING {
		t.Fatalf("status = %v, want running", taskHandle.status)
	}
	if taskHandle.ioExit {
		t.Fatal("event without container id should not signal IO exit")
	}
}

func TestHandleIOEventRejectsNilTask(t *testing.T) {
	svc := NewService(nil)
	var taskHandle *fakeTask

	svc.handleIOEvent(nil, taskHandle, ports.IOEvent{Type: ports.IOEventError})
}

func TestResolveIOEventPolicyRejectsNilService(t *testing.T) {
	var svc *Service

	_, ok := svc.resolveIOEventPolicy("container-a", ports.IOEvent{
		Type:        ports.IOEventExitCommand,
		ContainerID: "container-a",
	})
	if ok {
		t.Fatal("expected nil service to return no IO event policy")
	}
}

func TestNewServiceGetsIndependentIODefaultPolicySet(t *testing.T) {
	defaultStopReason, ok := defaultIOEventPolicySet.stopReasonForEvent(ports.IOEventExitCommand)
	if !ok {
		t.Fatal("expected default exit command stop reason")
	}
	originalExitStatus := defaultStopReason.exitStatus

	svc := NewService(nil, WithIOEventPolicies([]ioEventPolicy{
		makeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopReason{name: "mutated", exitStatus: originalExitStatus + 1}, handleIOEventStopTask),
	}))
	reason, ok := svc.ioStopReasonForEvent(ports.IOEventExitCommand)
	if !ok {
		t.Fatal("expected service exit command policy after override")
	}
	if reason.exitStatus != originalExitStatus+1 {
		t.Fatalf("service policy should be override-applied, got %d want %d", reason.exitStatus, originalExitStatus+1)
	}

	refreshedReason, ok := defaultIOEventPolicySet.stopReasonForEvent(ports.IOEventExitCommand)
	if !ok {
		t.Fatal("expected default exit command policy after service mutation")
	}
	if refreshedReason.exitStatus != originalExitStatus {
		t.Fatalf("default IO policy set should remain independent from service policies, got status %d want %d", refreshedReason.exitStatus, originalExitStatus)
	}
}

func TestHandleIOEventsRejectsNilTask(t *testing.T) {
	events := make(chan ports.IOEvent)
	close(events)

	svc := NewService(nil)
	var taskHandle *fakeTask

	svc.handleIOEvents(context.Background(), &fakeRuntime{}, taskHandle, events)
}

func TestHandleIOEventUsesInjectedPolicySet(t *testing.T) {
	handled := false
	customHandler := func(_ ioEventContext) {
		handled = true
	}
	svc := NewService(nil, WithIOEventPolicies([]ioEventPolicy{
		makeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopByExitCommand, customHandler),
	}))
	taskHandle := &fakeTask{id: "custom-policy", exitCh: make(chan struct{}), stdinClose: make(chan struct{})}

	svc.handleIOEvent(&fakeRuntime{}, taskHandle, ports.IOEvent{
		Type:        ports.IOEventExitCommand,
		ContainerID: "custom-policy",
	})

	if !handled {
		t.Fatal("expected custom IO event handler to be invoked")
	}
}

func TestNewServicePanicsOnInvalidIOEventPolicySet(t *testing.T) {
	t.Helper()
	defer func() {
		if err := recover(); err == nil {
			t.Fatal("expected panic for invalid injected IO policy set")
		}
	}()

	NewService(nil, WithIOEventPolicies([]ioEventPolicy{{
		eventType: ports.IOEventError,
		plan:      ioEventPlan{},
		handler:   nil,
	}}))
}

func TestNewServiceCheckedReturnsInvalidIOEventPolicySet(t *testing.T) {
	service, err := NewServiceChecked(nil, WithIOEventPolicies([]ioEventPolicy{{
		eventType: ports.IOEventError,
		plan:      ioEventPlan{},
		handler:   nil,
	}}))

	if err == nil {
		t.Fatal("expected error for invalid injected IO policy set")
	}
	if !strings.Contains(err.Error(), "missing handler") {
		t.Fatalf("error = %v, want missing handler reason", err)
	}
	if service != nil {
		t.Fatal("expected nil service when policy validation fails")
	}
}

func TestNewServiceCheckedReturnsInvalidBaseIOEventPolicySet(t *testing.T) {
	service, err := NewServiceChecked(nil, func(config *serviceConfig) {
		config.ioPolicies = ioEventPolicySet{
			byType: map[ports.IOEventType]ioEventPolicy{
				ports.IOEventDetach: makeIOEventPolicy(ports.IOEventDetach, handleIOEventDetach),
			},
		}
	})

	if err == nil {
		t.Fatal("expected error for invalid base IO policy set")
	}
	if !strings.Contains(err.Error(), "inconsistent") {
		t.Fatalf("error = %v, want inconsistent policy set reason", err)
	}
	if service != nil {
		t.Fatal("expected nil service when base policy validation fails")
	}
}

func TestNewServiceCheckedAcceptsValidOptions(t *testing.T) {
	service, err := NewServiceChecked(nil, nil, WithIOEventPolicies([]ioEventPolicy{
		makeIOEventPolicy(ports.IOEventTTYReady, handleIOEventReportError),
	}))

	if err != nil {
		t.Fatalf("NewServiceChecked returned error: %v", err)
	}
	if service == nil {
		t.Fatal("expected service")
	}
	if _, ok := service.resolveIOEventPolicy("task1", ports.IOEvent{
		Type:        ports.IOEventTTYReady,
		ContainerID: "task1",
	}); !ok {
		t.Fatal("expected checked service to include injected IO policy")
	}
}

func TestHandleIOEventStopTaskWithNilRuntime(t *testing.T) {
	now := time.Date(2026, 4, 27, 11, 10, 0, 0, time.UTC)
	svc := NewService(nil, WithClock(func() time.Time { return now }))
	taskHandle := &fakeTask{
		id:         "stop-no-runtime",
		status:     task.Status_RUNNING,
		ioMgr:      &fakeIOManager{},
		exitCh:     make(chan struct{}),
		stdinClose: make(chan struct{}),
	}

	svc.handleIOEvent(nil, taskHandle, ports.IOEvent{
		Type:        ports.IOEventInterrupt,
		ContainerID: "stop-no-runtime",
	})

	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("expected STOPPED, got %s", taskHandle.status)
	}
	if taskHandle.exitStatus != exitstatus.Interrupt() {
		t.Fatalf("expected interrupt exit status %d, got %d", exitstatus.Interrupt(), taskHandle.exitStatus)
	}
	if !taskHandle.exitTime.Equal(now) {
		t.Fatalf("expected exit time %s, got %s", now, taskHandle.exitTime)
	}
	if !taskHandle.ioExit {
		t.Fatal("expected IOExit to be triggered")
	}
	if taskHandle.ioMgr != nil {
		t.Fatal("expected IO manager to be cleared after stop")
	}
}

func TestStartManagedSessionSubscribesBeforeStart(t *testing.T) {
	stream := &fakeEventStream{}
	manager := &fakeIOManager{}
	manager.onStart = func() {
		if stream.subscribeCount == 0 {
			t.Fatal("expected IO events to be subscribed before manager Start")
		}
	}
	factory := &fakeIOFactory{manager: manager, stream: stream}
	svc := NewService(factory)
	taskHandle := &fakeTask{id: "subscribe-before-start"}

	if err := svc.StartInitialSession(context.Background(), &fakeRuntime{}, taskHandle, nil, nil, nil); err != nil {
		t.Fatalf("StartInitialSession returned error: %v", err)
	}
	if !manager.startCalled {
		t.Fatal("expected manager Start to be called")
	}
}

func TestStartInitialSessionStopsManagerWhenEventStreamMissing(t *testing.T) {
	manager := &fakeIOManager{}
	svc := NewService(&fakeIOFactory{manager: manager})
	taskHandle := &fakeTask{
		id:     "missing-events",
		stdin:  "/stdin",
		stdout: "/stdout",
		stderr: "/stderr",
	}

	err := svc.StartInitialSession(context.Background(), &fakeRuntime{}, taskHandle, nil, nil, nil)
	if err == nil {
		t.Fatal("expected StartInitialSession to fail without event stream")
	}
	if !manager.stopCalled {
		t.Fatal("expected manager Stop when event stream is missing")
	}
	if taskHandle.ioMgr != nil {
		t.Fatal("IO manager should not be installed after failed session start")
	}
}

func TestStartInitialSessionStopsManagerWhenEventSubscriberMissing(t *testing.T) {
	manager := &fakeIOManager{}
	svc := NewService(&fakeIOFactory{
		manager: manager,
		stream:  nilSubscriberEventStream{},
	})
	taskHandle := &fakeTask{
		id:     "missing-event-subscriber",
		stdin:  "/stdin",
		stdout: "/stdout",
		stderr: "/stderr",
	}

	err := svc.StartInitialSession(context.Background(), &fakeRuntime{}, taskHandle, nil, nil, nil)
	if err == nil {
		t.Fatal("expected StartInitialSession to fail without event subscriber")
	}
	if !manager.stopCalled {
		t.Fatal("expected manager Stop when event subscriber is missing")
	}
	if taskHandle.ioMgr != nil {
		t.Fatal("IO manager should not be installed after failed session start")
	}
}

func TestResolveFIFOPathsCompletesTerminalPaths(t *testing.T) {
	factory := &strictFIFOFactory{}
	attachInfo := &ports.AttachInfo{
		Stdin:    "/stale/stdin",
		Stdout:   "/valid/stdout",
		Stderr:   "/stale/stderr",
		Terminal: true,
	}

	got := buildAttachSessionInfo(attachSessionInfoRequest{
		factory:    factory,
		namespace:  "ns",
		taskID:     "task1",
		terminal:   true,
		attachInfo: attachInfo,
	})

	if got.Stdin != "/generated/ns/task1/stdin" {
		t.Fatalf("stdin = %q, want generated stdin", got.Stdin)
	}
	if got.Stdout != "/valid/stdout" {
		t.Fatalf("stdout = %q, want preserved valid stdout", got.Stdout)
	}
	if got.Stderr != "" {
		t.Fatalf("stderr = %q, want empty for terminal attach", got.Stderr)
	}
}

func TestResolveFIFOPathsCompletesNonTerminalOutputPaths(t *testing.T) {
	factory := &strictFIFOFactory{}
	attachInfo := &ports.AttachInfo{
		Stdin:  "/valid/stdin",
		Stdout: "/stale/stdout",
		TTYErr: bytes.NewReader(nil),
	}

	got := buildAttachSessionInfo(attachSessionInfoRequest{
		factory:    factory,
		namespace:  "ns",
		taskID:     "task1",
		terminal:   false,
		attachInfo: attachInfo,
	})

	if got.Stdin != "/valid/stdin" {
		t.Fatalf("stdin = %q, want preserved valid stdin", got.Stdin)
	}
	if got.Stdout != "/generated/ns/task1/stdout" {
		t.Fatalf("stdout = %q, want generated stdout", got.Stdout)
	}
	if got.Stderr != "/generated/ns/task1/stderr" {
		t.Fatalf("stderr = %q, want generated stderr", got.Stderr)
	}
}

func TestCloseIOToleratesMissingStdinResources(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id: "closeio-missing-stdin",
	}

	done := make(chan error, 1)
	go func() {
		done <- svc.CloseIO(context.Background(), taskHandle, true)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("CloseIO returned unexpected error: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("CloseIO blocked with missing stdin resources")
	}
}

func TestCloseIOHonorsContextWhileWaitingForStdinCloser(t *testing.T) {
	svc := NewService(nil)
	stdinClosed := make(chan struct{})
	taskHandle := &fakeTask{
		id:         "closeio-cancel",
		stdinPipe:  &attachTrackingWriteCloser{closed: stdinClosed},
		stdinClose: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- svc.CloseIO(ctx, taskHandle, true)
	}()

	select {
	case <-stdinClosed:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("CloseIO did not close stdin pipe")
	}

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("CloseIO error = %v, want context canceled", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("CloseIO did not return after context cancellation")
	}
}

func TestHandleStdinClosedStopsSessionWithoutClearingManager(t *testing.T) {
	svc := NewService(nil)
	ioMgr := &fakeIOManager{}
	taskHandle := &fakeTask{
		id:       "stdin-closed-test",
		ioMgr:    ioMgr,
		attached: true,
	}

	svc.handleStdinClosed(taskHandle)

	if taskHandle.attached {
		t.Fatal("expected stdin close to mark task detached")
	}
	if !ioMgr.stopCalled {
		t.Fatal("expected IO manager Stop to be called")
	}
	if taskHandle.ioMgr != ioMgr {
		t.Fatal("expected IO manager to remain installed after stdin close")
	}
	if ioMgr.stopWithoutClosingCalled {
		t.Fatal("did not expect detach-style stop on stdin close")
	}
}

func TestHandleExitCommandStopsSessionAndMarksExit(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 11, 12, 0, time.UTC)
	svc := NewService(nil, WithClock(func() time.Time { return now }))
	runtime := &fakeRuntime{}
	ioMgr := &fakeIOManager{}
	taskHandle := &fakeTask{
		id:         "exit-test",
		status:     task.Status_RUNNING,
		ioMgr:      ioMgr,
		exitCh:     make(chan struct{}),
		stdinClose: make(chan struct{}),
	}

	svc.handleIOEvent(runtime, taskHandle, ports.IOEvent{
		Type:        ports.IOEventExitCommand,
		ContainerID: "exit-test",
	})

	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("expected STOPPED, got %s", taskHandle.status)
	}
	if taskHandle.exitStatus != exitstatus.Success {
		t.Fatalf("expected exit status 0, got %d", taskHandle.exitStatus)
	}
	if !taskHandle.exitTime.Equal(now) {
		t.Fatalf("expected exit time %s, got %s", now, taskHandle.exitTime)
	}
	if !taskHandle.ioExit {
		t.Fatal("expected IOExit to be triggered")
	}
	if !ioMgr.stopCalled {
		t.Fatal("expected IO manager Stop to be called")
	}
	if taskHandle.ioMgr != nil {
		t.Fatal("expected IO manager to be cleared after exit")
	}
}

func TestHandleExitCommandStopsManagerOutsideRuntimeLock(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 11, 12, 0, time.UTC)
	svc := NewService(nil, WithClock(func() time.Time { return now }))
	runtime := &fakeRuntime{}
	ioMgr := &lockProbingIOManager{runtime: runtime, t: t}
	taskHandle := &fakeTask{
		id:         "exit-lock-test",
		status:     task.Status_RUNNING,
		ioMgr:      ioMgr,
		exitCh:     make(chan struct{}),
		stdinClose: make(chan struct{}),
	}

	svc.handleIOEvent(runtime, taskHandle, ports.IOEvent{
		Type:        ports.IOEventExitCommand,
		ContainerID: taskHandle.id,
	})

	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("expected STOPPED, got %s", taskHandle.status)
	}
	if !ioMgr.stopCalled {
		t.Fatal("expected IO manager Stop to be called")
	}
	if taskHandle.ioMgr != nil {
		t.Fatal("expected IO manager to be cleared under runtime lock before stop")
	}
}

func TestNewServiceAcceptsClockOption(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 13, 14, 0, time.UTC)
	svc := NewService(nil, WithClock(func() time.Time { return now }))

	if got := svc.clockNow(); !got.Equal(now) {
		t.Fatalf("clockNow() = %v, want %v", got, now)
	}
}

func TestHandleInterruptStopsSessionAndMarksSignalExit(t *testing.T) {
	svc := NewService(nil)
	runtime := &fakeRuntime{}
	ioMgr := &fakeIOManager{}
	taskHandle := &fakeTask{
		id:         "interrupt-test",
		status:     task.Status_RUNNING,
		ioMgr:      ioMgr,
		exitCh:     make(chan struct{}),
		stdinClose: make(chan struct{}),
	}

	svc.handleIOEvent(runtime, taskHandle, ports.IOEvent{
		Type:        ports.IOEventInterrupt,
		ContainerID: "interrupt-test",
	})

	if taskHandle.status != task.Status_STOPPED {
		t.Fatalf("expected STOPPED, got %s", taskHandle.status)
	}
	if taskHandle.exitStatus != exitstatus.Interrupt() {
		t.Fatalf("expected interrupt exit status %d, got %d", exitstatus.Interrupt(), taskHandle.exitStatus)
	}
	if !taskHandle.ioExit {
		t.Fatal("expected IOExit to be triggered")
	}
	if !ioMgr.stopCalled {
		t.Fatal("expected IO manager Stop to be called")
	}
	if taskHandle.ioMgr != nil {
		t.Fatal("expected IO manager to be cleared after interrupt")
	}
}

func TestIOStopReasonForEvent(t *testing.T) {
	svc := NewService(nil)

	if reason, ok := svc.ioStopReasonForEvent(ports.IOEventExitCommand); !ok || reason.exitStatus != exitstatus.Success {
		t.Fatalf("exit command reason = %+v, ok=%v", reason, ok)
	}
	if reason, ok := svc.ioStopReasonForEvent(ports.IOEventInterrupt); !ok || reason.exitStatus != exitstatus.Interrupt() {
		t.Fatalf("interrupt reason = %+v, ok=%v", reason, ok)
	}
	if _, ok := svc.ioStopReasonForEvent(ports.IOEventDetach); ok {
		t.Fatal("detach should not be a stop reason")
	}
}

func TestIOEventPolicyOverridesOptionPreservesDefaultsWhenNil(t *testing.T) {
	svc := NewService(nil, WithIOEventPolicies(nil))

	if reason, ok := svc.ioStopReasonForEvent(ports.IOEventExitCommand); !ok || reason.exitStatus != exitstatus.Success {
		t.Fatalf("exit command reason = %+v, ok=%v", reason, ok)
	}
}

func TestIOStopReasonForEventUsesInjectedPolicyOverrides(t *testing.T) {
	svc := NewService(nil, WithIOEventPolicies([]ioEventPolicy{
		makeIOEventPolicyWithStop(ports.IOEventDetach, ioStopReason{name: "policy-detach", exitStatus: 42}, handleIOEventDetach),
	}))

	if reason, ok := svc.ioStopReasonForEvent(ports.IOEventExitCommand); !ok || reason.exitStatus != exitstatus.Success {
		t.Fatalf("exit command reason = %+v, ok=%v", reason, ok)
	}

	reason, ok := svc.ioStopReasonForEvent(ports.IOEventDetach)
	if !ok {
		t.Fatal("expected injected detach policy to expose stop reason")
	}
	if reason.exitStatus != 42 {
		t.Fatalf("expected injected stop exit status 42, got %d", reason.exitStatus)
	}
	if reason.name != "policy-detach" {
		t.Fatalf("expected injected stop reason name policy-detach, got %q", reason.name)
	}
}

func TestIOEventPolicyOptionsComposeAcrossCalls(t *testing.T) {
	svc := NewService(nil,
		WithIOEventPolicies([]ioEventPolicy{
			makeIOEventPolicyWithStop(ports.IOEventDetach, ioStopReason{name: "policy-detach", exitStatus: 42}, handleIOEventDetach),
		}),
		WithIOEventPolicies([]ioEventPolicy{
			makeIOEventPolicyWithStop(ports.IOEventStdinClosed, ioStopReason{name: "policy-stdin", exitStatus: 43}, handleIOEventStdinClosed),
		}),
	)

	reasonDetach, ok := svc.ioStopReasonForEvent(ports.IOEventDetach)
	if !ok || reasonDetach.exitStatus != 42 || reasonDetach.name != "policy-detach" {
		t.Fatalf("composed detach reason = %+v, ok=%v", reasonDetach, ok)
	}
	reasonStdin, ok := svc.ioStopReasonForEvent(ports.IOEventStdinClosed)
	if !ok || reasonStdin.exitStatus != 43 || reasonStdin.name != "policy-stdin" {
		t.Fatalf("composed stdin reason = %+v, ok=%v", reasonStdin, ok)
	}

	if reasonExit, ok := svc.ioStopReasonForEvent(ports.IOEventExitCommand); !ok || reasonExit.exitStatus != exitstatus.Success {
		t.Fatalf("exit command reason = %+v, ok=%v", reasonExit, ok)
	}
}

func TestIOEventPolicyOptionsPreserveOverridesWithEmptyOption(t *testing.T) {
	svc := NewService(nil,
		WithIOEventPolicies([]ioEventPolicy{
			makeIOEventPolicyWithStop(ports.IOEventDetach, ioStopReason{name: "policy-detach", exitStatus: 42}, handleIOEventDetach),
		}),
		WithIOEventPolicies(nil),
	)

	reasonDetach, ok := svc.ioStopReasonForEvent(ports.IOEventDetach)
	if !ok || reasonDetach.exitStatus != 42 {
		t.Fatalf("composed reason with empty option should preserve detach override, got %+v ok=%v", reasonDetach, ok)
	}
}

func TestIOEventPolicyOptionsOverrideSameTypeAcrossCalls(t *testing.T) {
	svc := NewService(nil,
		WithIOEventPolicies([]ioEventPolicy{
			makeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopReason{name: "first", exitStatus: 41}, handleIOEventStopTask),
		}),
		WithIOEventPolicies([]ioEventPolicy{
			makeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopReason{name: "second", exitStatus: 42}, handleIOEventStopTask),
		}),
	)

	reason, ok := svc.ioStopReasonForEvent(ports.IOEventExitCommand)
	if !ok {
		t.Fatal("expected exit command stop reason")
	}
	if reason.exitStatus != 42 || reason.name != "second" {
		t.Fatalf("expected last override to win, got %+v ok=%v", reason, ok)
	}
}

func TestIOEventPolicyOptionsDeduplicateTypeWithinSingleCall(t *testing.T) {
	svc := NewService(nil,
		WithIOEventPolicies([]ioEventPolicy{
			makeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopReason{name: "first", exitStatus: 41}, handleIOEventStopTask),
			makeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopReason{name: "second", exitStatus: 42}, handleIOEventStopTask),
		}),
	)

	reason, ok := svc.ioStopReasonForEvent(ports.IOEventExitCommand)
	if !ok {
		t.Fatal("expected exit command stop reason")
	}
	if reason.exitStatus != 42 || reason.name != "second" {
		t.Fatalf("expected last policy in the same option to win, got %+v", reason)
	}
}

func TestStopFromIOEventDoesNotOverwriteStoppedTaskExitInfo(t *testing.T) {
	svc := NewService(nil)
	runtime := &fakeRuntime{}
	ioMgr := &fakeIOManager{}
	exitedAt := time.Unix(10, 0)
	taskHandle := &fakeTask{
		id:         "already-stopped",
		status:     task.Status_STOPPED,
		exitStatus: 123,
		exitTime:   exitedAt,
		ioMgr:      ioMgr,
	}

	svc.handleIOEvent(runtime, taskHandle, ports.IOEvent{
		Type:        ports.IOEventInterrupt,
		ContainerID: "already-stopped",
	})

	if taskHandle.exitStatus != 123 {
		t.Fatalf("exit status = %d, want preserved 123", taskHandle.exitStatus)
	}
	if !taskHandle.exitTime.Equal(exitedAt) {
		t.Fatalf("exit time = %v, want %v", taskHandle.exitTime, exitedAt)
	}
	if taskHandle.ioExit {
		t.Fatal("already stopped task should not signal IO exit again")
	}
	if ioMgr.stopCalled {
		t.Fatal("already stopped task should not stop IO manager again")
	}
}

func TestPrepareResizeBootstrapsManagedSessionWhenManagerMissing(t *testing.T) {
	stdinReader, stdinWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	defer stdinReader.Close()
	defer stdinWriter.Close()

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	defer stdoutReader.Close()
	defer stdoutWriter.Close()

	manager := &fakeIOManager{}
	stream := &fakeEventStream{}
	factory := &fakeIOFactory{
		manager: manager,
		stream:  stream,
	}
	svc := NewService(factory)

	taskHandle := &fakeTask{
		id:         "resize-test",
		status:     task.Status_RUNNING,
		stdin:      "/stdin",
		stdout:     "/stdout",
		stderr:     "/stderr",
		terminal:   true,
		stdinClose: make(chan struct{}),
		exitCh:     make(chan struct{}),
		attachInfo: &ports.AttachInfo{
			Stdin:    "/stdin",
			Stdout:   "/stdout",
			Stderr:   "/stderr",
			Terminal: true,
		},
	}
	sandbox := &fakeSandbox{
		ttyIn:  stdinWriter,
		ttyOut: stdoutReader,
	}
	runtime := &fakeRuntime{sandbox: sandbox}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := svc.PrepareResize(ctx, runtime, taskHandle, 24, 80); err != nil {
		t.Fatalf("PrepareResize returned error: %v", err)
	}

	if !manager.startCalled {
		t.Fatal("expected IO session start to be called")
	}
	if taskHandle.ioMgr != manager {
		t.Fatal("expected IO manager to be installed on task")
	}
	if taskHandle.attachInfo == nil {
		t.Fatal("expected attach info to be updated")
	}
	if taskHandle.attachInfo.TTYIn != stdinWriter {
		t.Fatal("expected fresh tty stdin to be stored in attach info")
	}
	if taskHandle.attachInfo.TTYOut != stdoutReader {
		t.Fatal("expected fresh tty stdout to be stored in attach info")
	}
	if !sandbox.openTTYsCalled {
		t.Fatal("expected sandbox OpenTTYs to be called")
	}
	if !sandbox.winResizeCalled {
		t.Fatal("expected sandbox WinResize to be called")
	}
	if sandbox.height != 24 || sandbox.width != 80 {
		t.Fatalf("unexpected resize dimensions: got %dx%d", sandbox.height, sandbox.width)
	}
	if stream.subscribeCount != len(defaultIOEventPolicySet.eventTypes()) {
		t.Fatalf("expected %d IO event subscriptions, got %d", len(defaultIOEventPolicySet.eventTypes()), stream.subscribeCount)
	}
	if factory.lastConfig.ContainerID != taskHandle.id {
		t.Fatalf("unexpected session config container id: %s", factory.lastConfig.ContainerID)
	}
	if factory.lastConfig.StdinFIFO != "/stdin" || factory.lastConfig.StdoutFIFO != "/stdout" {
		t.Fatalf("unexpected session FIFO config: stdin=%q stdout=%q", factory.lastConfig.StdinFIFO, factory.lastConfig.StdoutFIFO)
	}
}

func TestPrepareResizeRequiresFactoryBeforeOpeningTTYWhenManagerMissing(t *testing.T) {
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:         "resize-missing-factory",
		status:     task.Status_RUNNING,
		terminal:   true,
		stdinClose: make(chan struct{}),
		exitCh:     make(chan struct{}),
		attachInfo: &ports.AttachInfo{Terminal: true},
	}
	sandbox := &fakeSandbox{}
	runtime := &fakeRuntime{sandbox: sandbox}

	err := svc.PrepareResize(context.Background(), runtime, taskHandle, 24, 80)
	if !errors.Is(err, er.FactoryNotConfigured) {
		t.Fatalf("PrepareResize error = %v, want factory not configured", err)
	}
	if sandbox.openTTYsCalled {
		t.Fatal("OpenTTYs should not run before session factory is available")
	}
}

func TestPrepareResizeReturnsFreshTTYError(t *testing.T) {
	expectedErr := errors.New("open tty failed")
	svc := NewService(&fakeIOFactory{
		manager: &fakeIOManager{},
		stream:  &fakeEventStream{},
	})
	taskHandle := &fakeTask{
		id:         "resize-open-tty-error",
		status:     task.Status_RUNNING,
		stdin:      "/stdin",
		stdout:     "/stdout",
		stdinClose: make(chan struct{}),
		exitCh:     make(chan struct{}),
		attachInfo: &ports.AttachInfo{},
	}
	sandbox := &fakeSandbox{openTTYsErr: expectedErr}
	runtime := &fakeRuntime{sandbox: sandbox}

	err := svc.PrepareResize(context.Background(), runtime, taskHandle, 24, 80)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("PrepareResize error = %v, want %v", err, expectedErr)
	}
	if !sandbox.openTTYsCalled {
		t.Fatal("expected sandbox OpenTTYs to be called")
	}
	if sandbox.winResizeCalled {
		t.Fatal("WinResize should not run after fresh TTY open failure")
	}
}

func TestPrepareResizeRejectsNilFreshTTYHandles(t *testing.T) {
	svc := NewService(&fakeIOFactory{
		manager: &fakeIOManager{},
		stream:  &fakeEventStream{},
	})
	taskHandle := &fakeTask{
		id:         "resize-nil-tty",
		status:     task.Status_RUNNING,
		stdinClose: make(chan struct{}),
		exitCh:     make(chan struct{}),
		attachInfo: &ports.AttachInfo{},
	}
	sandbox := &fakeSandbox{}
	runtime := &fakeRuntime{sandbox: sandbox}

	err := svc.PrepareResize(context.Background(), runtime, taskHandle, 24, 80)
	if err == nil {
		t.Fatal("expected PrepareResize to reject nil fresh TTY handles")
	}
	if sandbox.winResizeCalled {
		t.Fatal("WinResize should not run after nil fresh TTY handles")
	}
}

func TestPrepareResizeReturnsRestartWithTTYsError(t *testing.T) {
	stdinReader, stdinWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	defer stdinReader.Close()
	defer stdinWriter.Close()

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	defer stdoutReader.Close()
	defer stdoutWriter.Close()

	expectedErr := errors.New("restart failed")
	manager := &fakeIOManager{restartWithTTYsErr: expectedErr}
	svc := NewService(nil)
	taskHandle := &fakeTask{
		id:         "resize-restart-error",
		status:     task.Status_RUNNING,
		terminal:   false,
		stdinClose: make(chan struct{}),
		exitCh:     make(chan struct{}),
		ioMgr:      manager,
		attachInfo: &ports.AttachInfo{},
	}
	sandbox := &fakeSandbox{
		ttyIn:  stdinWriter,
		ttyOut: stdoutReader,
	}
	runtime := &fakeRuntime{sandbox: sandbox}

	err = svc.PrepareResize(context.Background(), runtime, taskHandle, 24, 80)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("PrepareResize error = %v, want %v", err, expectedErr)
	}
	if !manager.restartWithTTYsCalled {
		t.Fatal("expected RestartWithTTYs to be called")
	}
	if sandbox.winResizeCalled {
		t.Fatal("WinResize should not run after IO manager restart failure")
	}
	if _, err := stdinWriter.Write([]byte("x")); err == nil {
		t.Fatal("expected fresh tty input to be closed after restart failure")
	}
}

func TestStartInitialSessionDoesNotInstallManagerWhenStartFails(t *testing.T) {
	expectedErr := errors.New("start failed")
	manager := &fakeIOManager{startErr: expectedErr}
	stream := &fakeEventStream{}
	svc := NewService(&fakeIOFactory{
		manager: manager,
		stream:  stream,
	})
	taskHandle := &fakeTask{
		id:         "start-session-error",
		stdin:      "/stdin",
		stdout:     "/stdout",
		stdinClose: make(chan struct{}),
		exitCh:     make(chan struct{}),
	}
	runtime := &fakeRuntime{}

	err := svc.StartInitialSession(context.Background(), runtime, taskHandle, nil, nil, nil)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("StartInitialSession error = %v, want %v", err, expectedErr)
	}
	if stream.subscribeCount == 0 {
		t.Fatal("event stream should be subscribed before manager Start")
	}
	if taskHandle.ioMgr != nil {
		t.Fatal("manager should not be installed when start fails")
	}
	if !manager.stopCalled {
		t.Fatal("manager Stop should be called after start failure")
	}
}

func TestStartInitialSessionRequiresFactoryOutputs(t *testing.T) {
	ctx := context.Background()
	taskHandle := &fakeTask{
		id:         "start-session-missing-output",
		stdin:      "/stdin",
		stdout:     "/stdout",
		stdinClose: make(chan struct{}),
		exitCh:     make(chan struct{}),
	}
	runtime := &fakeRuntime{}

	t.Run("manager", func(t *testing.T) {
		svc := NewService(&fakeIOFactory{stream: &fakeEventStream{}})
		if err := svc.StartInitialSession(ctx, runtime, taskHandle, nil, nil, nil); err == nil {
			t.Fatal("expected missing manager error")
		}
	})

	t.Run("event stream", func(t *testing.T) {
		svc := NewService(&fakeIOFactory{manager: &fakeIOManager{}})
		if err := svc.StartInitialSession(ctx, runtime, taskHandle, nil, nil, nil); err == nil {
			t.Fatal("expected missing event stream error")
		}
	})
}
