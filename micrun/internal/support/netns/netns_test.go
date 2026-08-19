package netns

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"micrun/internal/support/lockutil"
)

func resetHoldersForTest(t *testing.T) {
	t.Helper()
	lockutil.WithLock(&holdersMu, func() {
		holders = make(map[string]*holder)
	})
	t.Cleanup(func() {
		lockutil.WithLock(&holdersMu, func() {
			holders = make(map[string]*holder)
		})
	})
}

func registerHolderForTest(id string, _ int, pid int) {
	lockutil.WithLock(&holdersMu, func() {
		holders[id] = &holder{pid: pid} // cmd == nil: registered/recovered holder
	})
}

func TestRunHolderCommandIgnoresRegularArgs(t *testing.T) {
	if RunHolderCommand([]string{"--version"}) {
		t.Fatal("expected regular args to be ignored")
	}
}

func TestRunHolderCommandRequiresExactHolderInvocation(t *testing.T) {
	waited := false
	if runHolderCommand([]string{"run", HolderArg}, func() { waited = true }) {
		t.Fatal("expected mixed args to be ignored")
	}
	if waited {
		t.Fatal("mixed args should not enter holder wait")
	}
}

func TestRunHolderCommandRunsExactHolderInvocation(t *testing.T) {
	waited := false
	if !runHolderCommand([]string{HolderArg}, func() { waited = true }) {
		t.Fatal("expected exact holder invocation to be handled")
	}
	if !waited {
		t.Fatal("holder invocation should wait for signal")
	}
}

func withIsolatedHolders(t *testing.T) {
	t.Helper()
	holdersMu.Lock()
	oldHolders := holders
	holders = make(map[string]*holder)
	holdersMu.Unlock()
	t.Cleanup(func() {
		holdersMu.Lock()
		holders = oldHolders
		holdersMu.Unlock()
	})
}

func TestDeleteHolderIfCurrentPreservesReplacement(t *testing.T) {
	withIsolatedHolders(t)

	first := &holder{pid: 1}
	second := &holder{pid: 2}
	putHolder("sandbox", first)
	putHolder("sandbox", second)

	deleteHolderIfCurrent("sandbox", first)
	if pid, ok := holderPID("sandbox"); !ok || pid != 2 {
		t.Fatalf("holderPID after stale delete = (%d, %v), want (2, true)", pid, ok)
	}

	deleteHolderIfCurrent("sandbox", second)
	if pid, ok := holderPID("sandbox"); ok || pid != 0 {
		t.Fatalf("holderPID after current delete = (%d, %v), want (0, false)", pid, ok)
	}
}

func TestCreateHolderIfAbsentSkipsFactoryForExisting(t *testing.T) {
	withIsolatedHolders(t)

	// A started holder (cmd != nil) is guarded by its watcher; the reuse
	// branch must take it without a cmdline identity check.
	existing := &holder{pid: os.Getpid(), cmd: &exec.Cmd{}, done: make(chan error, 1)}
	putHolder("sandbox", existing)
	called := false
	got, created, err := createHolderIfAbsent("sandbox", func() (*holder, error) {
		called = true
		return &holder{pid: 8}, nil
	})

	if err != nil {
		t.Fatalf("createHolderIfAbsent error = %v", err)
	}
	if created {
		t.Fatal("expected existing holder to be reused")
	}
	if called {
		t.Fatal("factory should not be called for existing holder")
	}
	if got != existing {
		t.Fatal("expected existing holder to be returned")
	}
}

func TestCreateHolderIfAbsentStoresCreatedHolder(t *testing.T) {
	withIsolatedHolders(t)

	createdHolder := &holder{pid: 9}
	got, created, err := createHolderIfAbsent("sandbox", func() (*holder, error) {
		return createdHolder, nil
	})

	if err != nil {
		t.Fatalf("createHolderIfAbsent error = %v", err)
	}
	if !created {
		t.Fatal("expected holder to be created")
	}
	if got != createdHolder {
		t.Fatal("expected created holder to be returned")
	}
	if pid, ok := holderPID("sandbox"); !ok || pid != 9 {
		t.Fatalf("holderPID = (%d, %v), want (9, true)", pid, ok)
	}
}

