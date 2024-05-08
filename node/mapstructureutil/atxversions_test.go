package mapstructureutil_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/node/mapstructureutil"
)

func TestAtxVersionsHook(t *testing.T) {
	hook := mapstructureutil.AtxVersionsDecodeFunc().(func(reflect.Type, reflect.Type, any) (any, error))

	t.Run("valid", func(t *testing.T) {
		data := map[string]any{"2": types.AtxV2}

		result, err := hook(reflect.TypeOf(data), reflect.TypeOf(activation.AtxVersions{}), data)
		require.NoError(t, err)

		value, ok := result.(activation.AtxVersions)
		require.True(t, ok)
		require.EqualValues(t, map[types.EpochID]types.AtxVersion{2: types.AtxV2}, value)
	})
	t.Run("invalid", func(t *testing.T) {
		data := map[string]any{"2": types.AtxVMAX + 1}

		_, err := hook(reflect.TypeOf(data), reflect.TypeOf(activation.AtxVersions{}), data)
		require.Error(t, err)
	})
}
