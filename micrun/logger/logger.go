//go:build !debug
// +build !debug

package log

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const debugFileName = "/var/log/mica/mica-runtime.log"

var (
	Log = logrus.New()
)

// Set the default configuration for systemd compatibility.
func init() {
	Log.SetOutput(os.Stderr)
	Log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "01-02 15:04:05",
	})
}

// Config represents the logger configuration.
type Config struct {
	// Level is the minimum log level.
	Level string
	// Format is the log format (text or json).
	Format string
	// Output is the log output file path. If empty, use stderr.
	Output string
	// Debug enables debug mode.
	Debug bool
}

func Init(config *Config) error {
	if config == nil {
		return nil
	}

	if config.Level != "" {
		level, err := logrus.ParseLevel(config.Level)
		if err != nil {
			return err
		}
		Log.SetLevel(level)
	}

	switch config.Format {
	case "json":
		Log.SetFormatter(&logrus.JSONFormatter{})
	case "text", "":
		Log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "01-02 15:04:05",
		})
	}

	if config.Output != "" {
		file, err := os.OpenFile(config.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		Log.SetOutput(file)
	}

	if config.Debug {
		Log.SetLevel(logrus.DebugLevel)
		Log.SetReportCaller(true)
		Log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "01-02 15:04:05",
			CallerPrettyfier: func(f *runtime.Frame) (string, string) {
				_, file, _, _ := runtime.Caller(0)
				prefix := filepath.Dir(file) + "/"
				function := strings.TrimPrefix(f.Function, prefix) + "()"
				fileLine := strings.TrimPrefix(f.File, prefix) + ":" + strconv.Itoa(f.Line)
				return function, fileLine
			},
		})
	}

	return nil
}

func WithField(key string, value interface{}) *logrus.Entry {
	return Log.WithField(key, value)
}

func WithFields(fields logrus.Fields) *logrus.Entry {
	return Log.WithFields(fields)
}

func WithError(err error) *logrus.Entry {
	return Log.WithError(err)
}

func Debug(args ...interface{}) {
	Log.Debug(args...)
}

func Debugf(format string, args ...interface{}) {
	prefix := getDebugInfoPrefix()
	Log.Debugf(prefix+format+"\n", args...)
}

func Info(args ...interface{}) {
	Log.Info(args...)
}

func Warn(args ...interface{}) {
	Log.Warn(args...)
}

func Error(args ...interface{}) {
	Log.Error(args...)
}

func Fatal(args ...interface{}) {
	Log.Fatal(args...)
}

func Panic(args ...interface{}) {
	Log.Panic(args...)
}

// func locatedebugf(format string, args ...interface{}) {
// 	prefix := getdebuginfoprefix()
// 	debugf(prefix+format+"\n", args...)
// }

func Infof(format string, args ...interface{}) {
	Log.Infof(format, args...)
}

func Warnf(format string, args ...interface{}) {
	Log.Warnf(format, args...)
}

func Errorf(format string, args ...interface{}) {
	Log.Errorf(format, args...)
}

func Fatalf(format string, args ...interface{}) {
	Log.Fatalf(format, args...)
}

func Panicf(format string, args ...interface{}) {
	Log.Panicf(format, args...)
}

// FatalWithCleanup logs a fatal error and executes a cleanup function before exiting.
func FatalWithCleanup(cleanup func(), args ...interface{}) {
	if cleanup != nil {
		cleanup()
	}
	Log.Fatal(args...)
}

// BUG: facing multiinstance issue, there will be an conflict when cleaning debug file
// solution:
// create debug file with suffix <containerID>;
// we need to parse ID in task service
func CleanDebugFile() error {
	f, err := os.OpenFile(debugFileName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	_, err = fmt.Fprintf(f, "\n\n%s", timestamp)
	return err
}

// getDebugInfoPrefix gets the prefix for debug information.
// This is used for debug points that need to be traced.
func getDebugInfoPrefix() string {
	var prefix = ""
	pcParent, _, _, ok := runtime.Caller(3)
	if ok {
		fullFuncName := runtime.FuncForPC(pcParent).Name()
		funcName := filepath.Base(fullFuncName)
		prefix += fmt.Sprintf("%s-> ", funcName)
	}
	pc, _, _, ok := runtime.Caller(2)
	if ok {
		var caller string
		fullFuncName := runtime.FuncForPC(pc).Name()
		file, line := runtime.FuncForPC(pc).FileLine(pc)
		file = filepath.Base(file)
		caller = fullFuncName
		caller = "\033[32m" + caller + "\033[0m"
		prefix += fmt.Sprintf("[\033[33m%s:%d\033[0m] %s \n\t", file, line, caller)
	}
	return prefix
}

// FDebugf writes a debug message to the debug file.
func FDebugf(format string, args ...interface{}) error {
	f, err := os.OpenFile(debugFileName, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	prefix := getDebugInfoPrefix()
	_, err = fmt.Fprintf(f, prefix+format+"\n", args...)
	return err
}

func Pretty(format string, args ...interface{}) {
	Debugf(format, args...)
}
