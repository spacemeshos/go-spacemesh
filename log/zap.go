package log

import (
	"context"
	"fmt"
	"reflect"
	"runtime/debug"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Log is an exported type that embeds our logger.
type Log struct {
	logger *zap.Logger
	name   string
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

// Field satisfies loggable field interface
func (f Field) Field() Field { return f }

// FieldNamed returns a field with the provided name instead of the default
func FieldNamed(name string, field LoggableField) Field {
	if field == nil || (reflect.ValueOf(field).Kind() == reflect.Ptr && reflect.ValueOf(field).IsNil()) {
		return String(name, "nil")
	}
	f := field.Field()
	f.Key = name
	return f
}

// String returns a string Field
func String(name, val string) Field {
	return Field(zap.String(name, val))
}

// Binary will encode binary content in base64 when logged.
func Binary(name string, val []byte) Field {
	return Field(zap.Binary(name, val))
}

// Int returns an int Field
func Int(name string, val int) Field {
	return Field(zap.Int(name, val))
}

// Int32 returns an int32 Field
func Int32(name string, val int32) Field {
	return Field(zap.Int32(name, val))
}

// Uint16 returns an uint32 Field
func Uint16(name string, val uint16) Field {
	return Field(zap.Uint16(name, val))
}

// Uint32 returns an uint32 Field
func Uint32(name string, val uint32) Field {
	return Field(zap.Uint32(name, val))
}

// Uint64 returns an uint64 Field
func Uint64(name string, val uint64) Field {
	return Field(zap.Uint64(name, val))
}

// Namespace make next fields be inside a namespace
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
	l    *zap.Logger
	name string
}

// With returns a logger object that logs fields
func (l Log) With() FieldLogger {
	return FieldLogger{l.logger, l.name}
}

// SetLevel returns a logger with level as the log level derived from l.
func (l Log) SetLevel(level *zap.AtomicLevel) Log {
	// Warn if the new level is lower than the parent level
	if willWrite := l.logger.Check(level.Level(), "test"); willWrite == nil {
		Warning("attempt to SetLevel of logger lower than parent level, this may result in " +
			"log entries being dropped silently")
	}
	lgr := l.logger.WithOptions(zap.IncreaseLevel(level))
	return Log{logger: lgr, name: l.name}
}

// WithName returns a logger with the given name
func (l Log) WithName(prefix string) Log {
	lgr := l.logger.Named(fmt.Sprintf("%-13s", prefix))
	var name string
	if l.name == "" {
		name = prefix
	} else {
		name = strings.Join([]string{l.name, prefix}, ".")
	}
	return Log{logger: lgr, name: name}
}

// WithFields returns a logger with fields permanently appended to it
func (l Log) WithFields(fields ...LoggableField) Log {
	lgr := l.logger.With(unpack(fields)...)
	return Log{logger: lgr, name: l.name}
}

// WithContext creates a Log from an existing log and a context object
func (l Log) WithContext(ctx context.Context) Log {
	var fields []LoggableField
	if ctx != nil {
		if ctxRequestID, ok := ExtractRequestID(ctx); ok {
			fields = append(fields, append(ExtractRequestFields(ctx), String("requestId", ctxRequestID))...)
		}
		if ctxSessionID, ok := ExtractSessionID(ctx); ok {
			fields = append(fields, append(ExtractSessionFields(ctx), String("sessionId", ctxSessionID))...)
		}
	}
	return l.WithFields(fields...)
}

const eventKey = "event"

// Event returns a logger with the Event field appended to it
func (l Log) Event() FieldLogger {
	return FieldLogger{l: l.logger.With(zap.Field(Bool(eventKey, true))), name: l.name}
}

// Nop is an option that disables this logger
var Nop = zap.WrapCore(func(zapcore.Core) zapcore.Core {
	return zapcore.NewNopCore()
})

// WithOptions clones the current Logger, applies the supplied Options, and
// returns the resulting Logger. It's safe to use concurrently.
func (l Log) WithOptions(opts ...zap.Option) Log {
	lgr := l.logger.WithOptions(opts...)
	return Log{logger: lgr, name: l.name}
}

// note: we construct the fieldset on the fly, below, rather than simply adding `name' as a field since it may change
// if a child logger is created from a parent. once a field has been added to a logger it cannot be changed or removed.
// see WithName, above.

// Info prints message with fields
func (fl FieldLogger) Info(msg string, fields ...LoggableField) {
	fl.l.Info(msg, unpack(append(fields, String("name", fl.name)))...)
}

// Debug prints message with fields
func (fl FieldLogger) Debug(msg string, fields ...LoggableField) {
	fl.l.Debug(msg, unpack(append(fields, String("name", fl.name)))...)
}

// Error prints message with fields
func (fl FieldLogger) Error(msg string, fields ...LoggableField) {
	fl.l.Error(msg, unpack(append(fields, String("name", fl.name)))...)
}

// Warning prints message with fields
func (fl FieldLogger) Warning(msg string, fields ...LoggableField) {
	fl.l.Warn(msg, unpack(append(fields, String("name", fl.name)))...)
}

// Panic prints message with fields
func (fl FieldLogger) Panic(msg string, fields ...LoggableField) {
	fl.l.Panic(msg, unpack(append(fields, String("name", fl.name)))...)
}
