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

// TestVerifyMicadProcessRejectsRecycledPID covers the PID-reuse wedge:
// kill(pid, 0) succeeds on a recycled PID, but the process is not micad.
// verifyMicadProcess must reject it so micadDetect reports "not running" and
// setupMicad re-starts micad instead of trusting a stale pidfile.
func TestVerifyMicadProcessRejectsRecycledPID(t *testing.T) {
	original := procCommReader
	defer func() { procCommReader = original }()

	tests := []struct {
		name    string
		comm    string
		readErr error
		wantErr bool
	}{
		{name: "matching comm", comm: "micad", wantErr: false},
		{name: "matching comm prefix", comm: "micad-worker", wantErr: false},
		{name: "recycled pid (unrelated process)", comm: "containerd", wantErr: true},
		{name: "empty comm", comm: "", wantErr: true},
		{name: "unreadable comm (process gone)", comm: "", readErr: errors.New("open /proc/999/comm: no such file or directory"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procCommReader = func(pid int) (string, error) {
				if pid != 999 {
					t.Fatalf("pid = %d, want 999", pid)
				}
				return tt.comm, tt.readErr
			}
			err := verifyMicadProcess(999)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestDaemonStateStartsMicadWhenPidfilePointsAtRecycledPID simulates a stale
// pidfile: detect() returns the PID-reuse error on the first call (micad
// crashed, PID reused), then a real start succeeds and the second detect
// returns the fresh micad pid. Without identity verification this path is
// never taken — setupMicad would no-op and the shim would talk to a dead
// daemon.
func TestDaemonStateStartsMicadWhenPidfilePointsAtRecycledPID(t *testing.T) {
	attempts := 0
	started := false
	state, err := daemonState(
		func() (int, error) {
			attempts++
			if attempts == 1 {
				// First detect: stale pidfile + recycled PID.
				return 42, errors.New("pid 42 is not micad (comm=\"bash\"), pidfile is stale")
			}
			// Second detect: micad was re-started, fresh pid.
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
		t.Fatal("expected setupMicad to start micad after recycled-PID detect failure")
	}
	if state.Pid != 77 || state.State != DaemonRunning {
		t.Fatalf("unexpected state: %+v", state)
	}
}
