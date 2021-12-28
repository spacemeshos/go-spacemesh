package log

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestFatalLogWithArgs(t *testing.T) {
	var buf bytes.Buffer
	logwriter = &buf

	loggerName := "logtest"
	JSONLog(true)
	logger := NewWithLevel(loggerName, zap.NewAtomicLevelAt(zapcore.InfoLevel)).WithOptions(zap.OnFatal(zapcore.WriteThenPanic))

	testStr := "test001"
	testDur := 1 * time.Second

	type Entry struct {
		L     string
		M     string
		N     string
		Code  string
		Error string
		Args  []interface{}
	}

	expect := Entry{
		L:     "FATAL",
		M:     testStr,
		N:     loggerName,
		Code:  "ERR_CLOCK_AWAY",
		Error: "clock drift by 1s",
		Args:  []interface{}{float64(testDur)},
	}

	got := Entry{}

	defer func() {
		if r := recover(); r != nil {
			require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
			require.EqualValues(t, expect, got)
		}
	}()

	logger.With().Fatal(testStr, Err(ErrClockAway(testDur)))

	buf.Reset()
}
