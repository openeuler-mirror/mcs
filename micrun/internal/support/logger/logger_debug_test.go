//go:build debug
// +build debug

package log

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDebugSetupOutputCreatesFileAndEnablesCaller(t *testing.T) {
	oldHooks := Log.Hooks
	oldOut := Log.Out
	oldReportCaller := Log.ReportCaller
	defer func() {
		Log.ReplaceHooks(oldHooks)
		Log.SetOutput(oldOut)
		Log.SetReportCaller(oldReportCaller)
	}()

	cfg := DefaultConfig()
	cfg.Log.File = filepath.Join(t.TempDir(), "micrun-debug.log")

	require.NoError(t, setupOutputImpl(cfg))
	require.True(t, Log.ReportCaller)
	require.NotZero(t, countLoggerHooks(Log.Hooks))

	_, err := os.Stat(cfg.Log.File)
	require.NoError(t, err)
}

func TestDebugSetupOutputReplacesPreviousFileHandle(t *testing.T) {
	oldOpen := openLogFileFn
	oldHooks := Log.Hooks
	oldOut := Log.Out
	oldReportCaller := Log.ReportCaller
	defer func() {
		openLogFileFn = oldOpen
		Log.ReplaceHooks(oldHooks)
		Log.SetOutput(oldOut)
		Log.SetReportCaller(oldReportCaller)
		require.NoError(t, closeActiveDebugLogFile())
	}()

	workDir := t.TempDir()

	first := &testWriteCloser{}
	second := &testWriteCloser{}
	callCount := 0
	openLogFileFn = func(path string) (io.WriteCloser, error) {
		callCount++
		if callCount == 1 {
			return first, nil
		}
		return second, nil
	}

	cfg := DefaultConfig()
	cfg.Log.File = filepath.Join(workDir, "one.log")

	require.NoError(t, setupOutputImpl(cfg))
	require.False(t, first.closed)
	require.Equal(t, 1, callCount)
	require.NotZero(t, countLoggerHooks(Log.Hooks))

	cfg.Log.File = filepath.Join(workDir, "two.log")
	require.NoError(t, setupOutputImpl(cfg))
	require.True(t, first.closed)
	require.False(t, second.closed)
	require.Equal(t, 2, callCount)
	require.NotZero(t, countLoggerHooks(Log.Hooks))

	require.NoError(t, closeActiveDebugLogFile())
	require.True(t, second.closed)
}

type testWriteCloser struct {
	closed bool
}

func (f *testWriteCloser) Write(_ []byte) (int, error) {
	return 0, nil
}

func (f *testWriteCloser) Close() error {
	f.closed = true
	return nil
}
