// Package log provides the both file and console (general) logging capabilities
// to spacemesh modules such as app and node.
package log

import (
	"fmt"
	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/op/go-logging.v1"
	"os"
	"path/filepath"
)

// SpaceMeshLogger is a custom logger for Spacemesh project.
type SpaceMeshLogger struct {
	Logger *logging.Logger
}

// uLogger is per local node logger.
var ulogger *SpaceMeshLogger

func init() {
	// create a basic temp os.Stdout logger
	// This logger is going to be used by tests when an app was is created
	log := logging.MustGetLogger("app")
	log.ExtraCalldepth = 1
	logFormat := logging.MustStringFormatter(`%{color}%{time:15:04:05.000} %{shortpkg} %{shortfunc} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}`)
	backend := logging.NewLogBackend(os.Stdout, "", 0)
	backendFormatter := logging.NewBackendFormatter(backend, logFormat)
	logging.SetBackend(backendFormatter)
	ulogger = &SpaceMeshLogger{Logger: log}

}

// CreateLogger creates a logger for a module.
func CreateLogger(module string, dataFolderPath string, logFileName string) *logging.Logger {

	log := logging.MustGetLogger(module)
	log.ExtraCalldepth = 1
	logFormat := logging.MustStringFormatter(` %{color}%{time:15:04:05.000} %{shortpkg} %{shortfunc} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}`)
	backend := logging.NewLogBackend(os.Stderr, module, 0)
	backendFormatter := logging.NewBackendFormatter(backend, logFormat)

	fileName := filepath.Join(dataFolderPath, logFileName)

	fileLogger := &lumberjack.Logger{
		Filename:   fileName,
		MaxSize:    500, // megabytes
		MaxBackups: 3,
		MaxAge:     28, // days
		Compress:   false,
	}

	fileLoggerBackend := logging.NewLogBackend(fileLogger, "", 0)
	logFileFormat := logging.MustStringFormatter(` %{time:15:04:05.000} %{level:.4s}-%{id:03x} %{shortpkg}.%{shortfunc} ▶ %{message}`)
	fileBackendFormatter := logging.NewBackendFormatter(fileLoggerBackend, logFileFormat)

	logging.SetBackend(backendFormatter, fileBackendFormatter)

	return log
}

// InitSpaceMeshLoggingSystem initializes app logging system.
func InitSpaceMeshLoggingSystem(dataFolderPath string, logFileName string) {

	log := logging.MustGetLogger("app")

	// we wrap all log calls so we need to add 1 to call depth
	log.ExtraCalldepth = 1

	logFormat := logging.MustStringFormatter(`%{color}%{time:15:04:05.000} %{shortpkg} %{shortfunc} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}`)
	backend := logging.NewLogBackend(os.Stderr, "", 0)
	backendFormatter := logging.NewBackendFormatter(backend, logFormat)

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

	logging.SetBackend(backendFormatter, fileBackendFormatter)

	ulogger = &SpaceMeshLogger{Logger: log}
}

// public wrappers abstracting away logging lib impl

// Info prints formatted info level log message.
func Info(format string, args ...interface{}) {
	ulogger.Logger.Info(format, args...)
}

// Debug prints formatted debug level log message.
func Debug(format string, args ...interface{}) {
	ulogger.Logger.Debug(format, args...)
}

// Error prints formatted error level log message.
func Error(format string, args ...interface{}) {
	ulogger.Logger.Error(format, args...)
}

// Warning prints formatted warning level log message.
func Warning(format string, args ...interface{}) {
	ulogger.Logger.Warning(format, args...)
}

// PrettyID formats ID.
func PrettyID(id string) string {
	m := 6
	if len(id) < m {
		m = len(id)
	}
	return fmt.Sprintf("<ID %s>", id[:m])
}
