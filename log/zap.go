package log

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"runtime/debug"
	"time"
)

// Log is an exported type that embeds our logger.
type Log struct {
	logger *zap.Logger
	sugar  *zap.SugaredLogger
}

// Exported from Log basic logging options.

// Info prints formatted info level log message.
func (l Log) Info(format string, args ...interface{}) {
	l.sugar.Infof(format, args...)
}

// Debug prints formatted debug level log message.
func (l Log) Debug(format string, args ...interface{}) {
	l.sugar.Debugf(format, args...)
}

// Error prints formatted error level log message.
func (l Log) Error(format string, args ...interface{}) {
	l.sugar.Errorf(format, args...)
}

// Warning prints formatted warning level log message.
func (l Log) Warning(format string, args ...interface{}) {
	l.sugar.Warnf(format, args...)
}

func (l Log) Panic(format string, args ...interface{}) {
	l.sugar.Fatal("Fatal: goroutine panicked. Stacktrace: ", string(debug.Stack()))
	l.sugar.Panicf(format, args...)
}

// Wrap and export field logic

// Field is a log field holding a name and value
type Field zap.Field

// String returns a string Field
func String(name, val string) Field {
	return Field(zap.String(name, val))
}

// ByteString returns a byte string ([]byte) Field
func ByteString(name string, val []byte) Field {
	return Field(zap.ByteString(name, val))
}

// Int returns an int Field
func Int(name string, val int) Field {
	return Field(zap.Int(name, val))
}

func Int32(name string, val int32) Field {
	return Field(zap.Int32(name, val))
}

func Uint32(name string, val uint32) Field {
	return Field(zap.Uint32(name, val))
}

// Uint64 returns an uint64 Field
func Uint64(name string, val uint64) Field {
	return Field(zap.Uint64(name, val))
}

// Namespace make next fields be inside a namespace.
func Namespace(name string) Field {
	return Field(zap.Namespace(name))
}

// Bool returns a bool field
func Bool(name string, val bool) Field {
	return Field(zap.Bool(name, val))
}

// Duration returns a duration field
func Duration(name string, val time.Duration) Field {
	return Field(zap.Duration(name, val))
}

// Err returns an error field
func Err(v error) Field {
	return Field(zap.Error(v))
}

func unpack(fields ...Field) []zap.Field {
	flds := make([]zap.Field, len(fields))
	for f := range fields {
		flds[f] = zap.Field(fields[f])
	}
	return flds
}

type fieldLogger struct {
	l *zap.Logger
}

// With returns a logger object that logs fields
func (l Log) With() fieldLogger {
	return fieldLogger{l.logger}
}

// LogWith returns a logger the given fields
func (l Log) WithName(prefix string) Log {
	lgr := l.logger.Named(prefix)
	return Log{
		lgr,
		lgr.Sugar(),
	}
}

func (l Log) WithFields(fields ...Field) Log {
	lgr := l.logger.With(unpack(fields...)...)
	return Log{
		logger: lgr,
		sugar:  lgr.Sugar(),
	}
}

func EnableLevelOption(enabler zapcore.LevelEnabler) zap.Option {
	return zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		consoleSyncer := zapcore.AddSync(os.Stdout)
		return zapcore.NewCore(encoder(), consoleSyncer, enabler)
	})
}

var Nop = zap.WrapCore(func(zapcore.Core) zapcore.Core {
	return zapcore.NewNopCore()
})

func (l Log) WithOptions(opts ...zap.Option) Log {
	lgr := l.logger.WithOptions(opts...)
	return Log{
		logger: lgr,
		sugar:  lgr.Sugar(),
	}
}

// Info prints message with fields
func (fl fieldLogger) Info(msg string, fields ...Field) {
	fl.l.Info(msg, unpack(fields...)...)
}

// Debug prints message with fields
func (fl fieldLogger) Debug(msg string, fields ...Field) {
	fl.l.Debug(msg, unpack(fields...)...)
}

// Error prints message with fields
func (fl fieldLogger) Error(msg string, fields ...Field) {
	fl.l.Error(msg, unpack(fields...)...)
}

// Warning prints message with fields
func (fl fieldLogger) Warning(msg string, fields ...Field) {
	fl.l.Warn(msg, unpack(fields...)...)
}
