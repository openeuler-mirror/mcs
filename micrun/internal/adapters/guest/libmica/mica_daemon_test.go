package libmica

import (
	"errors"
	"reflect"
	"testing"

	er "micrun/internal/support/errors"
)

type fakeServiceCommandRunner struct {
	available map[string]bool
	runErr    map[string]error
	calls     []string
}

func (f *fakeServiceCommandRunner) LookPath(file string) (string, error) {
	if f.available[file] {
		return "/usr/bin/" + file, nil
	}
	return "", errors.New("not found")
}

func (f *fakeServiceCommandRunner) Run(name string, args ...string) error {
	f.calls = append(f.calls, name)
	return f.runErr[name]
}

func TestStartMicadServiceUsesFirstAvailableCommand(t *testing.T) {
	runner := &fakeServiceCommandRunner{
		available: map[string]bool{"systemctl": true, "service": true},
		runErr:    map[string]error{},
	}

	err := startMicadService(runner, micadServiceStartCommands)
	if err != nil {
		t.Fatalf("startMicadService returned error: %v", err)
	}
	if !reflect.DeepEqual(runner.calls, []string{"systemctl"}) {
		t.Fatalf("calls = %v, want [systemctl]", runner.calls)
	}
}

func TestStartMicadServiceFallsBackAfterRunFailure(t *testing.T) {
	runner := &fakeServiceCommandRunner{
		available: map[string]bool{"systemctl": true, "service": true},
		runErr:    map[string]error{"systemctl": errors.New("failed")},
	}

	err := startMicadService(runner, micadServiceStartCommands)
	if err != nil {
		t.Fatalf("startMicadService returned error: %v", err)
	}
	if !reflect.DeepEqual(runner.calls, []string{"systemctl", "service"}) {
		t.Fatalf("calls = %v, want [systemctl service]", runner.calls)
	}
}

func TestStartMicadServiceReportsMissingCommands(t *testing.T) {
	runner := &fakeServiceCommandRunner{
		available: map[string]bool{},
		runErr:    map[string]error{},
	}

	if err := startMicadService(runner, micadServiceStartCommands); err == nil {
		t.Fatal("expected error for missing commands")
	}
}

func TestDaemonStateReportsRunningDaemon(t *testing.T) {
	state, err := daemonState(
		func() (int, error) { return 42, nil },
		func() error { t.Fatal("starter should not be called"); return nil },
		func() bool { return true },
	)
	if err != nil {
		t.Fatalf("daemonState returned error: %v", err)
	}
	if state.Pid != 42 || state.State != DaemonRunning || !state.Listening {
		t.Fatalf("unexpected state: %+v", state)
	}
}

func TestDaemonStateStartsDaemonWhenInitialDetectFails(t *testing.T) {
	attempts := 0
	started := false
	state, err := daemonState(
		func() (int, error) {
			attempts++
			if attempts == 1 {
				return 0, errors.New("not running")
			}
			return 77, nil
		},
		func() error {
			started = true
			return nil
		},
		func() bool { return true },
	)
	if err != nil {
		t.Fatalf("daemonState returned error: %v", err)
	}
	if !started {
		t.Fatal("expected starter to be called")
	}
	if state.Pid != 77 || state.State != DaemonRunning {
		t.Fatalf("unexpected state: %+v", state)
	}
}

func TestDaemonStateReportsStoppedWhenDetectAfterStartFails(t *testing.T) {
	state, err := daemonState(
		func() (int, error) { return 0, errors.New("not running") },
		func() error { return nil },
		func() bool { return true },
	)
	if !errors.Is(err, er.MicadNotRunning) {
		t.Fatalf("daemonState error = %v, want MicadNotRunning", err)
	}
	if state == nil || state.State != DaemonStopped || state.Pid != 0 || state.Listening {
		t.Fatalf("unexpected stopped state: %+v", state)
	}
}
