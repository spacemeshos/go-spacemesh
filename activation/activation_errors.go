package activation

import (
	"errors"
	"fmt"
)

var (
	// ErrATXChallengeExpired is returned when atx missed its publication window and needs to be regenerated.
	ErrATXChallengeExpired = errors.New("builder: atx expired")
	// ErrPoetServiceUnstable is returned when poet quality of service is low.
	ErrPoetServiceUnstable = &PoetSvcUnstableError{}
	// ErrPoetProofNotReceived is returned when no poet proof was received.
	ErrPoetProofNotReceived = errors.New("builder: didn't receive any poet proof")
)

// PoetSvcUnstableError means there was a problem communicating
// with a Poet service. It wraps the source error.
type PoetSvcUnstableError struct {
	// additional contextual information
	msg string
	// the source (if any) that caused the error
	source error
}

func (e *PoetSvcUnstableError) Error() string {
	return fmt.Sprintf("poet service is unstable: %s (%v)", e.msg, e.source)
}

func (e *PoetSvcUnstableError) Unwrap() error { return e.source }

func (e *PoetSvcUnstableError) Is(target error) bool {
	_, ok := target.(*PoetSvcUnstableError)
	return ok
}
