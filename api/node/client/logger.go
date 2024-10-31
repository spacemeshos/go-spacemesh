package client

import "go.uber.org/zap"

// A wrapper around zap.Logger to make it compatible with
// retryablehttp.LeveledLogger interface.
type retryableHttpLogger struct {
	inner *zap.Logger
}

func (r retryableHttpLogger) Error(format string, args ...any) {
	r.inner.Sugar().Errorw(format, args...)
}

func (r retryableHttpLogger) Info(format string, args ...any) {
	r.inner.Sugar().Infow(format, args...)
}

func (r retryableHttpLogger) Warn(format string, args ...any) {
	r.inner.Sugar().Warnw(format, args...)
}

func (r retryableHttpLogger) Debug(format string, args ...any) {
	r.inner.Sugar().Debugw(format, args...)
}
