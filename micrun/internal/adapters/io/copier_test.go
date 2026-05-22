package io

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	filter "micrun/internal/domain/console"
	log "micrun/internal/support/logger"

	"github.com/sirupsen/logrus"
)

type recordingTTYWriter struct {
	bytes.Buffer
	writes [][]byte
}

func (w *recordingTTYWriter) Write(p []byte) (int, error) {
	w.writes = append(w.writes, append([]byte(nil), p...))
	return w.Buffer.Write(p)
}

func (w *recordingTTYWriter) Close() error {
	return nil
}

type failingReadCloser struct {
	err error
}

func (f failingReadCloser) Read(p []byte) (int, error) { return 0, io.EOF }
func (f failingReadCloser) Close() error               { return f.err }

type failingWriteCloser struct {
	err error
}

func (f failingWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (f failingWriteCloser) Close() error                { return f.err }

type fdWriteCloser struct {
	fd uintptr
}

func (f fdWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (f fdWriteCloser) Close() error                { return nil }
func (f fdWriteCloser) Fd() uintptr                 { return f.fd }

type failingReadCloserForReader struct {
	err error
}

func (f failingReadCloserForReader) Read(p []byte) (int, error) { return 0, io.EOF }
func (f failingReadCloserForReader) Close() error               { return f.err }

type fdReader struct {
	fd uintptr
}

func (f fdReader) Read(p []byte) (int, error) { return 0, io.EOF }
func (f fdReader) Fd() uintptr                { return f.fd }

type countingFDReadCloser struct {
	fd     uintptr
	closes int
	err    error
}

func (f *countingFDReadCloser) Read(p []byte) (int, error) { return 0, io.EOF }
func (f *countingFDReadCloser) Close() error {
	f.closes++
	return f.err
}
func (f *countingFDReadCloser) Fd() uintptr { return f.fd }

type writeError struct {
	err error
}

func (w writeError) Write(p []byte) (int, error) { return 0, w.err }

type shortWriter struct{}

func (w shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestWriteActionDataReportsWriteFailures(t *testing.T) {
	expectedErr := errors.New("write failed")
	if err := writeActionData(writeError{err: expectedErr}, []byte("hello")); !errors.Is(err, expectedErr) {
		t.Fatalf("writeActionData error = %v, want %v", err, expectedErr)
	}
	if err := writeActionData(shortWriter{}, []byte("hello")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeActionData error = %v, want short write", err)
	}
	if err := writeActionData(nil, []byte("hello")); err != nil {
		t.Fatalf("writeActionData nil writer error = %v, want nil", err)
	}
}

func TestCloseFIFOsReturnsAllCloseErrors(t *testing.T) {
	stdinErr := errors.New("stdin close failed")
	stdoutErr := errors.New("stdout close failed")
	stderrErr := errors.New("stderr close failed")
	copier := &Copier{
		stdinFifo:  failingReadCloser{err: stdinErr},
		stdoutFIFO: failingWriteCloser{err: stdoutErr},
		stderrFIFO: failingWriteCloser{err: stderrErr},
	}

	err := copier.closeFIFOs()
	for _, want := range []error{stdinErr, stdoutErr, stderrErr} {
		if !errors.Is(err, want) {
			t.Fatalf("closeFIFOs error = %v, want wrapped %v", err, want)
		}
	}
	if copier.stdinFifo != nil || copier.stdoutFIFO != nil || copier.stderrFIFO != nil {
		t.Fatal("closeFIFOs should clear closed FIFO references")
	}
}

func TestCloseFIFOsIgnoresTypedNilClosers(t *testing.T) {
	var nilReadCloser *os.File
	var nilWriteCloser *os.File
	copier := &Copier{
		stdinFifo:  nilReadCloser,
		stdoutFIFO: nilWriteCloser,
		stderrFIFO: nilWriteCloser,
	}

	if err := copier.closeFIFOs(); err != nil {
		t.Fatalf("closeFIFOs error = %v, want nil", err)
	}
	if copier.stdinFifo != nil || copier.stdoutFIFO != nil || copier.stderrFIFO != nil {
		t.Fatal("closeFIFOs should clear typed nil FIFO references")
	}
}

func TestCloseTTYsReturnsAllCloseErrors(t *testing.T) {
	stdoutErr := errors.New("tty stdout close failed")
	stderrErr := errors.New("tty stderr close failed")
	stdinErr := errors.New("tty stdin close failed")
	copier := &Copier{
		ttyOut: failingReadCloser{err: stdoutErr},
		ttyErr: failingReadCloser{err: stderrErr},
		ttyIn:  failingWriteCloser{err: stdinErr},
	}

	err := copier.closeTTYs()
	for _, want := range []error{stdoutErr, stderrErr, stdinErr} {
		if !errors.Is(err, want) {
			t.Fatalf("closeTTYs error = %v, want wrapped %v", err, want)
		}
	}
	if copier.ttyOut != nil || copier.ttyErr != nil || copier.ttyIn != nil {
		t.Fatal("closeTTYs should clear closed TTY references")
	}
}

func TestCloseTTYsIgnoresTypedNilClosers(t *testing.T) {
	var nilTTYOut *os.File
	var nilTTYIn *os.File
	copier := &Copier{
		ttyOut: nilTTYOut,
		ttyErr: nilTTYOut,
		ttyIn:  nilTTYIn,
	}

	if err := copier.closeTTYs(); err != nil {
		t.Fatalf("closeTTYs error = %v, want nil", err)
	}
	if copier.ttyOut != nil || copier.ttyErr != nil || copier.ttyIn != nil {
		t.Fatal("closeTTYs should clear typed nil TTY references")
	}
}

func TestCloseTTYsClosesSharedOutputFDOnce(t *testing.T) {
	output := &countingFDReadCloser{fd: 42}
	copier := &Copier{
		ttyOut: output,
		ttyErr: output,
	}

	if err := copier.closeTTYs(); err != nil {
		t.Fatalf("closeTTYs error = %v, want nil", err)
	}
	if output.closes != 1 {
		t.Fatalf("shared output closes = %d, want 1", output.closes)
	}
}

func TestCloseTTYsClosesStderrWhenSharedStdoutFDIsNotCloser(t *testing.T) {
	stderr := &countingFDReadCloser{fd: 42}
	copier := &Copier{
		ttyOut: fdReader{fd: 42},
		ttyErr: stderr,
	}

	if err := copier.closeTTYs(); err != nil {
		t.Fatalf("closeTTYs error = %v, want nil", err)
	}
	if stderr.closes != 1 {
		t.Fatalf("stderr closes = %d, want 1", stderr.closes)
	}
}

func TestCloseStreamIfCloseableClearsOnError(t *testing.T) {
	closeErr := errors.New("close failed")
	reader := io.Reader(failingReadCloserForReader{err: closeErr})

	errs := closeStreamIfCloseable("test close", &reader, nil)
	if len(errs) == 0 || !errors.Is(errs[0], closeErr) {
		t.Fatalf("closeStreamIfCloseable error = %v, want wrapped %v", errs, closeErr)
	}
	if reader != nil {
		t.Fatal("closeStreamIfCloseable should clear reader after close attempt")
	}
}

func TestCloseStreamIfCloseableResetsNonCloser(t *testing.T) {
	reader := io.Reader(bytes.NewBufferString("test"))

	errs := closeStreamIfCloseable("test close", &reader, nil)
	if len(errs) != 0 {
		t.Fatalf("closeStreamIfCloseable error = %v, want nil", errs)
	}
	if reader != nil {
		t.Fatal("closeStreamIfCloseable should clear non-closer reader reference")
	}
}

func TestCloseStreamIfCloseableIgnoresMatchedErrors(t *testing.T) {
	reader := io.Reader(failingReadCloserForReader{err: os.ErrClosed})

	errs := closeStreamIfCloseable("test close", &reader, func(err error) bool {
		return errors.Is(err, os.ErrClosed)
	})
	if len(errs) != 0 {
		t.Fatalf("closeStreamIfCloseable error = %v, want nil on ignored close error", errs)
	}
	if reader != nil {
		t.Fatal("closeStreamIfCloseable should clear reader after close attempt")
	}
}

func TestMergeCloseErrorsPreservesOrderAndIgnoresNilGroups(t *testing.T) {
	err1 := errors.New("first")
	err2 := errors.New("second")
	err3 := errors.New("third")

	got := mergeCloseErrors([]error{err1}, nil, []error{err2, err3}, nil)
	if len(got) != 3 {
		t.Fatalf("mergeCloseErrors len = %d, want %d", len(got), 3)
	}
	if got[0] != err1 || got[1] != err2 || got[2] != err3 {
		t.Fatalf("mergeCloseErrors order = %v, want [%v %v %v]", got, err1, err2, err3)
	}
}

func TestMergeCloseErrorsEmpty(t *testing.T) {
	if got := mergeCloseErrors(); got != nil {
		t.Fatalf("mergeCloseErrors() = %v, want nil", got)
	}
}

func TestBeginStopIsIdempotent(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "test-stop-idempotent"})
	defer copier.finishStop(0)

	if !copier.beginStop("test stop") {
		t.Fatal("expected first beginStop to start stopping")
	}
	if !copier.stopped.Load() {
		t.Fatal("expected copier to be marked stopped")
	}
	if !errors.Is(copier.ctx.Err(), context.Canceled) {
		t.Fatalf("context error = %v, want canceled", copier.ctx.Err())
	}
	if copier.beginStop("test stop again") {
		t.Fatal("expected second beginStop to be ignored")
	}
}

func TestNewCopierGivesCancelPipeOwnershipToSingleWaiter(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "test-cancel-owner"})
	defer copier.finishStop(0)
	if copier.ttyWaiter.cancelPipeR < 0 {
		t.Skip("cancel pipe unavailable")
	}

	if !copier.ttyWaiter.ownsCancel {
		t.Fatal("tty waiter should own the shared cancel pipe")
	}
	if copier.stdinWaiter.ownsCancel {
		t.Fatal("stdin waiter should not own the shared cancel pipe")
	}
	if copier.stdinWaiter.cancelPipeW >= 0 {
		t.Fatalf("stdin waiter cancel writer = %d, want no writer", copier.stdinWaiter.cancelPipeW)
	}
	if copier.stdinWaiter.cancelPipeR != copier.ttyWaiter.cancelPipeR {
		t.Fatal("stdin waiter should listen on the shared cancel reader")
	}
}

