package logtest

import (
	"os"
	"testing"

	"github.com/spacemeshos/go-spacemesh/log"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

const testLogLevel = "TEST_LOG_LEVEL"

// New creates log.Log instance that will use testing.TB.Log internally.
func New(tb testing.TB, overvwrite ...zapcore.Level) log.Log {
	var level zapcore.Level
	if len(overvwrite) > 0 {
		level = overvwrite[0]
	} else {
		lvl := os.Getenv(testLogLevel)
		if len(lvl) == 0 {
			return log.NewNop()
		}
		if err := level.Set(lvl); err != nil {
			panic(err)
		}
	}
	return log.NewFromLog(zaptest.NewLogger(tb, zaptest.Level(level)))
}

// SetupGlobal updates AppLog to the instance of test-specific logger.
// Some tests do not terminate all goroutines when they exit, so we can't recover original for now.
func SetupGlobal(tb testing.TB, overvwrite ...zapcore.Level) {
	log.AppLog = New(tb, overvwrite...)
}
