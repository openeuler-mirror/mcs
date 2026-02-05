package log

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/sirupsen/logrus"
)

const (
	// DefaultConfigPath is the default path for micrun configuration file
	DefaultConfigPath = "/etc/micrun/config.json"

	// DefaultLogFile is the default log file path for debug builds
	DefaultLogFile = "/var/log/mica/mica-runtime.log"

	// IDKey is the field key for container ID in log entries
	// Using "id" to match containerd log format
	IDKey = "id"

	// NamespaceKey is the field key for namespace in log entries
	NamespaceKey = "namespace"
)

var (
	// Log is the global logger instance
	Log *logrus.Logger

	// currentContainerID stores the container ID for the current context
	currentContainerID string
	containerIDMutex   sync.RWMutex

	// currentNamespace stores the namespace for the current context
	currentNamespace string
	namespaceMutex   sync.RWMutex

	currentConfig      *Config
	currentConfigMutex sync.RWMutex
)

func init() {
	Log = logrus.New()
	Log.SetOutput(io.Discard) // Initially discard, will be set during initialization
}

// Config represents the logger configuration.
type Config struct {
	Log LogConfig `json:"log"`
}

// LogConfig holds logging specific configuration.
type LogConfig struct {
	// Level is the minimum log level (debug, info, warn, error).
	// Default: info
	Level string `json:"level,omitempty"`

	// File is the log file path for debug builds.
	// Default: /var/log/mica/mica-runtime.log
	File string `json:"file,omitempty"`

	// Color enables colored output for debug file logs.
	// Default: false
	Color bool `json:"color,omitempty"`

	// Caller enables caller information (file:line func) in debug file logs.
	// Default: true
	Caller bool `json:"caller,omitempty"`
}

// DefaultConfig returns a default configuration.
func DefaultConfig() *Config {
	return &Config{
		Log: LogConfig{
			Level:  "info",
			File:   DefaultLogFile,
			Color:  false,
			Caller: true,
		},
	}
}

// LoadConfig loads the configuration from the specified file.
// If the file does not exist or cannot be read, returns default config.
func LoadConfig(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	if configPath == "" {
		configPath = DefaultConfigPath
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Config file doesn't exist, use defaults
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}

	// Set defaults for empty fields
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.File == "" {
		cfg.Log.File = DefaultLogFile
	}

	return cfg, nil
}

