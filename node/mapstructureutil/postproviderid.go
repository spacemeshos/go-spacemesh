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
			if value > math.MaxUint32 || value < 0 || value != math.Trunc(value) {
				return nil, fmt.Errorf("invalid provider ID value: %v", value)
			}
			id := activation.PostProviderID{}
			id.SetUint32(uint32(value))
			return id, nil
		case reflect.String:
			return nil, fmt.Errorf("invalid provider ID value: %v", data)
		default:
			return data, nil
		}
	}
}
