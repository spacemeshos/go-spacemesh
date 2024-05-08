package mapstructureutil

import (
	"reflect"

	"github.com/mitchellh/mapstructure"

	"github.com/spacemeshos/go-spacemesh/activation"
)

// AtxVersionsDecodeFunc mapstructure decode func for activation.AtxVersions.
func AtxVersionsDecodeFunc() mapstructure.DecodeHookFunc {
	return func(f, t reflect.Type, data any) (any, error) {
		if t != reflect.TypeOf(activation.AtxVersions{}) {
			return data, nil
		}

		if f.Kind() != reflect.Map {
			return data, nil
		}

		var result activation.AtxVersions
		err := mapstructure.WeakDecode(data, &result)
		if err != nil {
			return nil, err
		}

		return result, result.Validate()
	}
}
