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

// where logs go by default.
var logWriter io.Writer = os.Stdout

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

// SetupGlobal overwrites global logger.
func SetupGlobal(logger Log) {
	mu.Lock()
	defer mu.Unlock()
	AppLog = NewFromLog(logger.logger.Named(mainLoggerName))
}

func init() {
	SetupGlobal(NewWithLevel(mainLoggerName,
		zap.NewAtomicLevelAt(zapcore.InfoLevel),
		zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
	))
}

// NewNop creates silent logger.
func NewNop() Log {
	return NewFromLog(zap.NewNop())
}

// NewWithLevel creates a logger with a fixed level and with a set of (optional) hooks.
func NewWithLevel(module string,
	level zap.AtomicLevel,
	encoder zapcore.Encoder,
	hooks ...func(zapcore.Entry) error,
) Log {
	consoleSyncer := zapcore.AddSync(logWriter)
	core := zapcore.NewCore(encoder, consoleSyncer, level)
	log := zap.New(zapcore.RegisterHooks(core, hooks...)).Named(module)
	return NewFromLog(log)
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

// Warning prints formatted warning level log message.
func Warning(msg string, args ...any) {
	GetLogger().Warning(msg, args...)
}

// With returns a FieldLogger which you can append fields to.
func With() FieldLogger {
	return FieldLogger{GetLogger().logger, GetLogger().name}
}

// Panic writes the log message and then panics.
func Panic(msg string, args ...any) {
	GetLogger().Panic(msg, args...)
}