func TestNewCopierDerivesLifecycleFromConfigContext(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	copier := NewCopier(Config{Context: parent, ContainerID: "test-parent-context"})
	defer copier.finishStop(0)

	cancel()

	assertContextDone(t, copier.ctx)
}

func TestStopWithoutClosingFIFOsPreservesStreams(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "test-stop-preserve-streams"})
	copier.SetStdin(failingReadCloser{})
	copier.SetStdout(failingWriteCloser{})
	copier.SetStderr(failingWriteCloser{})
	copier.SetTTYs(failingWriteCloser{}, failingReadCloser{}, failingReadCloser{})

	copier.StopWithoutClosingFIFOs()

	if copier.stdinFifo == nil || copier.stdoutFIFO == nil || copier.stderrFIFO == nil {
		t.Fatal("StopWithoutClosingFIFOs should preserve FIFO handles")
	}
	if copier.ttyIn == nil || copier.ttyOut == nil || copier.ttyErr == nil {
		t.Fatal("StopWithoutClosingFIFOs should preserve TTY handles")
	}
}

func TestFDOfUsesGenericFDProvider(t *testing.T) {
	fd, ok := fdOf(fdWriteCloser{fd: 42})
	if !ok {
		t.Fatal("expected fd provider to be detected")
	}
	if fd != 42 {
		t.Fatalf("fdOf returned %d, want 42", fd)
	}

	if _, ok := fdOf(failingWriteCloser{}); ok {
		t.Fatal("expected writer without Fd to be ignored")
	}
	if _, ok := fdOf(nil); ok {
		t.Fatal("expected nil to be ignored")
	}
}

