package attach

import (
	"io"
	"testing"
	"time"

	"github.com/containerd/containerd/api/types/task"
	"micrun/internal/ports"
)

func TestIOManagerControlNoopWhenTaskIsNil(t *testing.T) {
	defer func() {
		if got := recover(); got != nil {
			t.Fatalf("expected no panic, got %v", got)
		}
	}()
	stopIOManager(nil)
	detachIOManager(nil)
	stopAndClearIOManager(nil)
}

func TestIOManagerControlNoopWhenManagerMissing(t *testing.T) {
	taskHandle := &ioManagerTaskHandle{}
	defer func() {
		if got := recover(); got != nil {
			t.Fatalf("expected no panic, got %v", got)
		}
	}()
	stopIOManager(taskHandle)
	detachIOManager(taskHandle)
	stopAndClearIOManager(taskHandle)
}

func TestIOManagerControlClearInvokesStop(t *testing.T) {
	manager := &fakeManagedIOManager{}
	taskHandle := &ioManagerTaskHandle{manager: manager}
	stopAndClearIOManager(taskHandle)
	if !manager.stopCalled {
		t.Fatal("expected manager.Stop to be called")
	}
	if taskHandle.manager != nil {
		t.Fatal("expected manager to be cleared after stopAndClearIOManager")
	}
}

func TestIOManagerControlStopDoesNotClearManager(t *testing.T) {
	manager := &fakeManagedIOManager{}
	taskHandle := &ioManagerTaskHandle{manager: manager}
	stopIOManager(taskHandle)
	if !manager.stopCalled {
		t.Fatal("expected manager.Stop to be called")
	}
	if taskHandle.manager == nil {
		t.Fatal("unexpected manager clear on plain stopIOManager")
	}
}

func TestIOManagerControlDetachPreservesManager(t *testing.T) {
	manager := &fakeManagedIOManager{}
	taskHandle := &ioManagerTaskHandle{manager: manager}
	detachIOManager(taskHandle)
	if !manager.stopWithoutClosingCalled {
		t.Fatal("expected StopWithoutClosingFIFOs to be called")
	}
	if taskHandle.manager == nil {
		t.Fatal("expected manager not to be cleared on detachIOManager")
	}
}

type fakeManagedIOManager struct {
	stopCalled               bool
	stopWithoutClosingCalled bool
}

func (f *fakeManagedIOManager) Start() error             { return nil }
func (f *fakeManagedIOManager) Stop()                    { f.stopCalled = true }
func (f *fakeManagedIOManager) StopWithoutClosingFIFOs() { f.stopWithoutClosingCalled = true }
func (f *fakeManagedIOManager) Restart() error           { return nil }
func (f *fakeManagedIOManager) RestartWithTTYs(ttyIn io.WriteCloser, ttyOut io.Reader) error {
	return nil
}
func (f *fakeManagedIOManager) IsRunning() bool { return false }
func (f *fakeManagedIOManager) EventStream() ports.IOEventStream {
	return closedEventStream{}
}

type ioManagerTaskHandle struct {
	manager ports.IOManager
}

func (t *ioManagerTaskHandle) ID() string                           { return "io-manager-task" }
func (t *ioManagerTaskHandle) Bundle() string                       { return "" }
func (t *ioManagerTaskHandle) PID() uint32                          { return 0 }
func (t *ioManagerTaskHandle) Status() task.Status                  { return task.Status_UNKNOWN }
func (t *ioManagerTaskHandle) SetStatus(task.Status)                {}
func (t *ioManagerTaskHandle) Terminal() bool                       { return false }
func (t *ioManagerTaskHandle) StdinPath() string                    { return "" }
func (t *ioManagerTaskHandle) StdoutPath() string                   { return "" }
func (t *ioManagerTaskHandle) StderrPath() string                   { return "" }
func (t *ioManagerTaskHandle) ExitStatus() uint32                   { return 0 }
func (t *ioManagerTaskHandle) ExitTime() time.Time                  { return time.Time{} }
func (t *ioManagerTaskHandle) SetExitInfo(uint32, time.Time)        {}
func (t *ioManagerTaskHandle) StdinPipe() io.WriteCloser            { return nil }
func (t *ioManagerTaskHandle) StdinCloser() chan struct{}           { return nil }
func (t *ioManagerTaskHandle) ExitChan() chan struct{}              { return nil }
func (t *ioManagerTaskHandle) IOExit()                              {}
func (t *ioManagerTaskHandle) CanBeSandbox() bool                   { return false }
func (t *ioManagerTaskHandle) IsCriSandbox() bool                   { return false }
func (t *ioManagerTaskHandle) Annotations() map[string]string       { return nil }
func (t *ioManagerTaskHandle) IOManager() ports.IOManager           { return t.manager }
func (t *ioManagerTaskHandle) SetIOManager(manager ports.IOManager) { t.manager = manager }
func (t *ioManagerTaskHandle) AttachInfo() *ports.AttachInfo        { return nil }
func (t *ioManagerTaskHandle) SetAttachInfo(*ports.AttachInfo)      {}
func (t *ioManagerTaskHandle) SetStdinPipe(io.WriteCloser)          {}
func (t *ioManagerTaskHandle) SetAttached(bool) bool                { return false }
