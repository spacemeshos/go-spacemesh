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
	expectedLevel := zapcore.InfoLevel
	hookFn := func(entry zapcore.Entry) error {
		hooked++
		r.Equal(expectedLevel, entry.Level, "got wrong log level")
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
	buf.Reset()

	// Test sublog level relative to parent logger. This should produce a warning.
	lvl = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	lvlStored := &lvl
	subLogger2 := logger.SetLevel(lvlStored)
	r.Contains(buf.String(), "attempt to SetLevel of logger lower", "expected a warning about lower child log level")
	buf.Reset()

	// This should not print anything
	subLogger2.Debug("foobar")
	r.Equal(0, buf.Len())

	// Try changing the log level the same way App.SetLogLevel does
	r.NoError(lvlStored.UnmarshalText([]byte("WARN")))
	subLogger2.Debug("foobar")
	subLogger2.Info("foobar")
	teststr = "test004"
	expectedLevel = zapcore.WarnLevel
	subLogger2.Warning(teststr)
	hookedExpected++
	r.Equal(fmt.Sprintf("WARN\t%s\t%s\t%s\n", loggerName, teststr, nidEncoded), buf.String())

	r.Equal(hookedExpected, hooked, "hook function was not called the expected number of times")
}