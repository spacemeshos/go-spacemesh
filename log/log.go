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

var debugLevel = zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
	return lvl >= zapcore.DebugLevel
})

var infoLevel = zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
	return lvl >= zapcore.InfoLevel
})

func logLevel() zap.LevelEnablerFunc {
	if debugMode {
		return debugLevel
	}
	return infoLevel
}

// Level returns the zapcore level of logging.
func Level() zapcore.Level {
	if debugMode {
		return zapcore.DebugLevel
	}
	return zapcore.InfoLevel
}

// Logger is an interface for our logging API.
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

// JSONLog sets logging to be in JSON format or not.
func JSONLog(b bool) {
	jsonLog = b
}

// NewWithLevel creates a logger with a fixed level
func NewWithLevel(module string, level zap.AtomicLevel, hooks ...func(zapcore.Entry) error) Log {
	consoleSyncer := zapcore.AddSync(os.Stdout)
	enc := encoder()
	consoleCore := zapcore.NewCore(enc, consoleSyncer, zap.LevelEnablerFunc(level.Enabled))
	core := zapcore.RegisterHooks(consoleCore, hooks...)
	log := zap.New(core).Named(module)
	return Log{log}
}

// New creates a logger for a module. e.g. p2p instance logger.
func New(module string, dataFolderPath string, logFileName string, hooks ...func(zapcore.Entry) error) Log {
	var cores []zapcore.Core

	consoleSyncer := zapcore.AddSync(os.Stdout)
	enc := encoder()

	cores = append(cores, zapcore.NewCore(enc, consoleSyncer, logLevel()))

	if dataFolderPath != "" && logFileName != "" {
		wr := getFileWriter(dataFolderPath, logFileName)
		fs := zapcore.AddSync(wr)
		cores = append(cores, zapcore.NewCore(enc, fs, debugLevel))
	}

	core := zapcore.RegisterHooks(zapcore.NewTee(cores...), hooks...)

	log := zap.New(core)
	log = log.Named(module)
	return Log{log}
}

// NewDefault creates a Log without file output
func NewDefault(module string) Log {
	return NewWithLevel(module, zap.NewAtomicLevelAt(Level()))
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
func InitSpacemeshLoggingSystem() {
	AppLog = NewDefault(mainLoggerName)
}

// InitSpacemeshLoggingSystemWithHooks sets up a logging system with one or more
// registered hooks
func InitSpacemeshLoggingSystemWithHooks(hooks ...func(zapcore.Entry) error) {
	AppLog = New(mainLoggerName, "", "", hooks...)
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

// With returns a FieldLogger which you can append fields to.
func With() FieldLogger {
	return FieldLogger{AppLog.logger}
}

// Event returns a field logger with the Event field set to true.
func Event() FieldLogger {
	return AppLog.Event()
}

// Panic writes the log message and then panics.
func Panic(msg string, args ...interface{}) {
	AppLog.Panic(msg, args...)
}
