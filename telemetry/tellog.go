// Based on https://github.com/tchap/zapext/blob/master/zapsentry/core.go
package telemetry

import (
	"github.com/influxdata/influxdb-client-go"
	"go.uber.org/zap/zapcore"
)

////
//// Significant field keys
////
//
//const TagPrefix = "#"
//
//const (
//	EventIDKey     = "event_id"
//	ProjectKey     = "project"
//	TimestampKey   = "timestamp"
//	LoggerKey      = "logger"
//	PlatformKey    = "platform"
//	CulpritKey     = "culprit"
//	ServerNameKey  = "server_name"
//	ErrorKey       = "error"
//	HTTPRequestKey = "http_request"
//	UserKey        = "user"
//)
//
//const ErrorStackTraceKey = "error_stack_trace"
//
//const SkipKey = "_zapsentry_skip"
//
//// Skip returns a field that tells zapsentry to skip the log entry.
//func Skip() zapcore.Field {
//	return zap.Bool(SkipKey, true)
//}

//
// Core options
//

type Option func(*Core)

//
// Core
//

type Core struct {
	zapcore.LevelEnabler

	writeApi influxdb2.WriteApi
	nodeId string
	module string

	fields []zapcore.Field
}

func NewCore(enab zapcore.LevelEnabler, wa influxdb2.WriteApi, nodeId string, module string, options ...Option) *Core {
	core := &Core{
		LevelEnabler:   enab,
		writeApi:		wa,
		nodeId:			nodeId,
		module:			module,
	}

	for _, opt := range options {
		opt(core)
	}

	return core
}

func (core *Core) With(fields []zapcore.Field) zapcore.Core {
	// Clone core.
	clone := *core

	// Clone and append fields.
	clone.fields = make([]zapcore.Field, len(core.fields)+len(fields))
	copy(clone.fields, core.fields)
	copy(clone.fields[len(core.fields):], fields)

	// Done.
	return &clone
}

func (core *Core) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if core.Enabled(entry.Level) {
		return checked.AddCore(entry, core)
	}
	return checked
}

func (core *Core) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	// Create a new point
	p := influxdb2.NewPointWithMeasurement("log").
		SetTime(entry.Time).
		AddTag("level", string(entry.Level)).
		AddTag("nodeId", core.nodeId).
		AddTag("module", core.module).
		AddTag("logger", entry.LoggerName).
		AddField("msg", entry.Message)

	//// Process fields.
	//encoder := zapcore.NewMapObjectEncoder()
	//
	//// When set, relevant Sentry interfaces are added.
	//var (
	//	err error
	//	req *http.Request
	//)
	//
	//// processField processes the given field.
	//// When false is returned, the whole entry is to be skipped.
	//processField := func(field zapcore.Field) bool {
	//	// Check for significant keys.
	//	switch field.Key {
	//	case EventIDKey:
	//		event.EventID = sentry.EventID(field.String)
	//
	//	case PlatformKey:
	//		event.Platform = field.String
	//
	//	case ServerNameKey:
	//		event.ServerName = field.String
	//
	//	case ErrorKey:
	//		if ex, ok := field.Interface.(error); ok {
	//			err = ex
	//		} else {
	//			field.AddTo(encoder)
	//		}
	//
	//	case HTTPRequestKey:
	//		switch r := field.Interface.(type) {
	//		case *http.Request:
	//			req = r
	//		case types.HTTPRequest:
	//			req = r.R
	//		case *types.HTTPRequest:
	//			req = r.R
	//		default:
	//			field.AddTo(encoder)
	//		}
	//
	//	case SkipKey:
	//		return false
	//
	//	case UserKey:
	//		switch user := field.Interface.(type) {
	//		case User:
	//			event.User = sentry.User(user)
	//		case *User:
	//			event.User = sentry.User(*user)
	//		default:
	//			field.AddTo(encoder)
	//		}
	//
	//	default:
	//		// Add to the encoder in case this is not a significant key.
	//		field.AddTo(encoder)
	//	}
	//
	//	return true
	//}
	//
	//// Process core fields first.
	//for _, field := range core.fields {
	//	if !processField(field) {
	//		return nil
	//	}
	//}
	//
	//// Then process the fields passed directly.
	//// These can be then used to overwrite the core fields.
	//for _, field := range fields {
	//	if !processField(field) {
	//		return nil
	//	}
	//}
	//
	//// Split fields into tags and extra.
	//tags := make(map[string]string)
	//extra := make(map[string]interface{})
	//
	//for key, value := range encoder.Fields {
	//	if strings.HasPrefix(key, TagPrefix) {
	//		key = key[len(TagPrefix):]
	//		if v, ok := value.(string); ok {
	//			tags[key] = v
	//		} else {
	//			tags[key] = fmt.Sprintf("%v", value)
	//		}
	//	} else {
	//		extra[key] = value
	//	}
	//}
	//
	//if err != nil {
	//	// In case an error object is present, create an exception.
	//	// Capture the stack trace in any case.
	//	stacktrace := sentry.ExtractStacktrace(err)
	//	if stacktrace == nil {
	//		stacktrace = sentry.NewStacktrace()
	//	}
	//	// Handle wrapped errors for github.com/pingcap/errors and github.com/pkg/errors
	//	cause := errors.Cause(err)
	//	event.Exception = []sentry.Exception{{
	//		Value:      cause.Error(),
	//		Type:       reflect.TypeOf(cause).String(),
	//		Stacktrace: stacktrace,
	//	}}
	//} else {
	//	stacktrace := sentry.NewStacktrace()
	//	stacktrace.Frames = filterFrames(stacktrace.Frames)
	//	event.Exception = []sentry.Exception{{
	//		Value:      entry.Message,
	//		Stacktrace: stacktrace,
	//	}}
	//}
	//
	//// Add tags and extra into the packet.
	//if len(tags) != 0 {
	//	event.Tags = tags
	//}
	//if len(extra) != 0 {
	//	event.Extra = extra
	//}

	// Send the point
	core.writeApi.WritePoint(p)
	return nil
}

func (core *Core) Sync() error {
	core.writeApi.Flush()
	return nil
}