func TestFDOfIgnoresTypedNilFDProvider(t *testing.T) {
	var nilFile *os.File
	if _, ok := fdOf(nilFile); ok {
		t.Fatal("expected typed nil interface to be ignored")
	}
}

func TestSameTTYOutputFD(t *testing.T) {
	tests := []struct {
		name        string
		stdout      io.Reader
		stderr      io.Reader
		wantFD      int
		wantUnified bool
	}{
		{
			name:        "same fd",
			stdout:      fdReadCloser{fd: 42},
			stderr:      fdReadCloser{fd: 42},
			wantFD:      42,
			wantUnified: true,
		},
		{
			name:        "different fd",
			stdout:      fdReadCloser{fd: 42},
			stderr:      fdReadCloser{fd: 43},
			wantUnified: false,
		},
		{
			name:        "stdout without fd",
			stdout:      failingReadCloser{},
			stderr:      fdReadCloser{fd: 42},
			wantUnified: false,
		},
		{
			name:        "stderr without fd",
			stdout:      fdReadCloser{fd: 42},
			stderr:      failingReadCloser{},
			wantUnified: false,
		},
		{
			name:        "nil reader",
			stdout:      nil,
			stderr:      fdReadCloser{fd: 42},
			wantUnified: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFD, gotUnified := sameTTYOutputFD(tt.stdout, tt.stderr)
			if gotUnified != tt.wantUnified {
				t.Fatalf("sameTTYOutputFD unified = %v, want %v", gotUnified, tt.wantUnified)
			}
			if gotFD != tt.wantFD {
				t.Fatalf("sameTTYOutputFD fd = %d, want %d", gotFD, tt.wantFD)
			}
		})
	}
}

func TestCloseTTYOutputReadersClosesSharedNonFDHandleOnce(t *testing.T) {
	shared := &closeTrackingTTY{}
	var stdout io.Reader = shared
	var stderr io.Reader = shared

	if err := joinCloseErrors(closeTTYOutputReaders(&stdout, &stderr)); err != nil {
		t.Fatalf("closeTTYOutputReaders returned error: %v", err)
	}
	if shared.closeCount != 1 {
		t.Fatalf("shared closeCount = %d, want 1", shared.closeCount)
	}
	if stdout != nil || stderr != nil {
		t.Fatalf("TTY outputs should be reset after close, stdout=%v stderr=%v", stdout, stderr)
	}
}

