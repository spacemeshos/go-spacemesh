package log

import (
	"fmt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"runtime/debug"
	"time"
)

var NilLogger Log

// Log is an exported type that embeds our logger.
type Log struct {
	logger *zap.Logger
	sugar  *zap.SugaredLogger
	lvl    *zap.AtomicLevel
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
	l.sugar.Error("Fatal: goroutine panicked. Stacktrace: ", string(debug.Stack()))
	l.sugar.Panicf(format, args...)
}

// Wrap and export field logic

// Field is a log field holding a name and value
type Field zap.Field

func (f Field) Field() Field { return f }

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

// LayerId return a Uint64 field (key - "layer_id")
func LayerId(val uint64) Field {
	return Uint64("layer_id", val)
}

// TxId return a String field (key - "tx_id")
func TxId(val string) Field {
	return String("tx_id", val)
}

// AtxId return a String field (key - "atx_id")
func AtxId(val string) Field {
	return String("atx_id", val)
}

// BlockId return a Uint64 field (key - "block_id")
func BlockId(val string) Field {
	return String("block_id", val)
}

// EpochId return a Uint64 field (key - "epoch_id")
func EpochId(val uint64) Field {
	return Uint64("epoch_id", val)
}

// NodeId return a String field (key - "node_id")
func NodeId(val string) Field {
	return String("node_id", val)
}

// Err returns an error field
func Err(v error) Field {
	return Field(zap.Error(v))
}

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

type fieldLogger struct {
	l *zap.Logger
}

// With returns a logger object that logs fields
func (l Log) With() fieldLogger {
	return fieldLogger{l.logger}
}

func (l Log) SetLevel(level *zap.AtomicLevel) Log {
	lgr := l.logger.WithOptions(AddDynamicLevel(level))
	return Log{
		lgr,
		lgr.Sugar(),
		level,
	}
}

// LogWith returns a logger the given fields
func (l Log) WithName(prefix string) Log {
	lgr := l.logger.Named(fmt.Sprintf("%-13s", prefix)).WithOptions(AddDynamicLevel(l.lvl))
	return Log{
		lgr,
		lgr.Sugar(),
		l.lvl,
	}
}

func AddDynamicLevel(level *zap.AtomicLevel) zap.Option {
	return zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return &coreWithLevel{
			Core: core,
			lvl:  level,
		}
	})
}

type coreWithLevel struct {
	zapcore.Core
	lvl *zap.AtomicLevel
}

func (c *coreWithLevel) Enabled(level zapcore.Level) bool {
	return c.lvl.Enabled(level)
}

func (c *coreWithLevel) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if !c.lvl.Enabled(e.Level) {
		return ce
	}
	return ce.AddCore(e, c.Core)
}

func (l Log) WithFields(fields ...LoggableField) Log {
	lgr := l.logger.With(unpack(fields)...).WithOptions(AddDynamicLevel(l.lvl))
	return Log{
		logger: lgr,
		sugar:  lgr.Sugar(),
		lvl:    l.lvl,
	}
}

const event_key = "event"

func (l Log) Event() fieldLogger {
	return fieldLogger{l: l.logger.With(zap.Field(Bool(event_key, true)))}
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
		lvl:    l.lvl,
	}
}

// Info prints message with fields
func (fl fieldLogger) Info(msg string, fields ...LoggableField) {
	fl.l.Info(msg, unpack(fields)...)
}

// Debug prints message with fields
func (fl fieldLogger) Debug(msg string, fields ...LoggableField) {
	fl.l.Debug(msg, unpack(fields)...)
}

// Error prints message with fields
func (fl fieldLogger) Error(msg string, fields ...LoggableField) {
	fl.l.Error(msg, unpack(fields)...)
}

// Warning prints message with fields
func (fl fieldLogger) Warning(msg string, fields ...LoggableField) {
	fl.l.Warn(msg, unpack(fields)...)
}
