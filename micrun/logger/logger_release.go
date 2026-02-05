//go:build !debug
// +build !debug

package log

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/sirupsen/logrus"
)

// setupOutputImpl sets up output for release builds.
// Only outputs to containerd log fifo.
func setupOutputImpl(cfg *Config) error {
	// Open containerd log fifo (in shim's cwd)
	containerdLog, err := openContainerdLog()
	if err != nil {
		// If we can't open containerd log, fall back to stderr
		// This can happen during early shim startup
		containerdLog = os.Stderr
	}

	Log.SetOutput(containerdLog)
	Log.SetFormatter(&containerdFormatter{})
	Log.SetReportCaller(false) // No caller info in release logs
	Log.AddHook(&contextHook{})

	return nil
}

// openContainerdLog opens the containerd-provided log fifo.
// containerd creates a fifo named "log" in the shim's cwd.
func openContainerdLog() (io.WriteCloser, error) {
	// Try to open the log fifo that containerd provides
	// The fifo is in the shim's current working directory
	f, err := os.OpenFile("log", os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open containerd log fifo: %w", err)
	}
	return f, nil
}

// contextHook adds the current namespace and container ID to all log entries.
type contextHook struct{}

func (h *contextHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *contextHook) Fire(entry *logrus.Entry) error {
	// Get current namespace
	namespaceMutex.RLock()
	ns := currentNamespace
	namespaceMutex.RUnlock()

	// Get current container ID
	containerIDMutex.RLock()
	id := currentContainerID
	containerIDMutex.RUnlock()

	// Add namespace if not already present and if it's set
	if ns != "" && entry.Data[NamespaceKey] == nil {
		entry.Data[NamespaceKey] = ns
	}

	// Add id if not already present and if it's set
	if id != "" && entry.Data[IDKey] == nil {
		entry.Data[IDKey] = id
	}

	return nil
}

// containerdFormatter formats logs in containerd-compatible format.
// Format: time=<timestamp> level=<level> msg=<message> id=<id> namespace=<namespace>
// Matches containerd's log format for consistency.
type containerdFormatter struct{}

func (f *containerdFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var b *bytes.Buffer

	if entry.Buffer != nil {
		b = entry.Buffer
	} else {
		b = &bytes.Buffer{}
	}

	// Format: time="..." level=... msg=... id=... namespace=...
	// Timestamp in RFC3339 format with nanoseconds, like containerd
	timestamp := entry.Time.Format("2006-01-02T15:04:05.000000000Z")
	b.WriteString("time=\"")
	b.WriteString(timestamp)
	b.WriteString("\" ")

	// Log level
	b.WriteString("level=")
	b.WriteString(entry.Level.String())
	b.WriteString(" ")

	// Message - use "msg" key like containerd, quote if needed
	message := entry.Message
	if needsQuoting(message) {
		b.WriteString("msg=\"")
		b.WriteString(message)
		b.WriteString("\"")
	} else {
		b.WriteString("msg=")
		b.WriteString(message)
	}

	// Add fields (id first, then namespace, matching containerd order)
	if len(entry.Data) > 0 {
		// Output id first if present (like containerd)
		if id, ok := entry.Data[IDKey]; ok && id != nil {
			b.WriteString(" ")
			b.WriteString(IDKey)
			b.WriteString("=")
			b.WriteString(fmt.Sprintf("%v", id))
		}

		// Output namespace second if present (like containerd)
		if ns, ok := entry.Data[NamespaceKey]; ok && ns != nil {
			b.WriteString(" ")
			b.WriteString(NamespaceKey)
			b.WriteString("=")
			b.WriteString(fmt.Sprintf("%v", ns))
		}

		// Output any other fields
		for k, v := range entry.Data {
			if k != NamespaceKey && k != IDKey {
				b.WriteString(" ")
				b.WriteString(k)
				b.WriteString("=")
				b.WriteString(fmt.Sprintf("%v", v))
			}
		}
	}

	b.WriteByte('\n')
	return b.Bytes(), nil
}

// needsQuoting checks if a string needs quoting.
func needsQuoting(s string) bool {
	for _, c := range s {
		if c <= ' ' || c == '"' || c == '\\' {
			return true
		}
	}
	return false
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