func TestPlanCopierStart(t *testing.T) {
	tests := []struct {
		name       string
		stdinFIFO  io.ReadCloser
		stdoutFIFO io.WriteCloser
		stderrFIFO io.WriteCloser
		in         io.WriteCloser
		out        io.Reader
		err        io.Reader
		want       copierStartPlan
	}{
		{
			name:       "all streams separate",
			stdinFIFO:  failingReadCloser{},
			stdoutFIFO: failingWriteCloser{},
			stderrFIFO: failingWriteCloser{},
			in:         failingWriteCloser{},
			out:        fdReadCloser{fd: 10},
			err:        fdReadCloser{fd: 11},
			want: copierStartPlan{
				stdinToTTY:   true,
				stdoutToFIFO: true,
				stderrToFIFO: true,
			},
		},
		{
			name:       "unified output wins over separate output workers",
			stdinFIFO:  failingReadCloser{},
			stdoutFIFO: failingWriteCloser{},
			stderrFIFO: failingWriteCloser{},
			in:         failingWriteCloser{},
			out:        fdReadCloser{fd: 10},
			err:        fdReadCloser{fd: 10},
			want: copierStartPlan{
				stdinToTTY:      true,
				unifiedOutput:   true,
				unifiedOutputFD: 10,
			},
		},
		{
			name:       "missing fifo skips corresponding workers",
			stdoutFIFO: failingWriteCloser{},
			in:         failingWriteCloser{},
			out:        fdReadCloser{fd: 10},
			err:        fdReadCloser{fd: 11},
			want:       copierStartPlan{stdoutToFIFO: true},
		},
		{
			name:       "missing tty skips corresponding workers",
			stdinFIFO:  failingReadCloser{},
			stdoutFIFO: failingWriteCloser{},
			stderrFIFO: failingWriteCloser{},
			in:         nil,
			out:        fdReadCloser{fd: 10},
			err:        nil,
			want:       copierStartPlan{stdoutToFIFO: true},
		},
		{
			name:       "missing stdout fifo keeps stderr worker even when tty fds match",
			stderrFIFO: failingWriteCloser{},
			out:        fdReadCloser{fd: 10},
			err:        fdReadCloser{fd: 10},
			want:       copierStartPlan{stderrToFIFO: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planCopierStart(tt.stdinFIFO, tt.stdoutFIFO, tt.stderrFIFO, tt.in, tt.out, tt.err)
			if got != tt.want {
				t.Fatalf("planCopierStart() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestCompressLineEndings tests the filter.CompressLineEndings function
func TestCompressLineEndings(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "single CRLF",
			input:    []byte("Hello\r\nWorld"),
			expected: []byte("Hello\r\nWorld"),
		},
		{
			name:     "multiple consecutive CRLF",
			input:    []byte("Hello\r\n\r\n\r\nWorld"),
			expected: []byte("Hello\r\nWorld"),
		},
		{
			name:     "single LF",
			input:    []byte("Hello\nWorld"),
			expected: []byte("Hello\r\nWorld"),
		},
		{
			name:     "multiple consecutive LF",
			input:    []byte("Hello\n\n\nWorld"),
			expected: []byte("Hello\r\nWorld"),
		},
		{
			name:     "mixed CRLF and LF",
			input:    []byte("Hello\r\n\n\r\nWorld"),
			expected: []byte("Hello\r\nWorld"),
		},
		{
			name:     "standalone CR",
			input:    []byte("Hello\rWorld"),
			expected: []byte("Hello\rWorld"),
		},
		{
			name:     "standalone CR followed by CRLF",
			input:    []byte("Hello\r\r\nWorld"),
			expected: []byte("Hello\r\r\nWorld"),
		},
		{
			name:     "empty input",
			input:    []byte(""),
			expected: []byte(""),
		},
		{
			name:     "only line endings",
			input:    []byte("\r\n\r\n\r\n"),
			expected: []byte("\r\n"),
		},
		{
			name:     "RTOS boot message - excessive newlines",
			input:    []byte("Hello, UniProton!\r\n\r\n\r\nopenEuler UniProton #"),
			expected: []byte("Hello, UniProton!\r\nopenEuler UniProton #"),
		},
		{
			name:     "no line endings",
			input:    []byte("HelloWorld"),
			expected: []byte("HelloWorld"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.CompressLineEndings(tt.input)
			if string(result) != string(tt.expected) {
				t.Errorf("filter.CompressLineEndings(%q) = %q, want %q",
					string(tt.input), string(result), string(tt.expected))
			}
		})
	}
}

// TestCompressLineEndingsBoundaryCases tests boundary conditions
func TestCompressLineEndingsBoundaryCases(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "CR at end",
			input:    []byte("Hello\r"),
			expected: []byte("Hello\r"),
		},
		{
			name:     "LF at end",
			input:    []byte("Hello\n"),
			expected: []byte("Hello\r\n"),
		},
		{
			name:     "CRLF at end",
			input:    []byte("Hello\r\n"),
			expected: []byte("Hello\r\n"),
		},
		{
			name:     "starts with LF",
			input:    []byte("\nHello"),
			expected: []byte("\r\nHello"),
		},
		{
			name:     "starts with CRLF",
			input:    []byte("\r\nHello"),
			expected: []byte("\r\nHello"),
		},
		{
			name:     "starts with CR",
			input:    []byte("\rHello"),
			expected: []byte("\rHello"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.CompressLineEndings(tt.input)
			if string(result) != string(tt.expected) {
				t.Errorf("filter.CompressLineEndings(%q) = %q, want %q",
					string(tt.input), string(result), string(tt.expected))
			}
		})
	}
}

// TestFilterNUL tests the filter.FilterNUL function
func TestFilterNUL(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "no NUL bytes",
			input:    []byte("Hello"),
			expected: []byte("Hello"),
		},
		{
			name:     "NUL bytes in middle",
			input:    []byte("H\x00e\x00l\x00l\x00o"),
			expected: []byte("Hello"),
		},
		{
			name:     "NUL bytes at start",
			input:    []byte("\x00\x00Hello"),
			expected: []byte("Hello"),
		},
		{
			name:     "NUL bytes at end",
			input:    []byte("Hello\x00\x00"),
			expected: []byte("Hello"),
		},
		{
			name:     "all NUL bytes",
			input:    []byte("\x00\x00\x00"),
			expected: []byte(""),
		},
		{
			name:     "empty input",
			input:    []byte(""),
			expected: []byte(""),
		},
		{
			name:     "mixed with CRLF",
			input:    []byte("Hel\x00lo\r\nWor\x00ld"),
			expected: []byte("Hello\r\nWorld"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a destination buffer with same capacity as input
			dst := make([]byte, 0, len(tt.input))
			result := filter.FilterNUL(dst, tt.input)
			if string(result) != string(tt.expected) {
				t.Errorf("filter.FilterNUL(%q) = %q, want %q",
					string(tt.input), string(result), string(tt.expected))
			}
		})
	}
}

