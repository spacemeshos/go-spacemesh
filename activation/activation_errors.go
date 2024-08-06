package activation

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrATXChallengeExpired is returned when atx missed its publication window and needs to be regenerated.
	ErrATXChallengeExpired = errors.New("builder: atx expired")
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

type PoetRegistrationMismatchError struct {
	registrations   []string
	configuredPoets []string
}

func (e *PoetRegistrationMismatchError) Error() string {
	var sb strings.Builder
	sb.WriteString("builder: none of configured poets matches the existing registrations.\n")
	sb.WriteString("registrations:\n")
	for _, r := range e.registrations {
		sb.WriteString("\t")
		sb.WriteString(r)
		sb.WriteString("\n")
	}
	sb.WriteString("\n configured poets:\n")
	for _, p := range e.configuredPoets {
		sb.WriteString("\t")
		sb.WriteString(p)
		sb.WriteString("\n")
	}
	return sb.String()
}

func (e *PoetRegistrationMismatchError) As(target any) bool {
	var poetRegistrationMismatchError *PoetRegistrationMismatchError
	return errors.As(poetRegistrationMismatchError, target)
}
