package log

import (
	"fmt"
	"go.uber.org/zap"
	"time"
)

// Log is an exported type that embeds our logger.
type Log struct {
	Logger *zap.Logger
}

// Exported from Log basic logging options.

// Info prints formatted info level log message.
func (l Log) Info(format string, args ...interface{}) {
	l.Logger.Info(fmt.Sprintf(format, args...))
}

// Debug prints formatted debug level log message.
func (l Log) Debug(format string, args ...interface{}) {
	l.Logger.Debug(fmt.Sprintf(format, args...))
}

// Error prints formatted error level log message.
func (l Log) Error(format string, args ...interface{}) {
	l.Logger.Error(fmt.Sprintf(format, args...))
}

// Warning prints formatted warning level log message.
func (l Log) Warning(format string, args ...interface{}) {
	l.Logger.Warn(fmt.Sprintf(format, args...))
}

// Wrap and export field logic


// Field is a log field holding a name and value
type Field zap.Field

// String returns a string Field
func String(name, val string) Field {
	return Field(zap.String(name,val))
}

// Int returns an int Field
func Int(name string, val int) Field {
	return Field(zap.Int(name,val))
}

// Namespace make next fields be inside a namespace.
func Namespace(name string ) Field {
	return Field(zap.Namespace(name))
}

// Bool returns a bool field
func Bool(name string, val bool) Field {
	return Field(zap.Bool(name,val))
}

// Duration returns a duration field
func Duration(name string, val time.Duration) Field {
	return Field(zap.Duration(name,val))
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

// Infow prints message with fields
func (l Log) Infow(msg string, fields ...Field) {
	l.Logger.Info(msg, unpack(fields...)...)
}

// Debugw prints message with fields
func (l Log) Debugw(msg string, fields ...Field) {
	l.Logger.Debug(msg, unpack(fields...)...)
}

// Errorw prints message with fields
func (l Log) Errorw(msg string, fields ...Field) {
	l.Logger.Error(msg, unpack(fields...)...)
}

// Warningw prints message with fields
func (l Log) Warningw(msg string, fields ...Field) {
	l.Logger.Warn(msg, unpack(fields...)...)
}