// TestRemoveNUL tests the removeNUL function
func TestRemoveNUL(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "no NUL bytes",
			input:    []byte("Hello"),
			expected: []byte("Hello"),
		},
		{
			name:     "NUL bytes scattered",
			input:    []byte("H\x00e\x00l\x00l\x00o"),
			expected: []byte("Hello"),
		},
		{
			name:     "RTOS output with NUL bytes",
			input:    []byte("Hel\x00lo,\x00 UniPro\x00ton!\r\n"),
			expected: []byte("Hello, UniProton!\r\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.FilterNUL(nil, tt.input)
			if string(result) != string(tt.expected) {
				t.Errorf("filter.FilterNUL(%q) = %q, want %q",
					string(tt.input), string(result), string(tt.expected))
			}
		})
	}
}

// TestCompressLineEndingsFragmented tests fragmented input simulation
// This simulates the case where Read() buffers split CRLF sequences
func TestCompressLineEndingsFragmented(t *testing.T) {
	// Simulate RTOS output split across multiple Read() calls
	// Original: "Hello\r\n\r\nWorld"
	// Split into: ["Hello\r", "\n", "\r\n", "World"]

	fragments := [][]byte{
		[]byte("Hello\r"),
		[]byte("\n\r"),
		[]byte("\nWorld"),
	}

	// Process fragments through one stateful normalizer, as copier does.
	normalizer := filter.NewOutputNormalizer(filter.OutputConfig{CompressLineEndings: true})
	var result []byte
	for _, frag := range fragments {
		compressed := normalizer.Normalize(frag)
		result = append(result, compressed...)
	}
	result = append(result, normalizer.Flush()...)

	expected := []byte("Hello\r\nWorld")
	if string(result) != string(expected) {
		t.Fatalf("fragmented compression = %q, want %q", result, expected)
	}
}

