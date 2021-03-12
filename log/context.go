package log

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/crypto"
)

type correlationIdType int

const (
	RequestIdKey correlationIdType = iota
	SessionIdKey

	// Request and Session fields need to be separate so that each can have extra fields associated
	// with it and they don't overwrite each other.
	RequestFieldsKey
	SessionFieldsKey
)

// WithRequestId returns a context which knows its request ID.
// A request ID tracks the lifecycle of a single request across all execution contexts, including
// multiple goroutines, task queues, workers, etc. The canonical example is an incoming message
// received over the network. Rule of thumb: requests "traverse the heap" and may be passed from one
// session to another.
// This requires a requestId string, and optionally, other LoggableFields that are added to
// context and printed in contextual logs.
func WithRequestId(ctx context.Context, requestId string, fields ...LoggableField) context.Context {
	ctx = context.WithValue(ctx, RequestIdKey, requestId)
	if len(fields) > 0 {
		ctx = context.WithValue(ctx, RequestFieldsKey, fields)
	}
	return ctx
}

// WithNewRequestId does the same thing as WithRequestId but generates a new, random requestId.
// It can be used when there isn't a single, clear, unique id associated with a request (e.g.,
// a block or tx hash).
func WithNewRequestId(ctx context.Context, fields ...LoggableField) context.Context {
	return WithRequestId(ctx, crypto.UUIDString(), fields...)
}

// ExtractSessionId extracts the session id from a context object
func ExtractSessionId(ctx context.Context) (string, bool) {
	if ctxSessionId, ok := ctx.Value(SessionIdKey).(string); ok {
		return ctxSessionId, true
	}
	return "", false
}

func ExtractRequestId(ctx context.Context) (string, bool) {
	if ctxRequestId, ok := ctx.Value(RequestIdKey).(string); ok {
		return ctxRequestId, true
	}
	return "", false
}

func ExtractSessionFields(ctx context.Context) (fields []LoggableField) {
	if sessionFields, ok := ctx.Value(SessionFieldsKey).([]LoggableField); ok {
		fields = sessionFields
	}
	return
}

func ExtractRequestFields(ctx context.Context) (fields []LoggableField) {
	if requestFields, ok := ctx.Value(RequestFieldsKey).([]LoggableField); ok {
		fields = requestFields
	}
	return
}

// WithSessionId returns a context which knows its session ID
// A session ID tracks a single thread of execution. This may include multiple goroutines running
// concurrently, but it does not include asynchronous task execution such as task queues handled
// in separate threads. The canonical example is a single protocol routine, down to the point where
// an incoming request is either handled, or else handed off to another routine. Rule of thumb: sessions
// live entirely on the stack, and never on the heap.
// This requires a sessionId string, and optionally, other LoggableFields that are added to
// context and printed in contextual logs.
func WithSessionId(ctx context.Context, sessionId string, fields ...LoggableField) context.Context {
	ctx = context.WithValue(ctx, SessionIdKey, sessionId)
	if len(fields) > 0 {
		ctx = context.WithValue(ctx, SessionFieldsKey, fields)
	}
	return ctx
}

// WithNewSessionId does the same thing as WithSessionId but generates a new, random sessionId.
// It can be used when there isn't a single, clear, unique id associated with a session.
func WithNewSessionId(ctx context.Context, fields ...LoggableField) context.Context {
	return WithSessionId(ctx, crypto.UUIDString(), fields...)
}
