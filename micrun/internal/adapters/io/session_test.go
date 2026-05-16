package io

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"micrun/internal/ports"
	defs "micrun/internal/support/definitions"
)

type recordingFIFOManager struct {
	ensurePaths     []string
	openCalls       []string
	ensureErrByPath map[string]error
	openErrByPath   map[string]error
}

func (m *recordingFIFOManager) EnsureFIFO(path string) error {
	m.ensurePaths = append(m.ensurePaths, path)
	if m.ensureErrByPath == nil {
		return nil
	}
	return m.ensureErrByPath[path]
}

func (m *recordingFIFOManager) OpenFIFO(_ context.Context, path string, _ int) (io.ReadWriteCloser, error) {
	m.openCalls = append(m.openCalls, path)
	if m.openErrByPath == nil {
		return &recordingReadWriteCloser{}, nil
	}
	if err, ok := m.openErrByPath[path]; ok {
		return nil, err
	}
	return &recordingReadWriteCloser{}, nil
}

type recordingReadWriteCloser struct{}

func (r *recordingReadWriteCloser) Read(p []byte) (int, error) {
	return 0, io.EOF
}

func (r *recordingReadWriteCloser) Write(p []byte) (int, error) {
	return len(p), nil
}

func (r *recordingReadWriteCloser) Close() error {
	return nil
}

type closeTrackingTTY struct {
	closeCount int
}

func (t *closeTrackingTTY) Read(p []byte) (int, error) {
	return 0, io.EOF
}

func (t *closeTrackingTTY) Write(p []byte) (int, error) {
	return len(p), nil
}

func (t *closeTrackingTTY) Close() error {
	t.closeCount++
	return nil
}

func TestFIFOOpenErrorIncludesCleanupErrors(t *testing.T) {
	openErr := errors.New("open failed")
	cleanupErr := errors.New("cleanup failed")

	err := fifoOpenError("stdout", openErr, cleanupErr)
	if !errors.Is(err, openErr) {
		t.Fatalf("fifoOpenError = %v, want wrapped open error", err)
	}
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("fifoOpenError = %v, want wrapped cleanup error", err)
	}
}

func TestNormalizeConfigAppliesScalarDefaults(t *testing.T) {
	got := normalizeConfig(Config{ContainerID: "c1"})
	defaults := DefaultConfig()

	if got.StdinBufSize != defaults.StdinBufSize {
		t.Fatalf("StdinBufSize = %d, want %d", got.StdinBufSize, defaults.StdinBufSize)
	}
	if got.StdoutBufSize != defaults.StdoutBufSize {
		t.Fatalf("StdoutBufSize = %d, want %d", got.StdoutBufSize, defaults.StdoutBufSize)
	}
	if got.TTYWriteDelay != defaults.TTYWriteDelay {
		t.Fatalf("TTYWriteDelay = %v, want %v", got.TTYWriteDelay, defaults.TTYWriteDelay)
	}
	if got.TTYWriteLineDelay != defaults.TTYWriteLineDelay {
		t.Fatalf("TTYWriteLineDelay = %v, want %v", got.TTYWriteLineDelay, defaults.TTYWriteLineDelay)
	}
	if got.Terminal {
		t.Fatal("Terminal should preserve the caller-provided false value")
	}
	if got.FilterNUL {
		t.Fatal("FilterNUL should preserve the caller-provided false value")
	}
}

func TestNormalizeConfigPreservesExplicitValues(t *testing.T) {
	config := Config{
		ContainerID:       "c1",
		StdinBufSize:      1,
		StdoutBufSize:     2,
		TTYWriteDelay:     -1,
		TTYWriteLineDelay: -1,
		Terminal:          true,
		FilterNUL:         true,
	}
	got := normalizeConfig(config)

	if got != config {
		t.Fatalf("normalizeConfig changed explicit config:\ngot:  %+v\nwant: %+v", got, config)
	}
}

func TestIsValidFIFOPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "empty", path: "", want: false},
		{name: "whitespace", path: " \t ", want: false},
		{name: "leading whitespace", path: " /run/containerd/io/stdin", want: false},
		{name: "trailing whitespace", path: "/run/containerd/io/stdin ", want: false},
		{name: "binary url", path: "binary://micrun-io", want: false},
		{name: "fd url", path: "fd://3", want: false},
		{name: "socket url", path: "socket:///run/micrun.sock", want: false},
		{name: "absolute file path", path: "/run/containerd/io/stdin", want: true},
		{name: "relative file path", path: "relative/stdin", want: true},
		{name: "similar prefix file path", path: "binary:/stdin", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidFIFOPath(tt.path); got != tt.want {
				t.Fatalf("IsValidFIFOPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestGenerateStandardFIFOPathSanitizesSegments(t *testing.T) {
	got := GenerateStandardFIFOPath("../namespace", `..\container`, "../stdout")
	want := filepath.Join(defs.ContainerdTaskDir, "_", "_", "_")

	if got != want {
		t.Fatalf("GenerateStandardFIFOPath = %q, want %q", got, want)
	}
}

func TestGenerateStandardFIFOPathPreservesSafeSegments(t *testing.T) {
	got := GenerateStandardFIFOPath("namespace", "container", "stdout")
	want := filepath.Join(defs.ContainerdTaskDir, "namespace", "container", "stdout")

	if got != want {
		t.Fatalf("GenerateStandardFIFOPath = %q, want %q", got, want)
	}
}

func TestGenerateStandardFIFOPathUsesPlaceholderForEmptySegments(t *testing.T) {
	got := GenerateStandardFIFOPath(" ", "..", ".")
	want := filepath.Join(defs.ContainerdTaskDir, "_", "_", "_")

	if got != want {
		t.Fatalf("GenerateStandardFIFOPath = %q, want %q", got, want)
	}
}

func TestValidateFreshTTYPairRejectsHalfPair(t *testing.T) {
	stdin := &recordingTTYWriter{}
	stdout := bytes.NewBuffer(nil)

	if err := validateFreshTTYPair(stdin, stdout); err != nil {
		t.Fatalf("full fresh TTY pair returned error: %v", err)
	}
	if err := validateFreshTTYPair(nil, nil); err != nil {
		t.Fatalf("empty fresh TTY pair returned error: %v", err)
	}
	if err := validateFreshTTYPair(stdin, nil); err == nil {
		t.Fatal("expected stdin-only fresh TTY pair to fail")
	}
	if err := validateFreshTTYPair(nil, stdout); err == nil {
		t.Fatal("expected stdout-only fresh TTY pair to fail")
	}
}

func TestValidateFreshTTYPairTreatsTypedNilPairAsAbsent(t *testing.T) {
	var stdin *os.File
	var stdout *os.File

	if err := validateFreshTTYPair(stdin, stdout); err != nil {
		t.Fatalf("typed nil fresh TTY pair returned error: %v", err)
	}
	if set, err := newFreshTTYSet(stdin, stdout); err != nil || set.provided {
		t.Fatalf("newFreshTTYSet typed nil = (%+v, %v), want absent pair", set, err)
	}
}

func TestFreshTTYSetReturnsFreshPairWithoutMutatingSession(t *testing.T) {
	oldIn := &recordingTTYWriter{}
	oldOut := bytes.NewBuffer(nil)
	freshIn := &recordingTTYWriter{}
	freshOut := bytes.NewBuffer(nil)

	session, err := NewSession(Config{
		ContainerID: "c1",
		TTYIn:       oldIn,
		TTYOut:      oldOut,
		TTYErr:      oldOut,
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}

	freshTTYs, err := newFreshTTYSet(freshIn, freshOut)
	if err != nil {
		t.Fatalf("newFreshTTYSet returned error: %v", err)
	}
	ttys := freshTTYs.resolve(session.ttys)
	if ttys.stdin != freshIn {
		t.Fatalf("ttyIn = %p, want %p", ttys.stdin, freshIn)
	}
	if ttys.stdout != freshOut {
		t.Fatalf("ttyOut = %p, want %p", ttys.stdout, freshOut)
	}
	if ttys.stderr != freshOut {
		t.Fatalf("ttyErr = %p, want %p", ttys.stderr, freshOut)
	}
	if session.ttys.stdin != oldIn || session.ttys.stdout != oldOut || session.ttys.stderr != oldOut {
		t.Fatal("session config should not update before restart commits the fresh TTY pair")
	}
}

func TestFreshTTYSetKeepsConfiguredPairWhenFreshPairMissing(t *testing.T) {
	oldIn := &recordingTTYWriter{}
	oldOut := bytes.NewBuffer(nil)

	session, err := NewSession(Config{
		ContainerID: "c1",
		TTYIn:       oldIn,
		TTYOut:      oldOut,
		TTYErr:      oldOut,
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}

	ttys := freshTTYSet{}.resolve(session.ttys)
	if ttys.stdin != oldIn || ttys.stdout != oldOut || ttys.stderr != oldOut {
		t.Fatal("expected configured TTY handles when no fresh pair is provided")
	}
}

func TestSetTTYsUpdatesAllStreams(t *testing.T) {
	session, err := NewSession(Config{ContainerID: "c1"})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	stdin := &recordingTTYWriter{}
	stdout := bytes.NewBuffer(nil)
	stderr := bytes.NewBuffer(nil)

	session.setTTYs(sessionTTYs{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	})

	if session.ttys.stdin != stdin || session.ttys.stdout != stdout || session.ttys.stderr != stderr {
		t.Fatal("session config did not store the provided TTY handles")
	}
}

func TestOpenFIFOsSkipsNonFilePaths(t *testing.T) {
	session, err := NewSession(Config{
		ContainerID: "c1",
		StdinFIFO:   "binary://stdin",
		StdoutFIFO:  "fd://1",
		StderrFIFO:  "socket:///run/micrun/stderr.sock",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	t.Cleanup(session.copier.Stop)

	streams, err := session.openFIFOs()
	if err != nil {
		t.Fatalf("openFIFOs returned error: %v", err)
	}
	if streams.stdin != nil || streams.stdout != nil || streams.stderr != nil {
		t.Fatalf("expected nil streams for non-file paths, got stdin=%T stdout=%T stderr=%T", streams.stdin, streams.stdout, streams.stderr)
	}
}

func TestEnsureFIFOsDelegatesToFIFOManager(t *testing.T) {
	manager := &recordingFIFOManager{}
	session, err := NewSession(Config{
		ContainerID: "c1",
		StdinFIFO:   "stdin",
		StdoutFIFO:  "stdout",
		StderrFIFO:  "stderr",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session.fifoOps = manager

	if err := session.ensureFIFOs(); err != nil {
		t.Fatalf("ensureFIFOs returned error: %v", err)
	}

	wantPaths := []string{"stdin", "stdout", "stderr"}
	if len(manager.ensurePaths) != len(wantPaths) {
		t.Fatalf("EnsureFIFO called %d times, want %d", len(manager.ensurePaths), len(wantPaths))
	}
	for i, want := range wantPaths {
		if manager.ensurePaths[i] != want {
			t.Fatalf("ensureFIFOs path[%d] = %s, want %s", i, manager.ensurePaths[i], want)
		}
	}
}

func TestEnsureFIFOsSkipsNonFilePaths(t *testing.T) {
	manager := &recordingFIFOManager{}
	session, err := NewSession(Config{
		ContainerID: "c1",
		StdinFIFO:   "binary://stdin",
		StdoutFIFO:  "fd://1",
		StderrFIFO:  "socket:///run/micrun/stderr.sock",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session.fifoOps = manager

	if err := session.ensureFIFOs(); err != nil {
		t.Fatalf("ensureFIFOs returned error: %v", err)
	}
	if len(manager.ensurePaths) != 0 {
		t.Fatalf("EnsureFIFO called for non-file paths: %v", manager.ensurePaths)
	}
}

func TestEnsureFIFOsDeduplicatesPaths(t *testing.T) {
	manager := &recordingFIFOManager{}
	session, err := NewSession(Config{
		ContainerID: "c1",
		StdinFIFO:   "shared",
		StdoutFIFO:  "shared",
		StderrFIFO:  "stderr",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session.fifoOps = manager

	if err := session.ensureFIFOs(); err != nil {
		t.Fatalf("ensureFIFOs returned error: %v", err)
	}

	wantPaths := []string{"shared", "stderr"}
	if len(manager.ensurePaths) != len(wantPaths) {
		t.Fatalf("EnsureFIFO called %d times, want %d: %v", len(manager.ensurePaths), len(wantPaths), manager.ensurePaths)
	}
	for i, want := range wantPaths {
		if manager.ensurePaths[i] != want {
			t.Fatalf("ensureFIFOs path[%d] = %s, want %s", i, manager.ensurePaths[i], want)
		}
	}
}

func TestOpenFIFOsDelegatesToFIFOManager(t *testing.T) {
	manager := &recordingFIFOManager{}
	session, err := NewSession(Config{
		ContainerID: "c1",
		StdinFIFO:   "stdin",
		StdoutFIFO:  "stdout",
		StderrFIFO:  "stderr",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session.fifoOps = manager

	streams, err := session.openFIFOs()
	if err != nil {
		t.Fatalf("openFIFOs returned error: %v", err)
	}
	t.Cleanup(func() {
		streams.closeAll()
	})

	wantPaths := []string{"stdin", "stdout", "stderr"}
	if len(manager.openCalls) != len(wantPaths) {
		t.Fatalf("OpenFIFO called %d times, want %d", len(manager.openCalls), len(wantPaths))
	}
	for i, want := range wantPaths {
		if manager.openCalls[i] != want {
			t.Fatalf("openFIFO path[%d] = %s, want %s", i, manager.openCalls[i], want)
		}
	}
}

func TestOpenFIFOsReturnsManagerOpenError(t *testing.T) {
	managerOpenErr := errors.New("open failed")
	session, err := NewSession(Config{
		ContainerID: "c1",
		StdinFIFO:   "stdin",
		StdoutFIFO:  "stdout",
		StderrFIFO:  "stderr",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session.fifoOps = &recordingFIFOManager{
		openErrByPath: map[string]error{
			"stdout": managerOpenErr,
		},
	}

	_, err = session.openFIFOs()
	if !errors.Is(err, managerOpenErr) {
		t.Fatalf("openFIFOs error = %v, want contains %v", err, managerOpenErr)
	}
}

func TestEnsureFIFOsReturnsManagerError(t *testing.T) {
	managerErr := errors.New("ensure failed")
	session, err := NewSession(Config{
		ContainerID: "c1",
		StdinFIFO:   "stdin",
		StdoutFIFO:  "stdout",
		StderrFIFO:  "stderr",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session.fifoOps = &recordingFIFOManager{
		ensureErrByPath: map[string]error{
			"stdout": managerErr,
		},
	}

	if err = session.ensureFIFOs(); !errors.Is(err, managerErr) {
		t.Fatalf("ensureFIFOs error = %v, want contains %v", err, managerErr)
	}
}

func TestSessionFIFOStreamsCloseAllReturnsAllErrors(t *testing.T) {
	stdinErr := errors.New("stdin close failed")
	stdoutErr := errors.New("stdout close failed")
	stderrErr := errors.New("stderr close failed")
	streams := sessionFIFOStreams{
		stdin:  failingReadCloser{err: stdinErr},
		stdout: failingWriteCloser{err: stdoutErr},
		stderr: failingWriteCloser{err: stderrErr},
	}

	err := errors.Join(streams.closeAll()...)
	for _, want := range []error{stdinErr, stdoutErr, stderrErr} {
		if !errors.Is(err, want) {
			t.Fatalf("closeAll error = %v, want wrapped %v", err, want)
		}
	}
	if streams.stdin != nil || streams.stdout != nil || streams.stderr != nil {
		t.Fatal("closeAll should clear stream references")
	}
}

func TestSessionFIFOStreamsCloseAllIgnoresTypedNilClosers(t *testing.T) {
	var nilReadCloser *os.File
	var nilWriteCloser *os.File
	streams := sessionFIFOStreams{
		stdin:  nilReadCloser,
		stdout: nilWriteCloser,
		stderr: nilWriteCloser,
	}

	if err := errors.Join(streams.closeAll()...); err != nil {
		t.Fatalf("closeAll error = %v, want nil", err)
	}
	if streams.stdin != nil || streams.stdout != nil || streams.stderr != nil {
		t.Fatal("closeAll should clear typed nil stream references")
	}
}

func TestNewSessionDerivesLifecycleFromConfigContext(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	session, err := NewSession(Config{
		Context:     parent,
		ContainerID: "c1",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	t.Cleanup(session.copier.Stop)

	if session.config.Context != parent {
		t.Fatal("session config should preserve the caller-provided parent context")
	}
	if session.copier.config.Context != session.ctx {
		t.Fatal("copier config should use the active session context")
	}

	cancel()

	assertContextDone(t, session.ctx)
	assertContextDone(t, session.copier.ctx)
}

func TestRestartUsesFreshChildContextFromOriginalParent(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()

	session, err := NewSession(Config{
		Context:     parent,
		ContainerID: "c1",
		StdinFIFO:   "binary://stdin",
		StdoutFIFO:  "fd://1",
		StderrFIFO:  "socket:///run/micrun/stderr.sock",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}

	if err := session.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	previousCtx := session.ctx
	session.Stop()
	assertContextDone(t, previousCtx)

	if err := session.Restart(); err != nil {
		t.Fatalf("Restart returned error: %v", err)
	}
	t.Cleanup(session.copier.Stop)

	if err := session.ctx.Err(); err != nil {
		t.Fatalf("restarted session context err = %v, want nil", err)
	}
	if err := session.copier.ctx.Err(); err != nil {
		t.Fatalf("restarted copier context err = %v, want nil", err)
	}
}

func TestRestartRejectsCanceledParentContext(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	session, err := NewSession(Config{
		Context:     parent,
		ContainerID: "c1",
		StdinFIFO:   "binary://stdin",
		StdoutFIFO:  "fd://1",
		StderrFIFO:  "socket:///run/micrun/stderr.sock",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	cancel()

	if err := session.Restart(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Restart error = %v, want context.Canceled", err)
	}
	if session.started {
		t.Fatal("Restart should not mark session started after parent cancellation")
	}
}

func TestRestartEnsuresFIFOsBeforeOpening(t *testing.T) {
	manager := &recordingFIFOManager{}
	session, err := NewSession(Config{
		ContainerID: "c1",
		StdinFIFO:   "stdin",
		StdoutFIFO:  "stdout",
		StderrFIFO:  "stderr",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session.fifoOps = manager

	if err := session.Restart(); err != nil {
		t.Fatalf("Restart returned error: %v", err)
	}
	t.Cleanup(session.Stop)

	wantPaths := []string{"stdin", "stdout", "stderr"}
	if len(manager.ensurePaths) != len(wantPaths) {
		t.Fatalf("Restart EnsureFIFO calls = %v, want %v", manager.ensurePaths, wantPaths)
	}
	for i, want := range wantPaths {
		if manager.ensurePaths[i] != want {
			t.Fatalf("Restart EnsureFIFO[%d] = %q, want %q", i, manager.ensurePaths[i], want)
		}
	}
	if len(manager.openCalls) != len(wantPaths) {
		t.Fatalf("Restart OpenFIFO calls = %v, want %v", manager.openCalls, wantPaths)
	}
}

func TestRestartWithTTYsCommitsFreshHandlesAfterStart(t *testing.T) {
	oldIn := &recordingTTYWriter{}
	oldOut := bytes.NewBuffer(nil)
	freshIn := &recordingTTYWriter{}
	freshOut := bytes.NewBuffer(nil)
	session, err := NewSession(Config{
		ContainerID: "c1",
		StdinFIFO:   "binary://stdin",
		StdoutFIFO:  "fd://1",
		StderrFIFO:  "fd://1",
		TTYIn:       oldIn,
		TTYOut:      oldOut,
		TTYErr:      oldOut,
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}

	if err := session.RestartWithTTYs(freshIn, freshOut); err != nil {
		t.Fatalf("RestartWithTTYs returned error: %v", err)
	}
	t.Cleanup(session.Stop)

	if session.ttys.stdin != freshIn || session.ttys.stdout != freshOut || session.ttys.stderr != freshOut {
		t.Fatal("session config did not commit fresh TTY handles after restart")
	}
	if session.copier.ttyIn != freshIn || session.copier.ttyOut != freshOut || session.copier.ttyErr != freshOut {
		t.Fatal("copier did not use committed fresh TTY handles")
	}
}

func TestSessionEventStreamAfterRestartUsesRenewedBus(t *testing.T) {
	session, err := NewSession(Config{
		ContainerID: "restart-events",
		StdinFIFO:   "binary://stdin",
		StdoutFIFO:  "fd://1",
		StderrFIFO:  "fd://2",
		TTYIn:       &recordingTTYWriter{},
		TTYOut:      bytes.NewBuffer(nil),
		TTYErr:      bytes.NewBuffer(nil),
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	if err := session.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	session.StopWithoutClosingFIFOs()

	if err := session.Restart(); err != nil {
		t.Fatalf("Restart returned error: %v", err)
	}
	t.Cleanup(session.Stop)

	events := session.EventStream().SubscribeMany(ports.IOEventTTYReady)
	session.eventBus.Publish(Event{Type: TTYReady, ContainerID: "restart-events"})

	got := receiveIOEvent(t, events)
	if got.Type != ports.IOEventTTYReady || got.ContainerID != "restart-events" {
		t.Fatalf("forwarded event = %+v, want TTY ready for restart-events", got)
	}
}

func TestRestartWithTTYsTreatsTypedNilPairAsNoFreshTTY(t *testing.T) {
	oldIn := &recordingTTYWriter{}
	oldOut := bytes.NewBuffer(nil)
	var freshIn *os.File
	var freshOut *os.File
	session, err := NewSession(Config{
		ContainerID: "typed-nil-fresh-tty",
		StdinFIFO:   "binary://stdin",
		StdoutFIFO:  "fd://1",
		StderrFIFO:  "fd://1",
		TTYIn:       oldIn,
		TTYOut:      oldOut,
		TTYErr:      oldOut,
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}

	if err := session.RestartWithTTYs(freshIn, freshOut); err != nil {
		t.Fatalf("RestartWithTTYs returned error: %v", err)
	}
	t.Cleanup(session.Stop)

	if session.ttys.stdin != oldIn || session.ttys.stdout != oldOut || session.ttys.stderr != oldOut {
		t.Fatal("typed nil fresh TTY pair should keep configured handles")
	}
	if session.copier.ttyIn != oldIn || session.copier.ttyOut != oldOut || session.copier.ttyErr != oldOut {
		t.Fatal("copier should use configured handles for typed nil fresh TTY pair")
	}
}

func TestRestartReturnsEnsureFIFOErrorBeforeOpening(t *testing.T) {
	managerErr := errors.New("ensure failed")
	manager := &recordingFIFOManager{
		ensureErrByPath: map[string]error{
			"stdout": managerErr,
		},
	}
	session, err := NewSession(Config{
		ContainerID: "c1",
		StdinFIFO:   "stdin",
		StdoutFIFO:  "stdout",
		StderrFIFO:  "stderr",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session.fifoOps = manager

	if err := session.Restart(); !errors.Is(err, managerErr) {
		t.Fatalf("Restart error = %v, want %v", err, managerErr)
	}
	if len(manager.openCalls) != 0 {
		t.Fatalf("Restart should not open FIFOs after ensure failure, got %v", manager.openCalls)
	}
}

func TestRestartEnsureFailureDoesNotAllocateLifecycleResources(t *testing.T) {
	managerErr := errors.New("ensure failed")
	session, err := NewSession(Config{
		ContainerID: "c1",
		StdinFIFO:   "stdin",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session.fifoOps = &recordingFIFOManager{
		ensureErrByPath: map[string]error{
			"stdin": managerErr,
		},
	}
	session.Stop()
	stoppedCtx := session.ctx

	if err := session.Restart(); !errors.Is(err, managerErr) {
		t.Fatalf("Restart error = %v, want %v", err, managerErr)
	}
	if session.eventBus != nil {
		t.Fatal("Restart ensure failure should not allocate a new event bus")
	}
	if session.ctx != stoppedCtx {
		t.Fatal("Restart ensure failure should not recycle session context")
	}
}

func TestSessionStopClosesStreamsAndCancelsContext(t *testing.T) {
	session, err := NewSession(Config{ContainerID: "c1"})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session.started = true
	session.copier.SetStdin(failingReadCloser{})
	session.copier.SetStdout(failingWriteCloser{})
	session.copier.SetStderr(failingWriteCloser{})
	previousCtx := session.ctx

	session.Stop()

	if session.started {
		t.Fatal("session should not remain started after Stop")
	}
	assertContextDone(t, previousCtx)
	if session.copier.stdinFifo != nil || session.copier.stdoutFIFO != nil || session.copier.stderrFIFO != nil {
		t.Fatal("Stop should close and clear FIFO streams")
	}
}

func TestSessionStopWithoutClosingFIFOsPreservesStreamsAndCancelsContext(t *testing.T) {
	session, err := NewSession(Config{ContainerID: "c1"})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session.started = true
	session.copier.SetStdin(failingReadCloser{})
	session.copier.SetStdout(failingWriteCloser{})
	session.copier.SetStderr(failingWriteCloser{})
	previousCtx := session.ctx

	session.StopWithoutClosingFIFOs()

	if session.started {
		t.Fatal("session should not remain started after StopWithoutClosingFIFOs")
	}
	assertContextDone(t, previousCtx)
	if session.copier.stdinFifo == nil || session.copier.stdoutFIFO == nil || session.copier.stderrFIFO == nil {
		t.Fatal("StopWithoutClosingFIFOs should preserve FIFO streams")
	}
	if !session.preservedStreams {
		t.Fatal("StopWithoutClosingFIFOs should mark streams as preserved")
	}
}

func TestSessionStopClosesStreamsPreservedByDetach(t *testing.T) {
	session, err := NewSession(Config{ContainerID: "c1"})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session.started = true
	session.copier.SetStdin(failingReadCloser{})
	session.copier.SetStdout(failingWriteCloser{})
	session.copier.SetStderr(failingWriteCloser{})

	session.StopWithoutClosingFIFOs()
	session.Stop()

	if session.preservedStreams {
		t.Fatal("final Stop should clear preserved stream state")
	}
	if session.copier.stdinFifo != nil || session.copier.stdoutFIFO != nil || session.copier.stderrFIFO != nil {
		t.Fatal("final Stop should close streams preserved by detach")
	}
}

func TestSessionRestartClosesSupersededPreservedStreams(t *testing.T) {
	session, err := NewSession(Config{
		ContainerID: "c1",
		StdinFIFO:   "binary://stdin",
		StdoutFIFO:  "fd://1",
		StderrFIFO:  "socket:///run/micrun/stderr.sock",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session.started = true
	session.copier.SetStdin(failingReadCloser{})
	session.copier.SetStdout(failingWriteCloser{})
	session.copier.SetStderr(failingWriteCloser{})
	oldCopier := session.copier

	session.StopWithoutClosingFIFOs()
	if err := session.Restart(); err != nil {
		t.Fatalf("Restart returned error: %v", err)
	}
	t.Cleanup(session.Stop)

	if session.preservedStreams {
		t.Fatal("Restart should clear preserved stream state after replacing copier")
	}
	if oldCopier.stdinFifo != nil || oldCopier.stdoutFIFO != nil || oldCopier.stderrFIFO != nil {
		t.Fatal("Restart should close streams preserved by the superseded copier")
	}
}

func TestSessionRestartKeepsReusedTTYHandlesOpen(t *testing.T) {
	ttyIn := &closeTrackingTTY{}
	ttyOut := &closeTrackingTTY{}
	session, err := NewSession(Config{
		ContainerID: "c1",
		StdinFIFO:   "binary://stdin",
		StdoutFIFO:  "fd://1",
		StderrFIFO:  "fd://1",
		TTYIn:       ttyIn,
		TTYOut:      ttyOut,
		TTYErr:      ttyOut,
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	session.started = true
	session.copier.SetStdin(failingReadCloser{})
	session.copier.SetStdout(failingWriteCloser{})
	session.copier.SetStderr(failingWriteCloser{})
	oldCopier := session.copier

	session.StopWithoutClosingFIFOs()
	if err := session.Restart(); err != nil {
		t.Fatalf("Restart returned error: %v", err)
	}
	t.Cleanup(session.Stop)

	if oldCopier.stdinFifo != nil || oldCopier.stdoutFIFO != nil || oldCopier.stderrFIFO != nil {
		t.Fatal("Restart should close superseded FIFO streams")
	}
	if ttyIn.closeCount != 0 {
		t.Fatalf("reused TTY stdin closeCount = %d, want 0", ttyIn.closeCount)
	}
	if ttyOut.closeCount != 0 {
		t.Fatalf("reused TTY output closeCount = %d, want 0", ttyOut.closeCount)
	}
}

func TestSameIOHandleRejectsTypedNil(t *testing.T) {
	var left *os.File
	var right *os.File

	if sameIOHandle(left, right) {
		t.Fatal("typed nil handles should not be considered the same live handle")
	}
}

func TestSessionStopClosesEventStream(t *testing.T) {
	session, err := NewSession(Config{ContainerID: "c1"})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	stream := session.eventStream()
	events := stream.SubscribeMany(ports.IOEventExitCommand)
	session.started = true

	session.Stop()

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected event stream to close after session stop")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event stream close")
	}
}

func TestSessionStopWithoutStartClosesContextAndStream(t *testing.T) {
	session, err := NewSession(Config{ContainerID: "c1"})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}
	previousCtx := session.ctx
	stream := session.eventStream()
	events := stream.SubscribeMany(ports.IOEventExitCommand)

	session.Stop()

	assertContextDone(t, previousCtx)
	if session.started {
		t.Fatal("session should not remain started after Stop when not started")
	}

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected event stream to close after session stop")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event stream close")
	}
}

func TestSessionStartAfterStopRefreshesContext(t *testing.T) {
	session, err := NewSession(Config{
		ContainerID: "c1",
		StdinFIFO:   "binary://stdin",
		StdoutFIFO:  "fd://1",
		StderrFIFO:  "socket:///run/micrun/stderr.sock",
	})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}

	previousCtx := session.ctx
	session.Stop()
	assertContextDone(t, previousCtx)

	if err := session.Start(); err != nil {
		t.Fatalf("Start after stop returned error: %v", err)
	}
	session.Stop()

	if session.ctx == previousCtx {
		t.Fatalf("session context should be refreshed after stop")
	}
}

func TestSessionRenewContextRecyclesLifecycle(t *testing.T) {
	session, err := NewSession(Config{ContainerID: "c1"})
	if err != nil {
		t.Fatalf("NewSession returned error: %v", err)
	}

	oldCtx := session.ctx
	oldStream := session.eventStream()
	oldEvents := oldStream.SubscribeMany(ports.IOEventExitCommand)
	session.renewContext()
	if oldCtx == session.ctx {
		t.Fatal("renewContext should create a new session context")
	}

	assertContextDone(t, oldCtx)
	select {
	case _, ok := <-oldEvents:
		if ok {
			t.Fatal("expected previous event stream to close after context recycle")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for previous event stream close")
	}
}

func assertContextDone(t *testing.T, ctx context.Context) {
	t.Helper()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for context cancellation")
	}
}
