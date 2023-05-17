// Package log provides the both file and console (general) logging capabilities
// to spacemesh modules such as app and identity.
package log

import (
	"context"
	"io"
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// mainLoggerName is a name of the global logger.
const mainLoggerName = "00000.defaultLogger"

// should we format out logs in json.
var jsonLog = false

// where logs go by default.
var logwriter io.Writer

// default encoders.
var defaultEncoder = zap.NewDevelopmentEncoderConfig()

// Level is an alias to zapcore.Level.
type Level = zapcore.Level

// DefaultLevel returns the zapcore level of logging.
func DefaultLevel() Level {
	return zapcore.InfoLevel
}

//go:generate mockgen -package=log -destination=./log_mock.go -source=./log.go

// Logger is an interface for our logging API.
type Logger interface {
	Info(format string, args ...any)
	Debug(format string, args ...any)
	Panic(format string, args ...any)
	Error(format string, args ...any)
	Warning(format string, args ...any)
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
var (
	mu     sync.RWMutex
	AppLog Log
)

// GetLogger gets logger.
func GetLogger() Log {
	mu.RLock()
	defer mu.RUnlock()

	return AppLog
}

// SetLogger sets logger.
func SetLogger(logger Log) {
	mu.Lock()
	defer mu.Unlock()

	AppLog = logger
}

// SetupGlobal overwrites global logger.
func SetupGlobal(logger Log) {
	SetLogger(NewFromLog(logger.logger.Named(mainLoggerName)))
}

func init() {
	logwriter = os.Stdout

	// create a basic temp os.Stdout logger
	initLogging()
}

func initLogging() {
	SetLogger(NewDefault(mainLoggerName))
}

// JSONLog turns JSON format on or off.
func JSONLog(b bool) {
	jsonLog = b

	// Need to reinitialize
	initLogging()
}

// NewNop creates silent logger.
func NewNop() Log {
	return NewFromLog(zap.NewNop())
}

// NewWithLevel creates a logger with a fixed level and with a set of (optional) hooks.
func NewWithLevel(module string, level zap.AtomicLevel, hooks ...func(zapcore.Entry) error) Log {
	consoleSyncer := zapcore.AddSync(logwriter)
	enc := encoder()
	core := zapcore.NewCore(enc, consoleSyncer, level)
	log := zap.New(zapcore.RegisterHooks(core, hooks...)).Named(module)
	return NewFromLog(log)
}

// RegisterHooks wraps provided loggers with hooks.
func RegisterHooks(lg Log, hooks ...func(zapcore.Entry) error) Log {
	core := zapcore.RegisterHooks(lg.logger.Core(), hooks...)
	return NewFromLog(zap.New(core))
}

// NewDefault creates a Log with the default log level.
func NewDefault(module string) Log {
	return NewWithLevel(module, zap.NewAtomicLevelAt(DefaultLevel()))
}

// NewFromLog creates a Log from an existing zap-compatible log.
func NewFromLog(l *zap.Logger) Log {
	return Log{logger: l}
}

// public wrappers abstracting away logging lib impl

// Info prints formatted info level log message.
func Info(msg string, args ...any) {
	GetLogger().Info(msg, args...)
}

// Debug prints formatted debug level log message.
func Debug(msg string, args ...any) {
	GetLogger().Debug(msg, args...)
}

// Error prints formatted error level log message.
func Error(msg string, args ...any) {
	GetLogger().Error(msg, args...)
}

// Warning prints formatted warning level log message.
func Warning(msg string, args ...any) {
	GetLogger().Warning(msg, args...)
}

// Fatal prints formatted error level log message.
func Fatal(msg string, args ...any) {
	GetLogger().Fatal(msg, args...)
}

// With returns a FieldLogger which you can append fields to.
func With() FieldLogger {
	return FieldLogger{GetLogger().logger, GetLogger().name}
}

// Event returns a field logger with the Event field set to true.
func Event() FieldLogger {
	return GetLogger().Event()
}

// Panic writes the log message and then panics.
func Panic(msg string, args ...any) {
	GetLogger().Panic(msg, args...)
}

type (
	// ObjectMarshaller is an alias to zapcore.ObjectMarshaller.
	ObjectMarshaller = zapcore.ObjectMarshaler
	// ObjectMarshallerFunc is an alias to zapcore.ObjectMarshallerFunc.
	ObjectMarshallerFunc = zapcore.ObjectMarshalerFunc
	// ObjectEncoder is an alias to zapcore.ObjectEncoder.
	ObjectEncoder = zapcore.ObjectEncoder
	// ArrayMarshaler is an alias to zapcore.ArrayMarshaller.
	ArrayMarshaler = zapcore.ArrayMarshaler
	// ArrayMarshalerFunc is an alias to zapcore.ArrayMarshallerFunc.
	ArrayMarshalerFunc = zapcore.ArrayMarshalerFunc
	// ArrayEncoder is an alias to zapcore.ArrayEncoder.
	ArrayEncoder = zapcore.ArrayEncoder
)
