package log

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestEnsureLogDirectoryCreatesBrokenSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	varDir := filepath.Join(root, "var")
	volatileDir := filepath.Join(varDir, "volatile")
	logSymlink := filepath.Join(varDir, "log")
	logFile := filepath.Join(logSymlink, "mica", "mica-runtime.log")

	require.NoError(t, os.MkdirAll(varDir, 0o755))
	require.NoError(t, os.MkdirAll(volatileDir, 0o755))
	require.NoError(t, os.Symlink("volatile/log", logSymlink))

	err := ensureLogDirectory(logFile)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(volatileDir, "log", "mica"))
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestOpenContainerdLogUsesCurrentWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "log")
	require.NoError(t, os.WriteFile(logPath, nil, 0o644))
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.Chdir(oldWD))
	}()
	require.NoError(t, os.Chdir(root))

	writer, err := openContainerdLog()
	require.NoError(t, err)
	_, err = writer.Write([]byte("hello"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Equal(t, "hello", string(data))
}

func TestOpenContainerdLogRespectsEnvOverride(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "shim-containerd.log")
	require.NoError(t, os.WriteFile(logPath, nil, 0o644))

	t.Setenv(ContainerdLogPathEnv, "  "+logPath+"  ")
	writer, err := openContainerdLog()
	require.NoError(t, err)

	_, err = writer.Write([]byte("override"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Equal(t, "override", string(data))
}

func TestAddContextFieldsAddsMissingValues(t *testing.T) {
	oldNamespace := GetNamespace()
	oldContainerID := GetContainerID()
	defer SetNamespace(oldNamespace)
	defer SetContainerID(oldContainerID)

	SetNamespace("ns-test")
	SetContainerID("container-test")
	fields := logrus.Fields{}

	addContextFields(fields)

	require.Equal(t, "ns-test", fields[NamespaceKey])
	require.Equal(t, "container-test", fields[IDKey])
}

func TestAddContextFieldsPreservesExistingValues(t *testing.T) {
	oldNamespace := GetNamespace()
	oldContainerID := GetContainerID()
	defer SetNamespace(oldNamespace)
	defer SetContainerID(oldContainerID)

	SetNamespace("ns-test")
	SetContainerID("container-test")
	fields := logrus.Fields{
		NamespaceKey: "existing-ns",
		IDKey:        "existing-container",
	}

	addContextFields(fields)

	require.Equal(t, "existing-ns", fields[NamespaceKey])
	require.Equal(t, "existing-container", fields[IDKey])
}

func TestInitializeDoesNotStoreInvalidConfig(t *testing.T) {
	currentConfigMutex.Lock()
	oldConfig := currentConfig
	currentConfig = DefaultConfig()
	currentConfigMutex.Unlock()
	defer func() {
		currentConfigMutex.Lock()
		currentConfig = oldConfig
		currentConfigMutex.Unlock()
	}()

	err := Initialize(&Config{Log: LogConfig{Level: "not-a-level", File: DefaultLogFile}})
	require.Error(t, err)

	currentConfigMutex.RLock()
	got := currentConfig
	currentConfigMutex.RUnlock()
	require.NotNil(t, got)
	require.Equal(t, "info", got.Log.Level)
}

func TestInitializeLoggerReplacesHooks(t *testing.T) {
	oldHooks := Log.Hooks
	defer func() {
		Log.ReplaceHooks(oldHooks)
	}()

	cfg := DefaultConfig()
	cfg.Log.File = filepath.Join(t.TempDir(), "micrun.log")

	require.NoError(t, initializeLogger(cfg))
	firstCount := countLoggerHooks(Log.Hooks)
	require.NotZero(t, firstCount)

	require.NoError(t, initializeLogger(cfg))
	require.Equal(t, firstCount, countLoggerHooks(Log.Hooks))
}

func TestLoadConfigUsesEnvironmentOverridesAndDefaults(t *testing.T) {
	workDir := t.TempDir()
	configPath := filepath.Join(workDir, "logger.json")

	configBody := `{"log":{"level":" debug ","file":"   "}}`
	require.NoError(t, os.WriteFile(configPath, []byte(configBody), 0o644))

	customLogFile := filepath.Join(workDir, "micrun.log")
	t.Setenv(DefaultLogConfigPathEnv, "  "+configPath+"  ")
	t.Setenv(DefaultLogFileEnv, "  "+customLogFile+"  ")

	cfg, err := LoadConfig("")
	require.NoError(t, err)
	require.Equal(t, "debug", cfg.Log.Level)
	require.Equal(t, customLogFile, cfg.Log.File)
}

func TestLoadConfigPathFallsBackToDefaultWhenWhitespace(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "logger.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{"log":{"file":"/tmp/ignore.log"}}`), 0o644))
	t.Setenv(DefaultLogConfigPathEnv, "   "+configPath+"   ")

	cfg, err := LoadConfig("   ")
	require.NoError(t, err)
	require.Equal(t, "/tmp/ignore.log", cfg.Log.File)
}

func countLoggerHooks(hooks logrus.LevelHooks) int {
	count := 0
	for _, levelHooks := range hooks {
		count += len(levelHooks)
	}
	return count
}

type testFailingWriteCloser struct {
}

func (f *testFailingWriteCloser) Write(_ []byte) (int, error) {
	return 0, nil
}

func (f *testFailingWriteCloser) Close() error {
	return os.ErrClosed
}

func TestInitializeStoresConfigSnapshot(t *testing.T) {
	currentConfigMutex.Lock()
	oldConfig := currentConfig
	currentConfig = nil
	currentConfigMutex.Unlock()
	oldHooks := Log.Hooks
	oldLevel := Log.GetLevel()
	defer func() {
		currentConfigMutex.Lock()
		currentConfig = oldConfig
		currentConfigMutex.Unlock()
		Log.ReplaceHooks(oldHooks)
		Log.SetLevel(oldLevel)
	}()

	cfg := DefaultConfig()
	cfg.Log.Level = "debug"
	cfg.Log.File = filepath.Join(t.TempDir(), "micrun.log")

	require.NoError(t, Initialize(cfg))
	cfg.Log.Level = "error"

	currentConfigMutex.RLock()
	got := currentConfig
	currentConfigMutex.RUnlock()
	require.NotNil(t, got)
	require.Equal(t, "debug", got.Log.Level)
}

func TestNeedsQuoting(t *testing.T) {
	tests := map[string]bool{
		"plain":       false,
		"with space":  true,
		"quote\"":     true,
		"back\\slash": true,
		"line\nbreak": true,
	}
	for input, want := range tests {
		require.Equal(t, want, needsQuoting(input), input)
	}
}

func TestFormatContainerdValueEscapesSpecialCharacters(t *testing.T) {
	tests := map[string]string{
		"plain":       "plain",
		"with space":  `"with space"`,
		"quote\"":     `"quote\""`,
		"back\\slash": `"back\\slash"`,
		"line\nbreak": `"line\nbreak"`,
	}
	for input, want := range tests {
		require.Equal(t, want, formatContainerdValue(input), input)
	}
}

func TestContainerdFormatterOrdersContextFields(t *testing.T) {
	entry := &logrus.Entry{
		Time:    time.Date(2026, 4, 27, 1, 2, 3, 4, time.UTC),
		Level:   logrus.InfoLevel,
		Message: "hello world",
		Data: logrus.Fields{
			"extra":      "value",
			NamespaceKey: "ns1",
			IDKey:        "container1",
		},
	}

	out, err := (&containerdFormatter{}).Format(entry)
	require.NoError(t, err)
	line := string(out)
	require.Contains(t, line, `msg="hello world"`)
	require.Less(t, strings.Index(line, " id=container1"), strings.Index(line, " namespace=ns1"))
	require.Contains(t, line, " extra=value")
}

func TestContainerdFormatterEscapesMessageAndFields(t *testing.T) {
	entry := &logrus.Entry{
		Time:    time.Date(2026, 4, 27, 1, 2, 3, 4, time.UTC),
		Level:   logrus.WarnLevel,
		Message: "quoted \"message\"\nnext",
		Data: logrus.Fields{
			IDKey:        `container\1`,
			NamespaceKey: "default namespace",
			"hint":       "check /var/log/mica/mica-runtime.log",
		},
	}

	out, err := (&containerdFormatter{}).Format(entry)
	require.NoError(t, err)
	line := string(out)
	require.Contains(t, line, `msg="quoted \"message\"\nnext"`)
	require.Contains(t, line, ` id="container\\1"`)
	require.Contains(t, line, ` namespace="default namespace"`)
	require.Contains(t, line, ` hint="check /var/log/mica/mica-runtime.log"`)
}
