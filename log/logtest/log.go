package logtest

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

var logLevel = zap.LevelFlag("log", zap.DebugLevel, "log level to use in tests")

// New creates log.Log instance that will use testing.TB.Log internally.
func New(t testing.TB) log.Log {
	return log.NewFromLog(zaptest.NewLogger(t, zaptest.Level(logLevel)))
}
