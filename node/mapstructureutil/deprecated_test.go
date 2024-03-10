package mapstructureutil_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/node/mapstructureutil"
)

type deprecated struct{}

func (deprecated) DeprecatedMsg() string {
	return "this is deprecated"
}

func TestDeprecatedHook(t *testing.T) {
	hook := mapstructureutil.DeprecatedHook().(func(reflect.Type, reflect.Type, any) (any, error))

	t.Run("deprecated", func(t *testing.T) {
		_, err := hook(nil, reflect.TypeOf(deprecated{}), "test")
		require.Error(t, err)
	})
	t.Run("not deprecated", func(t *testing.T) {
		d, err := hook(nil, reflect.TypeOf(1), "test")
		require.NoError(t, err)
		require.Equal(t, "test", d)
	})
}
