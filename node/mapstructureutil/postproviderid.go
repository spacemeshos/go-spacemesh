package mapstructureutil

import (
	"fmt"
	"math"
	"reflect"

	"github.com/mitchellh/mapstructure"

	"github.com/spacemeshos/go-spacemesh/activation"
)

// PostProviderIDDecodeFunc mapstructure decode func for activation.PostProviderID.
func PostProviderIDDecodeFunc() mapstructure.DecodeHookFunc {
	return func(f, t reflect.Type, data any) (any, error) {
		if t != reflect.TypeOf(activation.PostProviderID{}) {
			return data, nil
		}

		switch f.Kind() {
		case reflect.Float64:
			value := data.(float64)
			// TODO(mafa): update check from `value < -1` to `value < 0`, see https://github.com/spacemeshos/go-spacemesh/issues/4801
			if value > math.MaxUint32 || value < -1 || value != math.Trunc(value) {
				return nil, fmt.Errorf("invalid provider ID value: %v", value)
			}
			id := activation.PostProviderID{}
			id.SetInt64(int64(value))
			return id, nil
		case reflect.String:
			return nil, fmt.Errorf("invalid provider ID value: %v", data)
		default:
			return data, nil
		}
	}
}
