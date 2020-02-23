// Package log provides the both file and console (general) logging capabilities
// to spacemesh modules such as app and identity.
package log

import (
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"io"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

const mainLoggerName = "00000.defaultLogger"

// determine the level of messages we show.
var debugMode = false

// should we format out logs in json
var jsonLog = false

var DebugLevel = zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
	return lvl >= zapcore.DebugLevel
})

var InfoLevel = zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
	return lvl >= zapcore.InfoLevel
})

var ErrorLevel = zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
	return lvl >= zapcore.ErrorLevel
})

func logLevel() zap.LevelEnablerFunc {
	if debugMode {
		return DebugLevel
	} else {
		return InfoLevel
	}
}

func LogLvl() zapcore.Level {
	if debugMode {
		return zapcore.DebugLevel
	} else {
		return zapcore.InfoLevel
	}
}

type Logger interface {
	Info(format string, args ...interface{})
	Debug(format string, args ...interface{})
	Error(format string, args ...interface{})
	Warning(format string, args ...interface{})
	WithName(prefix string) Log
}

func encoder() zapcore.Encoder {
	if jsonLog {
		return zapcore.NewJSONEncoder(zap.NewDevelopmentEncoderConfig())
	}
	return zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
}

// AppLog is the local app singleton logger.
var AppLog Log

func init() {
	// create a basic temp os.Stdout logger
	// This logger is used until the app calls InitSpacemeshLoggingSystem().
	AppLog = NewDefault(mainLoggerName)
}

// DebugMode sets log debug level
func DebugMode(mode bool) {
	debugMode = mode
}

func JSONLog(b bool) {
	jsonLog = b
}

// New creates a logger for a module. e.g. p2p instance logger.
func New(module string, dataFolderPath string, logFileName string) Log {
	var cores []zapcore.Core

	consoleSyncer := zapcore.AddSync(os.Stdout)
	enc := encoder()

	cores = append(cores, zapcore.NewCore(enc, consoleSyncer, logLevel()))

	if dataFolderPath != "" && logFileName != "" {
		wr := getFileWriter(dataFolderPath, logFileName)
		fs := zapcore.AddSync(wr)
		cores = append(cores, zapcore.NewCore(enc, fs, DebugLevel))
	}

	core := zapcore.NewTee(cores...)

	log := zap.New(core)
	log = log.Named(module)
	lvl := zap.NewAtomicLevelAt(LogLvl())
	return Log{log, log.Sugar(), &lvl}
}

// New creates a logger for a module. e.g. p2p instance logger.
func NewWithErrorLevel(module string, dataFolderPath string, logFileName string) Log {
	var cores []zapcore.Core

	consoleSyncer := zapcore.AddSync(os.Stdout)
	enc := encoder()

	cores = append(cores, zapcore.NewCore(enc, consoleSyncer, ErrorLevel))

	if dataFolderPath != "" && logFileName != "" {
		wr := getFileWriter(dataFolderPath, logFileName)
		fs := zapcore.AddSync(wr)
		cores = append(cores, zapcore.NewCore(enc, fs, DebugLevel))
	}

	core := zapcore.NewTee(cores...)

	log := zap.New(core)
	log = log.Named(module)
	lvl := zap.NewAtomicLevelAt(zap.ErrorLevel)
	return Log{log, log.Sugar(), &lvl}
}

func NewDefault(module string) Log {
	return New(module, "", "")
}

// getBackendLevelWithFileBackend returns backends level including log file backend
func getFileWriter(dataFolderPath, logFileName string) io.Writer {
	fileName := filepath.Join(dataFolderPath, logFileName)

	fileLogger := &lumberjack.Logger{
		Filename:   fileName,
		MaxSize:    500, // megabytes
		MaxBackups: 3,
		MaxAge:     28, // days
		Compress:   false,
	}

	return fileLogger
}

// InitSpacemeshLoggingSystem initializes app logging system.
func InitSpacemeshLoggingSystem(dataFolderPath string, logFileName string) {
	AppLog = NewDefault(mainLoggerName)
}

// public wrappers abstracting away logging lib impl

// Info prints formatted info level log message.
func Info(msg string, args ...interface{}) {
	AppLog.Info(msg, args...)
}

// Debug prints formatted debug level log message.
func Debug(msg string, args ...interface{}) {
	AppLog.Debug(msg, args...)
}

// Error prints formatted error level log message.
func Error(msg string, args ...interface{}) {
	AppLog.Error(msg, args...)
}

// Warning prints formatted warning level log message.
func Warning(msg string, args ...interface{}) {
	AppLog.Warning(msg, args...)
}

func With() fieldLogger {
	return fieldLogger{AppLog.logger}
}

func Event() fieldLogger {
	return AppLog.Event()
}

func Panic(msg string, args ...interface{}) {
	AppLog.Panic(msg, args...)
}
