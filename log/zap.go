package log

import (
	"fmt"
	"runtime/debug"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Log is an exported type that embeds our logger.
type Log struct {
	logger *zap.Logger
}

// Exported from Log basic logging options.

// Info prints formatted info level log message.
func (l Log) Info(format string, args ...interface{}) {
	l.logger.Sugar().Infof(format, args...)
}

// Debug prints formatted debug level log message.
func (l Log) Debug(format string, args ...interface{}) {
	l.logger.Sugar().Debugf(format, args...)
}

// Error prints formatted error level log message.
func (l Log) Error(format string, args ...interface{}) {
	l.logger.Sugar().Errorf(format, args...)
}

// Warning prints formatted warning level log message.
func (l Log) Warning(format string, args ...interface{}) {
	l.logger.Sugar().Warnf(format, args...)
}

// Panic prints the log message and then panics.
func (l Log) Panic(format string, args ...interface{}) {
	l.logger.Sugar().Error("Fatal: goroutine panicked. Stacktrace: ", string(debug.Stack()))
	l.logger.Sugar().Panicf(format, args...)
}

// Wrap and export field logic

// Field is a log field holding a name and value
type Field zap.Field

// Field satisfy loggable field interface.
func (f Field) Field() Field { return f }

// FieldNamed returns a field with the provided name instead of the default.
func FieldNamed(name string, field LoggableField) Field {
	f := field.Field()
	f.Key = name
	return f
}

// String returns a string Field
func String(name, val string) Field {
	return Field(zap.String(name, val))
}

// Int returns an int Field.
func Int(name string, val int) Field {
	return Field(zap.Int(name, val))
}

// Int32 returns an int32 Field.
func Int32(name string, val int32) Field {
	return Field(zap.Int32(name, val))
}

// Uint32 returns an uint32 Field.
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
	return Field(zap.NamedError("errmsg", v))
}

// LoggableField as an interface to enable every type to be used as a log field.
type LoggableField interface {
	Field() Field
}

func unpack(fields []LoggableField) []zap.Field {
	flds := make([]zap.Field, len(fields))
	for i, f := range fields {
		flds[i] = zap.Field(f.Field())
	}
	return flds
}

// FieldLogger is a logger that only logs messages with fields. It does not support formatting.
type FieldLogger struct {
	l *zap.Logger
}

// With returns a logger object that logs fields
func (l Log) With() FieldLogger {
	return FieldLogger{l.logger}
}

// SetLevel returns a logger with level as the log level derived from l.
func (l Log) SetLevel(level *zap.AtomicLevel) Log {
	// Warn if the new level is lower than the parent level
	if willWrite := l.logger.Check(level.Level(), "test"); willWrite == nil {
		Warning("attempt to SetLevel of logger lower than parent level, this may result in " +
			"log entries being dropped silently")
	}
	lgr := l.logger.WithOptions(zap.IncreaseLevel(zap.LevelEnablerFunc(level.Enabled)))
	return Log{
		lgr,
	}
}

// WithName returns a logger the given fields
func (l Log) WithName(prefix string) Log {
	lgr := l.logger.Named(fmt.Sprintf("%-13s", prefix))
	return Log{lgr}
}

// WithFields returns a logger with fields permanently appended to it.
func (l Log) WithFields(fields ...LoggableField) Log {
	lgr := l.logger.With(unpack(fields)...)
	return Log{logger: lgr}
}

const eventKey = "event"

// Event returns a logger with the Event field appended to it.
func (l Log) Event() FieldLogger {
	return FieldLogger{l: l.logger.With(zap.Field(Bool(eventKey, true)))}
}

// Nop is an option that disables this logger.
var Nop = zap.WrapCore(func(zapcore.Core) zapcore.Core {
	return zapcore.NewNopCore()
})

// WithOptions clones the current Logger, applies the supplied Options, and
// returns the resulting Logger. It's safe to use concurrently.
func (l Log) WithOptions(opts ...zap.Option) Log {
	lgr := l.logger.WithOptions(opts...)
	return Log{logger: lgr}
}

// Info prints message with fields
func (fl FieldLogger) Info(msg string, fields ...LoggableField) {
	fl.l.Info(msg, unpack(fields)...)
}

// Debug prints message with fields
func (fl FieldLogger) Debug(msg string, fields ...LoggableField) {
	fl.l.Debug(msg, unpack(fields)...)
}

// Error prints message with fields
func (fl FieldLogger) Error(msg string, fields ...LoggableField) {
	fl.l.Error(msg, unpack(fields)...)
}

// Warning prints message with fields
func (fl FieldLogger) Warning(msg string, fields ...LoggableField) {
	fl.l.Warn(msg, unpack(fields)...)
}
