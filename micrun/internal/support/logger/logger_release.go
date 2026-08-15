//go:build !debug
// +build !debug

package log

import (
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
)

// setupOutputImpl sets up output for release builds.
// Only outputs to containerd log fifo.
func setupOutputImpl(cfg *Config) error {
	if err := closeActiveDebugLogFile(); err != nil {
		// Best effort cleanup for transition from debug mode.
		// Logging should remain available even when previous debug file handle cleanup fails.
		fmt.Fprintf(os.Stderr, "warning: close active debug log file: %v\n", err)
	}

	// Open containerd log fifo (in shim's cwd)
	containerdLog, err := openContainerdLog()
	if err != nil {
		// If we can't open containerd log, fall back to stderr
		// This can happen during early shim startup
		containerdLog = os.Stderr
	}

	// Switch logrus output to the new writer BEFORE closing the old one
	// via swapActiveContainerdLog. Otherwise concurrent goroutines calling
	// log.Debug/Info/... between the close and SetOutput would write to a
	// closed fd (EBADF) and silently lose log lines.
	Log.SetOutput(containerdLog)
	if err == nil {
		swapActiveContainerdLog(containerdLog)
	} else {
		// Reopen failed and we fell back to stderr. stderr must never be
		// tracked (a later swap would Close it), but the previously tracked
		// fifo fd still needs to be released instead of orphaned.
		closeTrackedContainerdLog()
	}
	Log.SetFormatter(&containerdFormatter{})
	Log.SetReportCaller(false) // No caller info in release logs
	Log.ReplaceHooks(make(logrus.LevelHooks))
	Log.AddHook(&contextHook{})

	return nil
}

// tracefImpl implements Tracef for release builds.
// This is a NO-OP function in release builds.
//
// The Go compiler optimizes away all calls to this function, resulting in:
//   - Zero runtime overhead
//   - No string formatting executed
//   - No memory allocation for format strings or arguments
//
// This means all Tracef() calls effectively disappear from release binaries,
// reducing log noise without losing any debugging information in debug builds.
func tracefImpl(format string, args ...any) {
	// No-op: compiler optimizes away all calls to this function
	// No code here ensures the function body is empty
}
