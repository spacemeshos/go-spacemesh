package flags_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/node/flags"
)

func TestDeprecatedFlag(t *testing.T) {
	err := errors.New("this is deprecated")
	f := flags.NewDeprecatedFlag(err)
	require.Equal(t, "deprecated", f.String())
	require.Equal(t, "deprecated", f.Type())
	require.Equal(t, err, f.Set("foo"))
}
