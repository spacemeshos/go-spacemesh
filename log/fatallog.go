package log

import (
	"fmt"
)

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

type FatalError struct {
	Code   string
	Text   string
	Args   []interface{}
	Reason error
}

func newFatalErrorWithArgs(code, text string) func(args ...interface{}) *FatalError {
	return func(args ...interface{}) *FatalError {
		return &FatalError{
			Code: code,
			Text: text,
			Args: args,
		}
	}
}

func newFatalErrorWithReason(code, text string) func(reason error) *FatalError {
	return func(reason error) *FatalError {
		return &FatalError{
			Code:   code,
			Text:   text,
			Reason: reason,
		}
	}
}

func (fe FatalError) Error() string {
	if fe.Reason != nil {
		return fmt.Sprintf("%v: %v", fe.Text, fe.Reason)
	}

	if len(fe.Args) != 0 {
		return fmt.Sprintf(fe.Text, fe.Args...)
	}

	return fe.Text
}

func (fe FatalError) Field() Field { return Inline(fe) }

// MarshalLogObject implements logging encoder for layerRewardsInfo.
func (fe FatalError) MarshalLogObject(encoder ObjectEncoder) error {
	encoder.AddString("code", fe.Code)
	encoder.AddString("error", fe.Error())
	return encoder.AddArray("args", arrayMarshaler(fe.Args))
}

type arrayMarshaler []interface{}

func (args arrayMarshaler) MarshalLogArray(encoder ArrayEncoder) error {
	for _, arg := range args {
		if err := encoder.AppendReflected(arg); err != nil {
			return err
		}
	}
	return nil
}