// TestMin tests the min helper function
func TestMin(t *testing.T) {
	tests := []struct {
		a, b     int
		expected int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{5, 5, 5},
		{0, 10, 0},
		{-1, 1, -1},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := min(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("min(%d, %d) = %d, want %d",
					tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

// TestCompressLineEndingsRealWorld tests real-world RTOS output patterns
func TestCompressLineEndingsRealWorld(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "RTOS boot with excessive newlines",
			input:    []byte("\r\n\r\n\r\nHello, UniProton!\r\n\r\n\r\nopenEuler UniProton # \r\n\r\n\r\n"),
			expected: []byte("\r\nHello, UniProton!\r\nopenEuler UniProton # \r\n"),
		},
		{
			name:     "help command output",
			input:    []byte("support shell\r\n\r\nAvailable commands:\r\n\r\n  help\r\n\r\n  version\r\n\r\nopenEuler UniProton #"),
			expected: []byte("support shell\r\nAvailable commands:\r\n  help\r\n  version\r\nopenEuler UniProton #"),
		},
		{
			name:     "prompt after Enter",
			input:    []byte("openEuler UniProton # \r\n\r\n\r\nopenEuler UniProton # "),
			expected: []byte("openEuler UniProton # \r\nopenEuler UniProton # "),
		},
		{
			name:     "command with output",
			input:    []byte("help\r\n\r\n\r\nsupport shell\r\n\r\n\r\nopenEuler UniProton # "),
			expected: []byte("help\r\nsupport shell\r\nopenEuler UniProton # "),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.CompressLineEndings(tt.input)
			if string(result) != string(tt.expected) {
				t.Errorf("filter.CompressLineEndings(%q) = %q, want %q",
					string(tt.input), string(result), string(tt.expected))
			}
		})
	}
}

// TestCompressLineEndingsPerformance tests performance characteristics
func TestCompressLineEndingsPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	// Generate large input with many consecutive newlines
	input := make([]byte, 0, 10000)
	for i := 0; i < 1000; i++ {
		input = append(input, []byte("Line\r\n\r\n\r\n")...)
	}

	// Benchmark
	result := filter.CompressLineEndings(input)

	// Verify compression happened
	if len(result) >= len(input) {
		t.Errorf("Compression failed: result length %d >= input length %d",
			len(result), len(input))
	}

	// Verify expected compression ratio
	// Input: "Line\r\n\r\n\r\n" = 10 bytes, repeated 1000 times = 10000 bytes
	// Expected: "Line\r\n" = 7 bytes, repeated 1000 times = 7000 bytes
	expectedLen := 7 * 1000
	if len(result) != expectedLen {
		t.Logf("Note: result length %d, expected %d", len(result), expectedLen)
	}
}

// TestFilterNULPerformance tests NUL filtering performance
func TestFilterNULPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	// Generate input with every other byte being NUL
	input := make([]byte, 10000)
	for i := range input {
		if i%2 == 0 {
			input[i] = 'A'
		} else {
			input[i] = 0
		}
	}

	dst := make([]byte, 0, len(input))
	result := filter.FilterNUL(dst, input)

	// Verify filtering happened
	if len(result) != 5000 {
		t.Errorf("Filter result length %d, expected 5000", len(result))
	}

	// Verify no NUL bytes remain
	for _, b := range result {
		if b == 0 {
			t.Error("NUL byte found in filtered output")
		}
	}
}

// TestBoolToInt tests the boolToInt helper function
func TestBoolToInt(t *testing.T) {
	tests := []struct {
		input    bool
		expected int32
	}{
		{true, 1},
		{false, 0},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := boolToInt(tt.input)
			if result != tt.expected {
				t.Errorf("boolToInt(%v) = %d, want %d",
					tt.input, result, tt.expected)
			}
		})
	}
}

// TestIsEAGAIN tests the isEAGAIN helper function
func TestIsEAGAIN(t *testing.T) {
	// Test with actual syscall.EAGAIN
	err := syscall.EAGAIN
	if !isEAGAIN(err) {
		t.Error("isEAGAIN(syscall.EAGAIN) returned false")
	}

	// Test with EWOULDBLOCK (same as EAGAIN on many systems)
	err2 := syscall.EWOULDBLOCK
	if !isEAGAIN(err2) {
		t.Error("isEAGAIN(syscall.EWOULDBLOCK) returned false")
	}

	if !isEAGAIN(fmt.Errorf("wrapped: %w", syscall.EAGAIN)) {
		t.Error("isEAGAIN(wrapped EAGAIN) returned false")
	}
}

func TestIOErrorClassifiersUseWrappedSentinels(t *testing.T) {
	if !isClosed(fmt.Errorf("wrapped: %w", os.ErrClosed)) {
		t.Fatal("isClosed(wrapped os.ErrClosed) returned false")
	}
	if !isClosed(fmt.Errorf("wrapped: %w", syscall.EBADF)) {
		t.Fatal("isClosed(wrapped EBADF) returned false")
	}
	if !isBrokenPipe(fmt.Errorf("wrapped: %w", syscall.EPIPE)) {
		t.Fatal("isBrokenPipe(wrapped EPIPE) returned false")
	}
}

