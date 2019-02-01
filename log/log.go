// Package log provides the both file and console (general) logging capabilities
// to spacemesh modules such as app and identity.
package log

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/op/go-logging.v1"
)

// Log is an exported type that embeds our logger.
// logging library can be replaced as long as it implements same functionality used across the project.
type Log struct {
	*logging.Logger
}

// smlogger is the local app singleton logger.
var AppLog Log
var debugMode = false

func init() {

	// create a basic temp os.Stdout logger
	// This logger is used until the app calls InitSpacemeshLoggingSystem().
	log := logging.MustGetLogger("app")
	log.ExtraCalldepth = 1
	logFormat := ` %{color}%{level:.4s} %{id:03x} %{time:15:04:05.000} ▶%{color:reset} %{message}`
	leveledBackend := getBackendLevel("app", "<APP>", logFormat)
	log.SetBackend(leveledBackend)
	AppLog = Log{Logger: log}
}

// getAllBackend returns level backends with an exception to Debug leve
// which is only returned if the flag debug is set
func getBackendLevel(module, prefix, format string) logging.LeveledBackend {
	logFormat := logging.MustStringFormatter(format)

	backend := logging.NewLogBackend(os.Stdout, prefix, 0)
	backendFormatter := logging.NewBackendFormatter(backend, logFormat)
	leveledBackend := logging.AddModuleLevel(backendFormatter)

	if debugMode {
		leveledBackend.SetLevel(logging.DEBUG, module)
	} else {
		leveledBackend.SetLevel(logging.INFO, module)
	}

	return leveledBackend
}

// DebugMode sets log debug level
func DebugMode(mode bool) {
	debugMode = mode
}

// New creates a logger for a module. e.g. p2p instance logger.
func New(module string, dataFolderPath string, logFileName string) Log {
	log := logging.MustGetLogger(module)
	log.ExtraCalldepth = 1
	logFormat := ` %{color:reset}%{color}%{level:.4s} %{id:03x} %{time:15:04:05.000} %{shortpkg}.%{shortfunc} ▶%{color:reset} %{message}`
	logFileFormat := `%{time:15:04:05.000} %{level:.4s} %{id:03x} %{shortpkg}.%{shortfunc} ▶ %{message}`
	// module name is set is message prefix

	backends := getBackendLevelWithFileBackend(module, module, logFormat, logFileFormat, dataFolderPath, logFileName)

	log.SetBackend(logging.MultiLogger(backends...))

	return Log{log}
}

// getBackendLevelWithFileBackend returns backends level including log file backend
func getBackendLevelWithFileBackend(module, prefix, logFormat, fileFormat, dataFolderPath, logFileName string) []logging.Backend {
	leveledBackends := []logging.Backend{getBackendLevel(module, prefix, logFormat)}

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
		logFileFormat := logging.MustStringFormatter(fileFormat)
		fileBackendFormatter := logging.NewBackendFormatter(fileLoggerBackend, logFileFormat)
		leveledBackends = append(leveledBackends, logging.AddModuleLevel(fileBackendFormatter))
	}

	return leveledBackends
}

// InitSpacemeshLoggingSystem initializes app logging system.
func InitSpacemeshLoggingSystem(dataFolderPath string, logFileName string) {
	log := logging.MustGetLogger("app")

	// we wrap all log calls so we need to add 1 to call depth
	log.ExtraCalldepth = 1

	logFormat := ` %{color}%{level:.4s} %{id:03x} %{time:15:04:05.000} %{shortpkg}.%{shortfunc}%{color:reset} ▶ %{message}`
	logFileFormat := `%{time:15:04:05.000} %{level:.4s}-%{id:03x} %{shortpkg}.%{shortfunc} ▶ %{message}`

	backends := getBackendLevelWithFileBackend("app", "<APP>", logFormat, logFileFormat, dataFolderPath, logFileName)

	log.SetBackend(logging.MultiLogger(backends...))

	AppLog = Log{log}
}

// public wrappers abstracting away logging lib impl

// Info prints formatted info level log message.
func Info(format string, args ...interface{}) {
	AppLog.Info(format, args...)
}

// Debug prints formatted debug level log message.
func Debug(format string, args ...interface{}) {
	AppLog.Debug(format, args...)
}

// Error prints formatted error level log message.
func Error(format string, args ...interface{}) {
	AppLog.Error(format, args...)
}

// Warning prints formatted warning level log message.
func Warning(format string, args ...interface{}) {
	AppLog.Warning(format, args...)
}

// PrettyID formats ID.
func PrettyID(id string) string {
	m := 6
	if len(id) < m {
		m = len(id)
	}
	return fmt.Sprintf("<ID %s>", id[:m])
}
