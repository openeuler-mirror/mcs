package netns

import (
	"errors"
	"syscall"
	"testing"
	"time"
)

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

	existing := &holder{pid: 7}
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

func TestCreateHolderIfAbsentReleasesUnusedCreatedHolder(t *testing.T) {
	withIsolatedHolders(t)

	released := false
	existing := &holder{pid: 7}
	proposed := &holder{
		pid: 8,
		release: func() {
			released = true
		},
	}
	got, created, err := createHolderIfAbsent("sandbox", func() (*holder, error) {
		putHolder("sandbox", existing)
		return proposed, nil
	})

	if err != nil {
		t.Fatalf("createHolderIfAbsent error = %v", err)
	}
	if created {
		t.Fatal("expected raced holder to be reused")
	}
	if got != existing {
		t.Fatal("expected existing holder to win race")
	}
	if !released {
		t.Fatal("unused proposed holder was not released")
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