// Initialize initializes the logger with the given configuration.
// If cfg is nil, attempts to load from default config path.
func Initialize(cfg *Config) error {
	if cfg == nil {
		var err error
		cfg, err = LoadConfig("")
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	currentConfigMutex.Lock()
	currentConfig = cfg
	currentConfigMutex.Unlock()

	return initializeLogger(cfg)
}

// initializeLogger performs the actual logger initialization.
func initializeLogger(cfg *Config) error {
	// Parse log level
	level, err := logrus.ParseLevel(cfg.Log.Level)
	if err != nil {
		return fmt.Errorf("invalid log level %q: %w", cfg.Log.Level, err)
	}
	Log.SetLevel(level)

	// Set up formatters and outputs
	return setupOutput(cfg)
}

// setupOutput sets up the output and formatter based on build type.
// This function is implemented differently in logger_release.go and logger_debug.go.
func setupOutput(cfg *Config) error {
	// Implementation in platform-specific files
	return setupOutputImpl(cfg)
}

// SilenceOutput redirects all log output to io.Discard.
// Useful for bootstrap phase where any output corrupts the handshake.
func SilenceOutput() {
	Log.SetOutput(io.Discard)
}

// RestoreOutput restores log output after silence mode.
func RestoreOutput() error {
	currentConfigMutex.RLock()
	cfg := currentConfig
	currentConfigMutex.RUnlock()

	if cfg == nil {
		// Fallback to default config
		cfg = DefaultConfig()
	}

	return setupOutput(cfg)
}

// SetContainerID sets the container ID for log context.
func SetContainerID(id string) {
	containerIDMutex.Lock()
	currentContainerID = id
	containerIDMutex.Unlock()
}

// GetContainerID returns the current container ID.
func GetContainerID() string {
	containerIDMutex.RLock()
	defer containerIDMutex.RUnlock()
	return currentContainerID
}

// SetNamespace sets the namespace for log context.
func SetNamespace(ns string) {
	namespaceMutex.Lock()
	currentNamespace = ns
	namespaceMutex.Unlock()
}

// GetNamespace returns the current namespace.
func GetNamespace() string {
	namespaceMutex.RLock()
	defer namespaceMutex.RUnlock()
	return currentNamespace
}

// GetDefaultNamespace returns the default namespace from environment or "default".
func GetDefaultNamespace() string {
	// Check CONTAINERD_NAMESPACE environment variable first
	// This is set by containerd when launching the shim
	if ns := os.Getenv("CONTAINERD_NAMESPACE"); ns != "" {
		return ns
	}
	// Fallback to "default" namespace
	return "default"
}

// ensureLogDirectory creates the log directory if it doesn't exist.
func ensureLogDirectory(filePath string) error {
	dir := filepath.Dir(filePath)
	return os.MkdirAll(dir, 0755)
}

// WithField adds a single field to the log entry.
func WithField(key string, value any) *logrus.Entry {
	entry := Log.WithField(key, value)
	addContainerIDIfNeeded(entry)
	return entry
}

// WithFields adds multiple fields to the log entry.
func WithFields(fields logrus.Fields) *logrus.Entry {
	entry := Log.WithFields(fields)
	addContainerIDIfNeeded(entry)
	return entry
}

// WithError adds an error field to the log entry.
func WithError(err error) *logrus.Entry {
	entry := Log.WithError(err)
	addContainerIDIfNeeded(entry)
	return entry
}

// addContainerIDIfNeeded adds container ID and namespace to entry if not already present.
func addContainerIDIfNeeded(entry *logrus.Entry) {
	containerIDMutex.RLock()
	id := currentContainerID
	containerIDMutex.RUnlock()

	namespaceMutex.RLock()
	ns := currentNamespace
	namespaceMutex.RUnlock()

	// Add namespace if not already present
	if ns != "" && entry.Data[NamespaceKey] == nil {
		entry.Data[NamespaceKey] = ns
	}

	// Add id if not already present
	if id != "" && entry.Data[IDKey] == nil {
		entry.Data[IDKey] = id
	}
}

// addContainerIDToLogger returns a log entry with id and namespace if set.
// Note: This should NOT be used for direct logging as it loses caller information.
// Use the contextHook in logger_debug.go instead.
func addContainerIDToLogger() *logrus.Entry {
	containerIDMutex.RLock()
	id := currentContainerID
	containerIDMutex.RUnlock()

	namespaceMutex.RLock()
	ns := currentNamespace
	namespaceMutex.RUnlock()

	entry := logrus.NewEntry(Log)
	if ns != "" {
		entry = entry.WithField(NamespaceKey, ns)
	}
	if id != "" {
		entry = entry.WithField(IDKey, id)
	}
	return entry
}

// Debug logs a message at debug level.
func Debug(args ...any) {
	Log.Debug(args...)
}

// Info logs a message at info level.
func Info(args ...any) {
	Log.Info(args...)
}

// Warn logs a message at warning level.
func Warn(args ...any) {
	Log.Warn(args...)
}

// Error logs a message at error level.
func Error(args ...any) {
	Log.Error(args...)
}

// Fatal logs a message at fatal level and exits.
func Fatal(args ...any) {
	Log.Fatal(args...)
}

// Panic logs a message at panic level and panics.
func Panic(args ...any) {
	Log.Panic(args...)
}

// DebugLevel logs a message at debug level (alias for Debug).
func DebugLevel(args ...any) {
	Log.Debug(args...)
}

// InfoLevel logs a message at info level (alias for Info).
func InfoLevel(args ...any) {
	Log.Info(args...)
}

// WarnLevel logs a message at warning level (alias for Warn).
func WarnLevel(args ...any) {
	Log.Warn(args...)
}

// ErrorLevel logs a message at error level (alias for Error).
func ErrorLevel(args ...any) {
	Log.Error(args...)
}

// DebugLevelf logs a formatted message at debug level (alias for Debugf).
func DebugLevelf(format string, args ...any) {
	Log.Debugf(format, args...)
}

// InfoLevelf logs a formatted message at info level (alias for Infof).
func InfoLevelf(format string, args ...any) {
	Log.Infof(format, args...)
}

// WarnLevelf logs a formatted message at warning level (alias for Warnf).
func WarnLevelf(format string, args ...any) {
	Log.Warnf(format, args...)
}

// ErrorLevelf logs a formatted message at error level (alias for Errorf).
func ErrorLevelf(format string, args ...any) {
	Log.Errorf(format, args...)
}

// Tracef logs a message at TRACE level.
//
// This function has DIFFERENT implementations in debug and release builds:
//
//   - Debug builds (-tags debug): Outputs to both containerd log fifo AND log file
//                                with "[TRACE]" prefix for easy identification
//
//   - Release builds: This is a NO-OP function. The compiler optimizes away
//                     all calls to Tracef, resulting in:
//                       * Zero runtime overhead
//                       * No string formatting
//                       * No memory allocation
//
// Usage guidelines (when to use Tracef vs Debugf/Infof):
//
//   USE Tracef for:
//   - Test-only diagnostics (fd values, byte-level tracking, raw epoll events)
//   - High-frequency events that don't help in production debugging
//   - Implementation details that don't affect problem diagnosis
//   - Variable values that are only useful during development
//
//   DO NOT use Tracef for (use Debugf instead):
//   - Functional debugging info (function entry/exit with context)
//   - State changes that help understand what's happening
//   - API calls and their parameters
//
//   DO NOT use Tracef for (use Infof/Warnf/Errorf instead):
//   - State transitions (use Infof)
//   - Recoverable issues (use Warnf)
//   - Failures (use Errorf)
func Tracef(format string, args ...any) {
	// Implemented differently in logger_debug.go and logger_release.go
	tracefImpl(format, args...)
}

// Debugf logs a formatted message at debug level.
func Debugf(format string, args ...any) {
	Log.Debugf(format, args...)
}

// Infof logs a formatted message at info level.
func Infof(format string, args ...any) {
	Log.Infof(format, args...)
}

// Warnf logs a formatted message at warning level.
func Warnf(format string, args ...any) {
	Log.Warnf(format, args...)
}

// Errorf logs a formatted message at error level.
func Errorf(format string, args ...any) {
	Log.Errorf(format, args...)
}

// Fatalf logs a formatted message at fatal level and exits.
func Fatalf(format string, args ...any) {
	Log.Fatalf(format, args...)
}

// Panicf logs a formatted message at panic level and panics.
func Panicf(format string, args ...any) {
	Log.Panicf(format, args...)
}

// Pretty logs a formatted message at debug level (alias for Debugf).
// This function exists for backward compatibility.
func Pretty(format string, args ...any) {
	Log.Debugf(format, args...)
}
