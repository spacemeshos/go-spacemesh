package log

import (
	"gopkg.in/op/go-logging.v1"
	"os"
	"gopkg.in/natefinch/lumberjack.v2"
	"path/filepath"
)

type UnrulyLogger struct {
	Logger *logging.Logger
}

var ulogger *UnrulyLogger

func InitUnrulyLoggingSystem(dataFolderPath string, logFileName string) {

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
	logFileFormat := logging.MustStringFormatter(`%{time:15:04:05.000} %{shortpkg}.%{shortfunc} ▶ %{level:.4s} %{id:03x} %{message}`)
	fileBackendFormatter := logging.NewBackendFormatter(fileLoggerBackend, logFileFormat)

	logging.SetBackend(backendFormatter, fileBackendFormatter)

	ulogger = &UnrulyLogger{Logger: log}
}

// public wrappers abstracting away logging lib impl

// Standard info level logging
func Info(format string, args ...interface{}) {
	ulogger.Logger.Info(format, args...)
}

// Standard debug level logging
func Debug(format string, args ...interface{}) {
	ulogger.Logger.Debug(format, args...)
}

// Standard error level logging
func Error(format string, args ...interface{}) {
	ulogger.Logger.Error(format, args...)
}

// Standard warning level logging
func Warning(format string, args ...interface{}) {
	ulogger.Logger.Warning(format, args...)
}