func TestCreateHolderIfAbsentPropagatesFactoryError(t *testing.T) {
	withIsolatedHolders(t)

	got, created, err := createHolderIfAbsent("sandbox", func() (*holder, error) {
		return nil, errors.New("factory failed")
	})

	if err == nil || !strings.Contains(err.Error(), "factory failed") {
		t.Fatalf("createHolderIfAbsent error = %v, want factory error", err)
	}
	if got != nil || created {
		t.Fatalf("createHolderIfAbsent = (%v, %v), want (nil, false) on factory error", got, created)
	}
	if _, ok := holderPID("sandbox"); ok {
		t.Fatal("holder registered despite factory error")
	}
}

func TestReplaceHolderReleasesPreviousHolder(t *testing.T) {
	withIsolatedHolders(t)

	released := false
	previous := &holder{
		pid: 7,
		release: func() {
			released = true
		},
	}
	replacement := &holder{pid: 8}
	putHolder("sandbox", previous)

	got, replaced := replaceHolder("sandbox", replacement)

	if got != previous {
		t.Fatal("expected previous holder to be returned")
	}
	if !replaced {
		t.Fatal("expected replacement to be reported")
	}
	if !released {
		t.Fatal("previous holder was not released")
	}
	if pid, ok := holderPID("sandbox"); !ok || pid != 8 {
		t.Fatalf("holderPID = (%d, %v), want (8, true)", pid, ok)
	}
}

func TestReplaceHolderKeepsSamePID(t *testing.T) {
	withIsolatedHolders(t)

	released := false
	previous := &holder{
		pid: 7,
		release: func() {
			released = true
		},
	}
	putHolder("sandbox", previous)

	got, replaced := replaceHolder("sandbox", &holder{pid: 7})

	if got != previous {
		t.Fatal("expected existing holder to be returned")
	}
	if replaced {
		t.Fatal("same pid should be idempotent")
	}
	if released {
		t.Fatal("same pid holder should not be released")
	}
	if pid, ok := holderPID("sandbox"); !ok || pid != 7 {
		t.Fatalf("holderPID = (%d, %v), want (7, true)", pid, ok)
	}
}

func TestCreateHolderIfAbsentRejectsNilHolder(t *testing.T) {
	withIsolatedHolders(t)

	got, created, err := createHolderIfAbsent("sandbox", func() (*holder, error) {
		return nil, nil
	})

	if err == nil {
		t.Fatal("expected nil holder error")
	}
	if created {
		t.Fatal("nil holder should not be marked created")
	}
	if got != nil {
		t.Fatal("nil holder should not return a holder")
	}
	if _, ok := holderPID("sandbox"); ok {
		t.Fatal("nil holder should not be stored")
	}
}

