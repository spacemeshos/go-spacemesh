package flags_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/node/flags"
)

type deprecatedFlag struct{}

func (deprecatedFlag) DeprecatedMsg() string {
	return "this is deprecated"
}

func TestDeprecatedFlag(t *testing.T) {
	f := flags.NewDeprecatedFlag(deprecatedFlag{})
	require.Equal(t, "deprecated", f.String())
	require.Equal(t, "deprecated", f.Type())
	require.ErrorContains(t, f.Set("foo"), "deprecated")
}
