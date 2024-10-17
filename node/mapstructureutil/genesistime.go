package mapstructureutil

import (
	"reflect"

	"github.com/mitchellh/mapstructure"

	"github.com/spacemeshos/go-spacemesh/config"
)

// GenesisDecodeFunc mapstructure decode func for config.Genesis.
func GenesisDecodeFunc() mapstructure.DecodeHookFunc {
	return func(f, t reflect.Type, data any) (any, error) {
		if t != reflect.TypeOf(config.Genesis{}) {
			return data, nil
		}

		switch f.Kind() {
		case reflect.String:
			var t config.Genesis
			err := t.Set(data.(string))
			return t, err
		default:
			return data, nil
		}
	}
}