func TestIsMicrunExecutable(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/usr/local/bin/micrun", true},
		{"/usr/local/bin/containerd-shim-mica-v2", true},
		{"/tmp/netns.test", false},
		{"sleep", false},
	}

	for _, tt := range tests {
		if got := isMicrunExecutable(tt.path); got != tt.want {
			t.Fatalf("isMicrunExecutable(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestTerminateByPIDStopsAfterGracefulExit(t *testing.T) {
	var signals []syscall.Signal
	terminator := pidTerminator{
		signal: func(_ int, sig syscall.Signal) error {
			signals = append(signals, sig)
			return nil
		},
		gone: func(_ int) bool {
			return false
		},
		waitForExit: func(pid int, timeout, pollInterval time.Duration, gone pidGoneFunc) bool {
			if pid != 42 {
				t.Fatalf("pid = %d, want 42", pid)
			}
			if timeout != pidTerminationGracePeriod {
				t.Fatalf("timeout = %s, want %s", timeout, pidTerminationGracePeriod)
			}
			if pollInterval != pidTerminationPollInterval {
				t.Fatalf("pollInterval = %s, want %s", pollInterval, pidTerminationPollInterval)
			}
			return true
		},
		gracePeriod:  pidTerminationGracePeriod,
		pollInterval: pidTerminationPollInterval,
	}

	if err := terminateByPIDWith(42, terminator); err != nil {
		t.Fatalf("terminateByPIDWith error = %v", err)
	}
	if len(signals) != 1 || signals[0] != syscall.SIGTERM {
		t.Fatalf("signals = %v, want [SIGTERM]", signals)
	}
}

func TestTerminateByPIDKillsAfterGracefulTimeout(t *testing.T) {
	var signals []syscall.Signal
	terminator := pidTerminator{
		signal: func(_ int, sig syscall.Signal) error {
			signals = append(signals, sig)
			return nil
		},
		gone:        func(_ int) bool { return false },
		waitForExit: func(int, time.Duration, time.Duration, pidGoneFunc) bool { return false },
	}

	if err := terminateByPIDWith(42, terminator); err != nil {
		t.Fatalf("terminateByPIDWith error = %v", err)
	}
	if len(signals) != 2 || signals[0] != syscall.SIGTERM || signals[1] != syscall.SIGKILL {
		t.Fatalf("signals = %v, want [SIGTERM SIGKILL]", signals)
	}
}

func TestTerminateByPIDIgnoresMissingProcess(t *testing.T) {
	var signals []syscall.Signal
	terminator := pidTerminator{
		signal: func(_ int, sig syscall.Signal) error {
			signals = append(signals, sig)
			return syscall.ESRCH
		},
		gone:        func(_ int) bool { return false },
		waitForExit: func(int, time.Duration, time.Duration, pidGoneFunc) bool { return false },
	}

	if err := terminateByPIDWith(42, terminator); err != nil {
		t.Fatalf("terminateByPIDWith error = %v", err)
	}
	if len(signals) != 1 || signals[0] != syscall.SIGTERM {
		t.Fatalf("signals = %v, want [SIGTERM]", signals)
	}
}

func TestTerminateByPIDReturnsKillError(t *testing.T) {
	killErr := errors.New("kill failed")
	terminator := pidTerminator{
		signal: func(_ int, sig syscall.Signal) error {
			if sig == syscall.SIGKILL {
				return killErr
			}
			return nil
		},
		gone:        func(_ int) bool { return false },
		waitForExit: func(int, time.Duration, time.Duration, pidGoneFunc) bool { return false },
	}

	if err := terminateByPIDWith(42, terminator); !errors.Is(err, killErr) {
		t.Fatalf("terminateByPIDWith error = %v, want %v", err, killErr)
	}
}

func TestWaitHolderDoneReportsClosedChannel(t *testing.T) {
	done := make(chan error)
	close(done)

	if !waitHolderDone(done, time.Second) {
		t.Fatal("waitHolderDone = false, want true for closed channel")
	}
}

func TestWaitHolderDoneTimesOutAndHandlesNil(t *testing.T) {
	if waitHolderDone(nil, time.Nanosecond) {
		t.Fatal("waitHolderDone(nil) = true, want false")
	}
	if waitHolderDone(make(chan error), time.Nanosecond) {
		t.Fatal("waitHolderDone(open channel) = true, want false after timeout")
	}
}

func TestNotifyHolderDoneSendsAndCloses(t *testing.T) {
	done := make(chan error, 1)
	notifyHolderDone(done, errors.New("done"))

	if _, ok := <-done; !ok {
		t.Fatal("done channel closed before value was received")
	}
	if _, ok := <-done; ok {
		t.Fatal("done channel should be closed after notification")
	}
}

func TestNotifyHolderDoneAllowsNilChannel(t *testing.T) {
	notifyHolderDone(nil, nil)
}

func TestNormalizePIDTerminatorFillsDefaults(t *testing.T) {
	terminator := normalizePIDTerminator(pidTerminator{})

	if terminator.signal == nil {
		t.Fatal("expected default signal function")
	}
	if terminator.gone == nil {
		t.Fatal("expected default gone function")
	}
	if terminator.waitForExit == nil {
		t.Fatal("expected default wait function")
	}
	if terminator.gracePeriod != pidTerminationGracePeriod {
		t.Fatalf("gracePeriod = %s, want %s", terminator.gracePeriod, pidTerminationGracePeriod)
	}
	if terminator.pollInterval != pidTerminationPollInterval {
		t.Fatalf("pollInterval = %s, want %s", terminator.pollInterval, pidTerminationPollInterval)
	}
}

func TestWaitForPIDExitChecksImmediately(t *testing.T) {
	calls := 0
	done := waitForPIDExit(42, time.Hour, time.Hour, func(int) bool {
		calls++
		return true
	})

	if !done {
		t.Fatal("waitForPIDExit returned false, want true")
	}
	if calls != 1 {
		t.Fatalf("gone calls = %d, want 1", calls)
	}
}

func TestHolderArgvMatches(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want bool
	}{
		{"self holder", []string{"/usr/bin/micrun", HolderArg}, true},
		{"shim-named self holder", []string{"/usr/bin/containerd-shim-mica-v2", HolderArg}, true},
		{"sleep holder", []string{"/bin/sleep", "infinity"}, true},
		{"tail holder", []string{"/usr/bin/tail", "-f", "/dev/null"}, true},
		{"empty argv", nil, false},
		{"empty argv element", []string{""}, false},
		{"unrelated process", []string{"/usr/bin/sshd", "-D"}, false},
		{"micrun without holder arg", []string{"/usr/bin/micrun", "shim"}, false},
		{"sleep without infinity", []string{"/bin/sleep", "5"}, false},
		{"recycled pid running plain shell", []string{"/bin/bash"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := holderArgvMatches(tc.argv); got != tc.want {
				t.Fatalf("holderArgvMatches(%v) = %v, want %v", tc.argv, got, tc.want)
			}
		})
	}
}

func TestRegisterExistingRejectsForeignProcess(t *testing.T) {
	// The current test process is alive and has a valid /proc/<pid>/ns/net,
	// but its cmdline is not a holder command — registration must refuse so a
	// later Cleanup cannot SIGTERM a recycled/foreign pid.
	if _, err := RegisterExisting("foreign-holder-check", syscall.Getpid()); err == nil {
		t.Fatal("expected RegisterExisting to reject a non-holder process")
	}
}

func TestRegisterExistingRejectsDeadProcess(t *testing.T) {
	if _, err := RegisterExisting("dead-holder-check", 99999); err == nil {
		t.Fatal("expected RegisterExisting to reject a dead pid")
	}
}

// TestTerminateRegisteredHolderSkipsForeignPID verifies PID-reuse safety for
// RegisterExisting holders. The test process is alive but is NOT a netns
// holder. The terminator must detect this via verifyHolderProcess and skip
// termination rather than killing the (recycled) PID. Without the verify
// guard, this test would SIGTERM its own process and fail.
func TestTerminateRegisteredHolderSkipsForeignPID(t *testing.T) {
	terminateRegisteredHolder("reuse-safety-check", syscall.Getpid())
	// Reaching here means the test process was not killed.
}

// TestTerminateRegisteredHolderSkipsZeroPID ensures a zero PID is a safe no-op.
func TestTerminateRegisteredHolderSkipsZeroPID(t *testing.T) {
	terminateRegisteredHolder("zero-pid-check", 0)
}

// TestCleanupRegisteredHolderSkipsForeignPID exercises the Cleanup cmd==nil
// branch with a stale registered holder whose PID has been recycled to a
// non-holder process (here: the test process itself). Cleanup must verify the
// process identity and skip termination instead of killing the innocent PID.
func TestCleanupRegisteredHolderSkipsForeignPID(t *testing.T) {
	withIsolatedHolders(t)

	putHolder("stale-registered", &holder{
		pid: syscall.Getpid(),
		release: func() {
			terminateRegisteredHolder("stale-registered", syscall.Getpid())
		},
	})

	if err := Cleanup("stale-registered", syscall.Getpid()); err != nil {
		t.Fatalf("Cleanup error = %v, want nil", err)
	}
	// Reaching here means the test process was not killed by Cleanup.
}

// A registered (recovered) holder has no watcher; if its PID was recycled by
// an unrelated live process, reusing the entry would hand out that process's
// netns as the sandbox network. The reuse branch must verify identity and
// respawn instead.
func TestCreateRefusesRegisteredHolderWithForeignPID(t *testing.T) {
	resetHoldersForTest(t)
	// Register a "recovered" holder pointing at this test process: alive,
	// but neither an holder-shaped argv nor an isolated netns.
	registerHolderForTest("foreign-pid", 0, os.Getpid())

	created := 0
	_, _, err := createHolderIfAbsent("foreign-pid", func() (*holder, error) {
		created++
		return nil, fmt.Errorf("spawn refused in test")
	})
	if err == nil {
		t.Fatal("expected create error from stub spawn")
	}
	if created != 1 {
		t.Fatalf("create called %d times, want 1 (stale entry must be dropped and respawned)", created)
	}
}

// An argv-shaped but host-namespace process must fail the netns isolation
// check: a real holder runs under CLONE_NEWNET, an innocent "tail -f
// /dev/null" on the host shares our namespace.
func TestVerifyHolderProcessRejectsSameNetnsShape(t *testing.T) {
	if _, err := exec.LookPath("tail"); err != nil {
		t.Skipf("tail not available: %v", err)
	}
	cmd := exec.Command("tail", "-f", "/dev/null")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start shape process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	if err := verifyHolderProcess(cmd.Process.Pid); err == nil {
		t.Fatalf("shape-matching host-netns pid %d accepted as holder", cmd.Process.Pid)
	}
}
