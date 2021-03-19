package log

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"testing"
)

type FakeNodeID struct {
	key string
}

func (id FakeNodeID) Field() Field {
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
	nid := FakeNodeID{key: "abc123"}
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

	r.Equal(hookedExpected, hooked, "hook function was not called the expected number of times")
}
func TestJsonLog(t *testing.T) {
	r := require.New(t)

	// Make it easier to read the logs
	defaultEncoder.TimeKey = ""

	// Capture the log output
	var buf bytes.Buffer
	logwriter = &buf
	AppLog = NewDefault(mainLoggerName)

	// Expect output not to be in JSON format
	teststr := "test001"
	Info(teststr)
	r.Equal(fmt.Sprintf("INFO\t%s\t%s\n", mainLoggerName, teststr), buf.String())
	buf.Reset()

	// Enable JSON mode
	JSONLog(true)

	// Expect output to be in JSON format
	teststr = "test002"
	type entry struct {
		L, M, N string
	}
	expect := entry{
		L: "INFO",
		M: teststr,
		N: mainLoggerName,
	}
	Info(teststr)
	got := entry{}
	r.NoError(json.Unmarshal(buf.Bytes(), &got))
	r.Equal(expect, got)
}

func TestContextualLogging(t *testing.T) {
	// basic housekeeping
	r := require.New(t)
	reqID := "myRequestId"
	sesID := "mySessionId"
	teststr := "test003"
	JSONLog(true)

	// test basic context first: try to set and read context, roundtrip
	ctx := context.Background()
	ctx = WithRequestID(ctx, reqID)
	if reqID2, ok := ExtractRequestID(ctx); ok {
		r.Equal(reqID, reqID2)
	} else {
		r.Fail("failed to extract request ID after setting")
	}
	ctx = WithSessionID(ctx, sesID)
	if sesID2, ok := ExtractSessionID(ctx); ok {
		r.Equal(sesID, sesID2)
	} else {
		r.Fail("failed to extract session ID after setting")
	}

	// try again in reverse order
	ctx = context.Background()
	ctx = WithRequestID(WithSessionID(ctx, sesID), reqID)
	if reqID2, ok := ExtractRequestID(ctx); ok {
		r.Equal(reqID, reqID2)
	} else {
		r.Fail("failed to extract request ID after setting")
	}
	ctx = WithSessionID(ctx, sesID)
	if sesID2, ok := ExtractSessionID(ctx); ok {
		r.Equal(sesID, sesID2)
	} else {
		r.Fail("failed to extract session ID after setting")
	}

	// try re-setting (in reverse)
	ctx = WithRequestID(WithSessionID(ctx, reqID), sesID)
	if reqID2, ok := ExtractRequestID(ctx); ok {
		r.Equal(sesID, reqID2)
	} else {
		r.Fail("failed to extract request ID after setting")
	}
	if sesID2, ok := ExtractSessionID(ctx); ok {
		r.Equal(reqID, sesID2)
	} else {
		r.Fail("failed to extract session ID after setting")
	}

	// try setting new
	ctx = WithNewRequestID(WithNewSessionID(context.Background()))
	if reqID2, ok := ExtractRequestID(ctx); ok {
		_, err := uuid.Parse(reqID2)
		r.NoError(err)
	} else {
		r.Fail("failed to extract request ID after setting")
	}
	if sesID2, ok := ExtractSessionID(ctx); ok {
		_, err := uuid.Parse(sesID2)
		r.NoError(err)
	} else {
		r.Fail("failed to extract session ID after setting")
	}

	// Capture the log output
	var buf bytes.Buffer
	logwriter = &buf
	AppLog = NewDefault(mainLoggerName)

	// make sure we can set and read context
	ctx = WithRequestID(context.Background(), reqID)
	contextualLogger := AppLog.WithContext(ctx)
	contextualLogger.Info(teststr)
	type entry struct {
		L, M, N, RequestID, SessionID, Foo string
	}
	expect := entry{
		L:         "INFO",
		M:         teststr,
		N:         mainLoggerName,
		RequestID: reqID,
	}
	got := entry{}
	r.NoError(json.Unmarshal(buf.Bytes(), &got))
	r.Equal(expect, got)

	// test extra fields
	buf.Reset()
	ctx = WithSessionID(context.Background(), sesID, String("foo", "bar"))
	contextualLogger = AppLog.WithContext(ctx)
	contextualLogger.Info(teststr)
	expect = entry{
		L:         "INFO",
		M:         teststr,
		N:         mainLoggerName,
		SessionID: sesID,
		Foo:       "bar",
	}
	got = entry{}
	r.NoError(json.Unmarshal(buf.Bytes(), &got))
	r.Equal(expect, got)
}
