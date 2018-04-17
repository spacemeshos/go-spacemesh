// Package log provides the both file and console (general) logging capabilities
// to spacemesh modules such as app and node.
package log

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/op/go-logging.v1"
)

// SpacemeshLogger is a custom logger.
type SpacemeshLogger struct {
	Logger *logging.Logger
}

// smlogger is the local app singleton logger.
var smLogger *SpacemeshLogger

func init() {
	// create a basic temp os.Stdout logger
	// This logger is used until the app calls InitSpacemeshLoggingSystem().
	log := logging.MustGetLogger("app")
	log.ExtraCalldepth = 1
	logFormat := logging.MustStringFormatter(` %{color}%{level:.4s} %{id:03x} %{time:15:04:05.000} ▶%{color:reset} %{message}`)
	backend := logging.NewLogBackend(os.Stdout, "<APP>", 0)
	backendFormatter := logging.NewBackendFormatter(backend, logFormat)
	logging.SetBackend(backendFormatter)
	smLogger = &SpacemeshLogger{Logger: log}
}

// CreateLogger creates a logger for a module. e.g. local node logger.
func CreateLogger(module string, dataFolderPath string, logFileName string) *logging.Logger {

	log := logging.MustGetLogger(module)
	log.ExtraCalldepth = 1
	logFormat := logging.MustStringFormatter(` %{color:reset}%{color}%{level:.4s} %{id:03x} %{time:15:04:05.000} %{shortpkg}.%{shortfunc} ▶%{color:reset} %{message}`)
	// module name is set is message prefix

	backends := []logging.Backend{}

	backend := logging.NewLogBackend(os.Stdout, module, 0)
	backendFormatter := logging.NewBackendFormatter(backend, logFormat)
	backends = append(backends, logging.AddModuleLevel(backendFormatter))

	if dataFolderPath != "" && logFileName != "" {
		fileName := filepath.Join(dataFolderPath, logFileName)

		fileLogger := &lumberjack.Logger{
			Filename:   fileName,
			MaxSize:    500, // megabytes
			MaxBackups: 3,
			MaxAge:     28, // days
			Compress:   false,
		}

		fileLoggerBackend := logging.NewLogBackend(fileLogger, "", 0)
		logFileFormat := logging.MustStringFormatter(`%{time:15:04:05.000} %{level:.4s} %{id:03x} %{shortpkg}.%{shortfunc} ▶ %{message}`)
		fileBackendFormatter := logging.NewBackendFormatter(fileLoggerBackend, logFileFormat)
		backends = append(backends, logging.AddModuleLevel(fileBackendFormatter))
	}

	log.SetBackend(logging.MultiLogger(backends...))

	return log
}

// InitSpacemeshLoggingSystem initializes app logging system.
func InitSpacemeshLoggingSystem(dataFolderPath string, logFileName string) {

	log := logging.MustGetLogger("app")

	// we wrap all log calls so we need to add 1 to call depth
	log.ExtraCalldepth = 1

	logFormat := logging.MustStringFormatter(` %{color}%{level:.4s} %{id:03x} %{time:15:04:05.000} %{shortpkg}.%{shortfunc}%{color:reset} ▶ %{message}`)

	backends := []logging.Backend{}

	backend := logging.NewLogBackend(os.Stdout, "<APP>", 0)
	backendFormatter := logging.NewBackendFormatter(backend, logFormat)
	backends = append(backends, logging.AddModuleLevel(backendFormatter))

	if dataFolderPath != "" && logFileName != "" {
		fileName := filepath.Join(dataFolderPath, logFileName)

		fileLogger := &lumberjack.Logger{
			Filename:   fileName,
			MaxSize:    500, // megabytes
			MaxBackups: 3,
			MaxAge:     28, // days
			Compress:   false,
		}

		fileLoggerBackend := logging.NewLogBackend(fileLogger, "", 0)
		logFileFormat := logging.MustStringFormatter(`%{time:15:04:05.000} %{level:.4s}-%{id:03x} %{shortpkg}.%{shortfunc} ▶ %{message}`)
		fileBackendFormatter := logging.NewBackendFormatter(fileLoggerBackend, logFileFormat)
		backends = append(backends, logging.AddModuleLevel(fileBackendFormatter))
	}

	log.SetBackend(logging.MultiLogger(backends...))

	smLogger = &SpacemeshLogger{Logger: log}
}

// public wrappers abstracting away logging lib impl

// Info prints formatted info level log message.
func Info(format string, args ...interface{}) {
	smLogger.Logger.Info(format, args...)
}

// Debug prints formatted debug level log message.
func Debug(format string, args ...interface{}) {
	smLogger.Logger.Debug(format, args...)
}

// Error prints formatted error level log message.
func Error(format string, args ...interface{}) {
	smLogger.Logger.Error(format, args...)
}

// Warning prints formatted warning level log message.
func Warning(format string, args ...interface{}) {
	smLogger.Logger.Warning(format, args...)
}

// PrettyID formats ID.
func PrettyID(id string) string {
	m := 6
	if len(id) < m {
		m = len(id)
	}
	return fmt.Sprintf("<ID %s>", id[:m])
}
