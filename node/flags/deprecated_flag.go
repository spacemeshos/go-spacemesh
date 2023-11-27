package flags

import (
	"errors"

	"github.com/spacemeshos/go-spacemesh/log"
)

// Deprecated is an interface for deprecated config fields.
// A deprecated config field should implement this interface (with a value receiver)
// and return a message explaining the deprecation.
type Deprecated interface {
	DeprecatedMsg() string
}
type deprecatedFlag struct {
	inner Deprecated
}

func (f *deprecatedFlag) Set(val string) error {
	log.GetLogger().Error(f.inner.DeprecatedMsg())
	return errors.New("use of deprecated flag")
}

func (f *deprecatedFlag) Type() string {
	return "deprecated"
}

func (f *deprecatedFlag) String() string {
	return "deprecated"
}

// NewDeprecatedFlag returns a flag that returns an error when used.
func NewDeprecatedFlag(v Deprecated) *deprecatedFlag {
	return &deprecatedFlag{inner: v}
}
