// Package log provides the both file and console (general) logging capabilities
// to spacemesh modules such as app and identity.
package log

import (
	"context"
	"io"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const mainLoggerName = "00000.defaultLogger"

// determine the level of messages we show.
var debugMode = false

// should we format out logs in json
var jsonLog = false

// where logs go by default
var logwriter io.Writer

// default encoders
var defaultEncoder = zap.NewDevelopmentEncoderConfig()

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
	With() FieldLogger
	WithContext(context.Context) Log
	WithName(string) Log
}

func encoder() zapcore.Encoder {
	if jsonLog {
		return zapcore.NewJSONEncoder(defaultEncoder)
	}
	return zapcore.NewConsoleEncoder(defaultEncoder)
}

// AppLog is the local app singleton logger.
var AppLog Log

func init() {
	logwriter = os.Stdout

	// create a basic temp os.Stdout logger
	initLogging()
}

func initLogging() {
	AppLog = NewDefault(mainLoggerName)
}

// DebugMode sets log debug level
func DebugMode(mode bool) {
	debugMode = mode
}

// JSONLog turns JSON format on or off
func JSONLog(b bool) {
	jsonLog = b

	// Need to reinitialize
	initLogging()
}

// NewWithLevel creates a logger with a fixed level and with a set of (optional) hooks
func NewWithLevel(module string, level zap.AtomicLevel, hooks ...func(zapcore.Entry) error) Log {
	consoleSyncer := zapcore.AddSync(logwriter)
	enc := encoder()
	consoleCore := zapcore.NewCore(enc, consoleSyncer, level)
	core := zapcore.RegisterHooks(consoleCore, hooks...)
	log := zap.New(core).Named(module)
	return NewFromLog(log)
}

// NewDefault creates a Log with the default log level
func NewDefault(module string) Log {
	return NewWithLevel(module, zap.NewAtomicLevelAt(Level()))
}

// NewFromLog creates a Log from an existing zap-compatible log.
func NewFromLog(l *zap.Logger) Log {
	return Log{logger: l}
}

// InitSpacemeshLoggingSystemWithHooks sets up a logging system with one or more
// registered hooks
func InitSpacemeshLoggingSystemWithHooks(hooks ...func(zapcore.Entry) error) {
	AppLog = NewWithLevel(mainLoggerName, zap.NewAtomicLevelAt(Level()), hooks...)
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
	return FieldLogger{AppLog.logger, AppLog.name}
}

// Event returns a field logger with the Event field set to true.
func Event() FieldLogger {
	return AppLog.Event()
}

// Panic writes the log message and then panics.
func Panic(msg string, args ...interface{}) {
	AppLog.Panic(msg, args...)
}
