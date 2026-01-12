//go:build debug
// +build debug

package log

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/sirupsen/logrus"
)

// setupOutputImpl sets up output for debug builds.
// Outputs to both containerd log fifo AND a log file.
func setupOutputImpl(cfg *Config) error {
	// Open containerd log fifo
	containerdLog, err := openContainerdLog()
	if err != nil {
		// If we can't open containerd log, use stderr as fallback
		containerdLog = os.Stderr
	}

	// Open log file for debug output
	if err := ensureLogDirectory(cfg.Log.File); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	fileLog, err := os.OpenFile(cfg.Log.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", cfg.Log.File, err)
	}

	// Set output to containerd log only (file output will be handled by hook)
	Log.SetOutput(containerdLog)

	// Use containerd formatter for the main output
	Log.SetFormatter(&containerdFormatter{})

	// Add hooks
	Log.AddHook(&contextHook{})
	Log.AddHook(&fileHook{
		file:      fileLog,
		formatter: &fileFormatter{color: cfg.Log.Color, caller: cfg.Log.Caller},
	})

	Log.SetReportCaller(true) // Enable caller reporting for debug

	return nil
}

// openContainerdLog opens the containerd-provided log fifo.
func openContainerdLog() (io.WriteCloser, error) {
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

// fileHook writes log entries to a file in a custom format.
type fileHook struct {
	file      io.WriteCloser
	formatter *fileFormatter
}

func (h *fileHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *fileHook) Fire(entry *logrus.Entry) error {
	// Fix caller information to skip logger.go wrapper
	// The entry.Caller points to logger.go due to wrapper functions
	// We need to find the actual caller outside of logger package
	if entry.HasCaller() {
		file, line, fn := getRealCaller()
		if file != "" {
			// Directly modify entry.Caller to point to the real caller
			entry.Caller.File = file
			entry.Caller.Line = line
			entry.Caller.Function = fn
		}
	}

	// Format the entry for file output
	bytes, err := h.formatter.Format(entry)
	if err != nil {
		return err
	}

	// Write to file
	_, err = h.file.Write(bytes)
	return err
}

// getRealCaller finds the actual caller by walking up the stack
// until we find a frame outside of the logger package.
func getRealCaller() (file string, line int, fn string) {
	// Start from frame 4 to skip:
	// 0: getRealCaller itself
	// 1: fileHook.Fire
	// 2: contextHook.Fire
	// 3: logrus.(*logger).Log
	// 4+: actual caller
	const maxDepth = 15
	for i := 4; i < maxDepth; i++ {
		pc, f, l, ok := runtime.Caller(i)
		if !ok {
			break
		}

		// Skip frames from the logger package
		if isLoggerPackage(f) {
			continue
		}

		// Also skip logrus internal frames
		if isLogrusPackage(f) {
			continue
		}

		// Found a frame outside logger package
		function := ""
		funcPtr := runtime.FuncForPC(pc)
		if funcPtr != nil {
			function = funcPtr.Name()
		}

		return f, l, function
	}

	return "", 0, ""
}

// isLogrusPackage checks if the file is from the logrus vendor package.
func isLogrusPackage(file string) bool {
	// Check if file path contains "vendor/github.com/sirupsen/logrus"
	return containsPath(file, "vendor/github.com/sirupsen/logrus") ||
		containsPath(file, "logrus/entry.go") ||
		containsPath(file, "logrus/logger.go")
}

// containsPath checks if a path contains a specific component.
func containsPath(file, component string) bool {
	for i := len(file) - len(component); i >= 0; i-- {
		if file[i:i+len(component)] == component {
			// Check if it's a proper path component
			if (i == 0 || file[i-1] == '/' || file[i-1] == '\\') &&
			   (i+len(component) >= len(file) || file[i+len(component)] == '/' || file[i+len(component)] == '\\') {
				return true
			}
		}
	}
	return false
}

// isLoggerPackage checks if the file is from the logger package.
func isLoggerPackage(file string) bool {
	// Check for "/logger/" pattern (files inside logger directory)
	for i := 0; i < len(file)-7; i++ {
		if file[i:i+8] == "/logger/" {
			return true
		}
	}

	// Also check if the file is logger.go itself
	if len(file) >= 10 && file[len(file)-10:] == "/logger.go" {
		return true
	}

	return false
}

// fileFormatter formats logs for file output in debug builds.
// Format: [namespace][id][timestamp]LOGLEVEL file:line func\n\tmessage
// Timestamp uses nanosecond precision to match containerd format
type fileFormatter struct {
	color  bool
	caller bool
}

func (f *fileFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var b *bytes.Buffer

	if entry.Buffer != nil {
		b = entry.Buffer
	} else {
		b = &bytes.Buffer{}
	}

	// Get namespace from fields
	namespace := ""
	if ns, ok := entry.Data[NamespaceKey]; ok {
		namespace = fmt.Sprintf("%v", ns)
	}

	// Get container ID from fields
	containerID := ""
	if id, ok := entry.Data[IDKey]; ok {
		containerID = fmt.Sprintf("%v", id)
	}

	// Format: [namespace][id][timestamp]LOGLEVEL file:line func\n\tmessage
	if namespace != "" {
		b.WriteString("[")
		b.WriteString(namespace)
		b.WriteString("]")
	}

	if containerID != "" {
		b.WriteString("[")
		b.WriteString(containerID)
		b.WriteString("]")
	}

	// Timestamp with nanoseconds: 1970-01-01T03:18:02.011123456Z
	timestamp := entry.Time.Format("2006-01-02T15:04:05.000000000Z")
	b.WriteString("[")
	b.WriteString(timestamp)
	b.WriteString("]")

	// Log level (uppercase, e.g., DEBUG, INFO, WARN, ERROR)
	level := entry.Level.String()
	switch level {
	case "debug":
		level = "DEBUG"
	case "info":
		level = "INFO"
	case "warning":
		level = "WARN"
	case "error":
		level = "ERROR"
	case "fatal":
		level = "FATAL"
	case "panic":
		level = "PANIC"
	default:
		level = "UNKNOWN"
	}
	b.WriteString(level)

	// Caller information
	if f.caller {
		if entry.HasCaller() {
			file := entry.Caller.File
			line := entry.Caller.Line
			fn := entry.Caller.Function

			// Convert file path to be relative to micrun directory
			file = relativeToMicrun(file)

			b.WriteString(" ")
			b.WriteString(file)
			b.WriteString(":")
			b.WriteString(fmt.Sprintf("%d", line))
			if fn != "" {
				b.WriteString(" ")
				b.WriteString(fn)
			}
		}
	}

	b.WriteString("\n\t")

	// Message with optional color codes
	if f.color {
		b.WriteString(colorize(entry.Level, entry.Message))
	} else {
		b.WriteString(entry.Message)
	}

	// Add other fields (excluding namespace and id which we already used)
	for k, v := range entry.Data {
		if k != NamespaceKey && k != IDKey {
			b.WriteString(" ")
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(fmt.Sprintf("%v", v))
		}
	}

	b.WriteByte('\n')
	return b.Bytes(), nil
}

// relativeToMicrun converts an absolute file path to be relative to the micrun directory.
func relativeToMicrun(file string) string {
	// Try to find "micrun/" in the path and strip everything before and including it
	for i := len(file) - 1; i >= 0; i-- {
		if i+6 <= len(file) && file[i:i+6] == "micrun" {
			// Check if this is actually the micrun directory (followed by /)
			if i+6 < len(file) && (file[i+6] == '/' || file[i+6] == '\\') {
				// Return everything after "micrun/"
				return file[i+7:]
			}
		}
	}
	// If we couldn't find micrun/, return the original path
	return file
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

// Color codes for different log levels
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorGreen  = "\033[32m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
)

// colorize adds ANSI color codes to the message based on log level.
func colorize(level logrus.Level, msg string) string {
	var colorCode string

	switch level {
	case logrus.DebugLevel:
		colorCode = colorBlue
	case logrus.InfoLevel:
		colorCode = colorGreen
	case logrus.WarnLevel:
		colorCode = colorYellow
	case logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel:
		colorCode = colorRed
	default:
		colorCode = colorPurple
	}

	return colorCode + msg + colorReset
}
