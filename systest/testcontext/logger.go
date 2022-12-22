package testcontext

import (
	"bytes"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func newLogger(t testing.TB, logLevel zapcore.LevelEnabler, opts ...zap.Option) *zap.Logger {
	writer := testingWriter{t: t}

	zapOptions := []zap.Option{
		// Send zap errors to the same writer and mark the test as failed if
		// that happens.
		zap.ErrorOutput(testingWriter{t: t, markFailed: true}),
	}

	zapOptions = append(zapOptions, opts...)

	return zap.New(
		zapcore.NewCore(
			zapcore.NewJSONEncoder(zap.NewDevelopmentEncoderConfig()),
			writer,
			logLevel,
		),
		zapOptions...,
	)
}

// testingWriter is a WriteSyncer that writes to the given testing.TB.
type testingWriter struct {
	t testing.TB

	// If true, the test will be marked as failed if this testingWriter is
	// ever used.
	markFailed bool
}

func (w testingWriter) Write(p []byte) (n int, err error) {
	n = len(p)

	// Strip trailing newline because t.Log always adds one.
	p = bytes.TrimRight(p, "\n")

	// Note: t.Log is safe for concurrent use.
	w.t.Logf("%s", p)
	if w.markFailed {
		w.t.Fail()
	}

	return n, nil
}

func (w testingWriter) Sync() error {
	return nil
}