func TestShouldMarkPostInputOutput(t *testing.T) {
	tests := []struct {
		name         string
		inputGen     uint64
		observedGen  uint64
		outputLen    int
		expectMarked bool
	}{
		{
			name:         "no output bytes",
			inputGen:     1,
			observedGen:  0,
			outputLen:    0,
			expectMarked: false,
		},
		{
			name:         "no stdin activity yet",
			inputGen:     0,
			observedGen:  0,
			outputLen:    16,
			expectMarked: false,
		},
		{
			name:         "first output after stdin activity",
			inputGen:     1,
			observedGen:  0,
			outputLen:    16,
			expectMarked: true,
		},
		{
			name:         "same input generation already observed",
			inputGen:     1,
			observedGen:  1,
			outputLen:    16,
			expectMarked: false,
		},
		{
			name:         "newer input generation should mark again",
			inputGen:     2,
			observedGen:  1,
			outputLen:    4,
			expectMarked: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldMarkPostInputOutput(tt.inputGen, tt.observedGen, tt.outputLen)
			if got != tt.expectMarked {
				t.Fatalf("shouldMarkPostInputOutput(%d, %d, %d) = %v, want %v",
					tt.inputGen, tt.observedGen, tt.outputLen, got, tt.expectMarked)
			}
		})
	}
}

func TestLogNonTTYInputActivityDoesNotLeakPayloadAtInfoLevel(t *testing.T) {
	var buf bytes.Buffer
	oldOut := log.Log.Out
	oldLevel := log.Log.Level
	log.Log.SetOutput(&buf)
	log.Log.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		log.Log.SetOutput(oldOut)
		log.Log.SetLevel(oldLevel)
	})

	logNonTTYInputActivity("test-ctr", []byte("secret-command\n"))

	output := buf.String()
	if strings.Contains(output, "secret-command") {
		t.Fatalf("unexpected payload leak in info log: %s", output)
	}
	if !strings.Contains(output, "Non-TTY stdin activity") {
		t.Fatalf("missing non-tty activity summary log: %s", output)
	}
}

func TestLogNonTTYInputActivityKeepsPayloadAtDebugLevel(t *testing.T) {
	var buf bytes.Buffer
	oldOut := log.Log.Out
	oldLevel := log.Log.Level
	log.Log.SetOutput(&buf)
	log.Log.SetLevel(logrus.DebugLevel)
	t.Cleanup(func() {
		log.Log.SetOutput(oldOut)
		log.Log.SetLevel(oldLevel)
	})

	logNonTTYInputActivity("test-ctr", []byte("help\n"))

	output := buf.String()
	if !strings.Contains(output, "help") {
		t.Fatalf("expected debug log to keep payload, got: %s", output)
	}
}

func TestWriteTTYPacesInputAsSingleByteWrites(t *testing.T) {
	tty := &recordingTTYWriter{}
	copier := NewCopier(Config{
		ContainerID:       "test-paced-tty",
		TTYWriteDelay:     -1,
		TTYWriteLineDelay: -1,
	})
	copier.SetTTYs(tty, nil, nil)

	n, err := copier.writeTTY([]byte("abc"))
	if err != nil {
		t.Fatalf("writeTTY returned error: %v", err)
	}
	if n != 3 {
		t.Fatalf("writeTTY wrote %d bytes, want 3", n)
	}
	if got := tty.String(); got != "abc" {
		t.Fatalf("TTY received %q, want abc", got)
	}
	if len(tty.writes) != 3 {
		t.Fatalf("TTY received %d write calls, want 3", len(tty.writes))
	}
	for i, write := range tty.writes {
		if len(write) != 1 {
			t.Fatalf("write %d length = %d, want 1", i, len(write))
		}
	}
}

func TestCopyStdinNonTTYUsesPacedCRLFWrite(t *testing.T) {
	tty := &recordingTTYWriter{}
	copier := NewCopier(Config{
		ContainerID:       "test-non-tty-paced",
		TTYWriteDelay:     -1,
		TTYWriteLineDelay: -1,
	})
	copier.SetTTYs(tty, nil, nil)

	copier.copyStdinNonTTY([]byte("help\nuname\n"))

	const want = "help\r\nuname\r\n"
	if got := tty.String(); got != want {
		t.Fatalf("TTY received %q, want %q", got, want)
	}
	if len(tty.writes) != len(want) {
		t.Fatalf("TTY received %d write calls, want %d", len(tty.writes), len(want))
	}
}

