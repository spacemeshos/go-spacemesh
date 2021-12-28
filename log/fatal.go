package log

import (
	"fmt"
)

// Common errors that can happen on node startup.
var (
	ErrMalformedConfig            = newFatalError("ERR_MALFORMED_CONFIG", "config file is malformed: %v")
	ErrBadFlags                   = newFatalError("ERR_BAD_FLAGS", "bad CLI flags: %v")
	ErrIncompatibleTortoiseParams = newFatalError("ERR_INCOMPATIBLE_TORTOISE_PARAMS", "incompatible tortoise hare params: %v")
	ErrEnsureDataDir              = newFatalError("ERR_ENSURE_DATA_DIR", "could not open/create data dir %v: %v")
	ErrClockAway                  = newFatalError("ERR_CLOCK_AWAY", "clock drift by %v")
	ErrReadHostName               = newFatalError("ERR_READ_HOSTNAME", "error reading hostname: %v")
	ErrStartProfiling             = newFatalError("ERR_START_PROFILING", "could not start profiling: %v")
	ErrRetrieveIdentity           = newFatalError("ERR_RETRIEVE_IDENTITY", "could not retrieve identity: %v")
	ErrVRFSigner                  = newFatalError("ERR_VRF_SIGNER", "could not create VRF signer: %v")
)

// fatalError describes a fatal error SMApp needs to know information about.
type fatalError struct {
	Code string
	Text string
	Args []interface{}
}

func newFatalError(code, text string) func(args ...interface{}) *fatalError {
	return func(args ...interface{}) *fatalError {
		return &fatalError{
			Code: code,
			Text: text,
			Args: args,
		}
	}
}

func (fe fatalError) Error() string {
	return fmt.Sprintf(fe.Text, fe.Args...)
}

// Field implements LoggableField.
func (fe fatalError) Field() Field { return Inline(fe) }

// MarshalLogObject implements logging encoder for fatalError.
func (fe fatalError) MarshalLogObject(encoder ObjectEncoder) error {
	encoder.AddString("code", fe.Code)
	encoder.AddString("error", fe.Error())
	if err := encoder.AddArray("args", arrayMarshaler(fe.Args)); err != nil {
		return fmt.Errorf("add array: %w", err)
	}

	return nil
}

type arrayMarshaler []interface{}

func (args arrayMarshaler) MarshalLogArray(encoder ArrayEncoder) error {
	for _, arg := range args {
		if err := encoder.AppendReflected(arg); err != nil {
			return fmt.Errorf("append reflected: %w", err)
		}
	}
	return nil
}
