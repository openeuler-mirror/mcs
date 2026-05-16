//go:build !debug
// +build !debug

package log

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReleaseSetupOutputIgnoresDebugCloseError(t *testing.T) {
	oldActive := activeDebugFile
	oldHooks := Log.Hooks
	oldOut := Log.Out
	defer func() {
		activeDebugFile = oldActive
		Log.ReplaceHooks(oldHooks)
		Log.SetOutput(oldOut)
	}()

	activeDebugFile = &testFailingWriteCloser{}
	logFile := filepath.Join(t.TempDir(), "micrun.log")
	cfg := DefaultConfig()
	cfg.Log.File = logFile

	// This validates that a debug handle close failure doesn't break release logger setup.
	require.NoError(t, setupOutputImpl(cfg))
	require.Nil(t, activeDebugFile)
}