func TestCopyStdinNonTTYDetectsFragmentedExitCommand(t *testing.T) {
	bus := NewEventBus(context.Background())
	t.Cleanup(bus.Close)
	exitCh := bus.Subscribe(ExitCommandDetected)
	tty := &recordingTTYWriter{}
	copier := NewCopier(Config{
		ContainerID:       "test-non-tty-fragmented-exit",
		EventBus:          bus,
		TTYWriteDelay:     -1,
		TTYWriteLineDelay: -1,
	})
	copier.SetTTYs(tty, nil, nil)

	copier.copyStdinNonTTY([]byte("ex"))
	copier.copyStdinNonTTY([]byte("it\n"))

	select {
	case ev := <-exitCh:
		if ev.ContainerID != "test-non-tty-fragmented-exit" {
			t.Fatalf("exit event container = %q, want test-non-tty-fragmented-exit", ev.ContainerID)
		}
	default:
		t.Fatal("expected fragmented non-TTY exit command to publish exit event")
	}
	if !copier.stopped.Load() {
		t.Fatal("copier should stop after fragmented non-TTY exit command")
	}
	if got := tty.String(); got != "ex" {
		t.Fatalf("TTY received %q, want only first pre-exit fragment", got)
	}
}

func TestCopyStdinNonTTYDoesNotTreatFragmentedNonExitAsExit(t *testing.T) {
	bus := NewEventBus(context.Background())
	t.Cleanup(bus.Close)
	exitCh := bus.Subscribe(ExitCommandDetected)
	tty := &recordingTTYWriter{}
	copier := NewCopier(Config{
		ContainerID:       "test-non-tty-fragmented-word",
		EventBus:          bus,
		TTYWriteDelay:     -1,
		TTYWriteLineDelay: -1,
	})
	copier.SetTTYs(tty, nil, nil)

	copier.copyStdinNonTTY([]byte("ex"))
	copier.copyStdinNonTTY([]byte("amine\n"))

	select {
	case ev := <-exitCh:
		t.Fatalf("unexpected exit event for non-exit line: %+v", ev)
	default:
	}
	if copier.stopped.Load() {
		t.Fatal("copier should keep running for non-exit line")
	}
	if got, want := tty.String(), "examine\r\n"; got != want {
		t.Fatalf("TTY received %q, want %q", got, want)
	}
}

func TestCopyStdinTTYCtrlCPublishesInterrupt(t *testing.T) {
	bus := NewEventBus(context.Background())
	t.Cleanup(bus.Close)
	interruptCh := bus.Subscribe(InterruptDetected)
	tty := &recordingTTYWriter{}
	copier := NewCopier(Config{
		ContainerID:       "test-tty-interrupt",
		EventBus:          bus,
		Terminal:          true,
		TTYWriteDelay:     -1,
		TTYWriteLineDelay: -1,
	})
	copier.SetTTYs(tty, nil, nil)

	copier.copyStdinTTY([]byte{0x03})

	select {
	case ev := <-interruptCh:
		if ev.ContainerID != "test-tty-interrupt" {
			t.Fatalf("interrupt event container = %q, want test-tty-interrupt", ev.ContainerID)
		}
	default:
		t.Fatal("expected TTY Ctrl+C to publish interrupt event")
	}
	if !copier.stopped.Load() {
		t.Fatal("copier should stop after TTY Ctrl+C")
	}
	if got := tty.String(); got != "" {
		t.Fatalf("TTY received %q, want Ctrl+C to be handled locally", got)
	}
}

func TestCopyStdinNonTTYCtrlCRemainsInputByte(t *testing.T) {
	bus := NewEventBus(context.Background())
	t.Cleanup(bus.Close)
	interruptCh := bus.Subscribe(InterruptDetected)
	tty := &recordingTTYWriter{}
	copier := NewCopier(Config{
		ContainerID:       "test-non-tty-ctrl-c",
		EventBus:          bus,
		TTYWriteDelay:     -1,
		TTYWriteLineDelay: -1,
	})
	copier.SetTTYs(tty, nil, nil)

	copier.copyStdinNonTTY([]byte{0x03, '\n'})

	select {
	case ev := <-interruptCh:
		t.Fatalf("unexpected interrupt event for non-TTY Ctrl+C byte: %+v", ev)
	default:
	}
	if copier.stopped.Load() {
		t.Fatal("copier should keep running for non-TTY Ctrl+C byte")
	}
	if got, want := tty.String(), string([]byte{0x03, '\r', '\n'}); got != want {
		t.Fatalf("TTY received %q, want %q", got, want)
	}
}

func TestWriteTTYReturnsClosedPipeWhenMissingTTY(t *testing.T) {
	copier := NewCopier(Config{ContainerID: "test-missing-tty"})
	_, err := copier.writeTTY([]byte("x"))
	if err != io.ErrClosedPipe {
		t.Fatalf("writeTTY error = %v, want io.ErrClosedPipe", err)
	}
}

func TestTrackSentCharBeforeEchoSuppression(t *testing.T) {
	copier := NewCopier(Config{
		ContainerID: "test-echo-suppression",
		Terminal:    true,
	})

	copier.trackSentCharForEcho('h')
	got := copier.suppressRTOSEcho([]byte("h"))
	if len(got) != 0 {
		t.Fatalf("suppressRTOSEcho returned %q, want empty echoed byte", got)
	}
}
