package log

import (
	"fmt"
)

// Common errors that can happen on node startup.
var (
	ErrMalformedConfig            = newFatalErrorWithReason("ERR_MALFORMED_CONFIG", "config file is malformed")
	ErrBadFlags                   = newFatalErrorWithArgs("ERR_BAD_FLAGS", "bad CLI flags")
	ErrIncompatibleTortoiseParams = newFatalErrorWithArgs("ERR_INCOMPATIBLE_TORTOISE_PARAMS", "incompatible tortoise params: %v")
	ErrEnsureDataDir              = newFatalErrorWithArgs("ERR_ENSURE_DATA_DIR", "could not open/create data dir %v: %v")
	ErrClockAway                  = newFatalErrorWithArgs("ERR_CLOCK_AWAY", "clock drift by %v")
	ErrReadHostName               = newFatalErrorWithReason("ERR_READ_HOSTNAME", "error reading hostname")
	ErrStartProfiling             = newFatalErrorWithReason("ERR_START_PROFILING", "could not start profiling")
	ErrRetrieveIdentity           = newFatalErrorWithReason("ERR_RETRIEVE_IDENTITY", "could not retrieve identity")
	ErrVRFSigner                  = newFatalErrorWithReason("ERR_VRF_SIGNER", "could not create VRF signer")
)

// fatalError describes a fatal error SMApp needs to know information about.
type fatalError struct {
	Code   string
	Text   string
	Args   []interface{}
	Reason error
}

func newFatalErrorWithArgs(code, text string) func(args ...interface{}) *fatalError {
	return func(args ...interface{}) *fatalError {
		return &fatalError{
			Code: code,
			Text: text,
			Args: args,
		}
	}
}

func newFatalErrorWithReason(code, text string) func(reason error) *fatalError {
	return func(reason error) *fatalError {
		return &fatalError{
			Code:   code,
			Text:   text,
			Reason: reason,
		}
	}
}

func (fe fatalError) Error() string {
	if fe.Reason != nil {
		return fmt.Sprintf("%v: %v", fe.Text, fe.Reason)
	}

	if len(fe.Args) != 0 {
		return fmt.Sprintf(fe.Text, fe.Args...)
	}

	return fe.Text
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
