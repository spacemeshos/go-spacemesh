package flags

type deprecatedFlag struct {
	// error to return when flag is used
	err error
}

func (f *deprecatedFlag) Set(val string) error {
	return f.err
}

func (f *deprecatedFlag) Type() string {
	return "deprecated"
}

func (f *deprecatedFlag) String() string {
	return "deprecated"
}

// NewDeprecatedFlag returns a flag that returns an error when used.
func NewDeprecatedFlag(err error) *deprecatedFlag {
	return &deprecatedFlag{err: err}
}
