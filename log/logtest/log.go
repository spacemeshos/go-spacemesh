package logtest

import (
	"flag"
	"testing"

	"github.com/spacemeshos/go-spacemesh/log"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

var logLevel = flag.String("log", "", "log level to use in tests")

// New creates log.Log instance that will use testing.TB.Log internally.
func New(t testing.TB) log.Log {
	if len(*logLevel) == 0 {
		return log.NewNop()
	}
	level := zapcore.Level(0)
	if err := level.Set(*logLevel); err != nil {
		panic(err)
	}
	return log.NewFromLog(zaptest.NewLogger(t, zaptest.Level(level)))
}
