package log

import (
	"bytes"
	"fmt"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"testing"
)

type FakeNodeId struct {
	key string
}

func (id FakeNodeId) Field() Field {
	return String("node_id", id.key)
}

func TestLogLevel(t *testing.T) {
	r := require.New(t)

	// Set up a hooked function to test the hook
	hooked := 0
	hookedExpected := 0
	hookFn := func(entry zapcore.Entry) error {
		hooked++
		r.Equal(zapcore.InfoLevel, entry.Level, "got wrong log level")
		return nil
	}

	// Make it easier to read the logs
	defaultEncoder.TimeKey = ""

	// Capture the log output
	var buf bytes.Buffer
	logwriter = &buf
	AppLog = NewWithLevel(mainLoggerName, zap.NewAtomicLevelAt(zapcore.DebugLevel))

	// Instantiate a logger and a sublogger
	nid := FakeNodeId{key: "abc123"}
	nidEncoded := fmt.Sprintf("{\"node_id\": \"%s\"}", nid.key)
	loggerName := "logtest"
	logger := NewWithLevel(loggerName, zap.NewAtomicLevelAt(zapcore.InfoLevel), hookFn).WithFields(nid)

	lvl := zap.NewAtomicLevel()
	r.NoError(lvl.UnmarshalText([]byte("INFO")))
	svcName := "mysvc"
	subLogger := logger.SetLevel(&lvl).WithName(svcName)
	prefix := fmt.Sprintf("%s.%-13s", loggerName, svcName)

	// Test the default app logger
	// This is NOT hooked
	teststr := "test001"
	Info(teststr)
	r.Equal(fmt.Sprintf("INFO\t%s\t%s\n", mainLoggerName, teststr), buf.String())
	buf.Reset()

	// Test the logger
	teststr = "test002"

	// This should not be printed
	logger.Debug(teststr)
	r.Equal(0, buf.Len())

	// This should be printed
	logger.Info(teststr)
	hookedExpected++
	r.Equal(fmt.Sprintf("INFO\t%s\t%s\t%s\n", loggerName, teststr, nidEncoded), buf.String())
	buf.Reset()

	// Test the sublogger
	teststr = "test003"

	// This should not be printed
	subLogger.Debug(teststr)
	r.Equal(0, buf.Len())

	// This should be printed
	subLogger.Info(teststr)
	hookedExpected++
	r.Equal(fmt.Sprintf("INFO\t%s\t%s\t%s\n", prefix, teststr, nidEncoded), buf.String())


	// Test sublog level relative to parent logger

	// Try changing the log level

	r.Equal(hookedExpected, hooked, "hook function was not called the expected number of times")
}