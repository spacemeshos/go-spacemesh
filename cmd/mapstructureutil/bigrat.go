package mapstructureutil

import (
	"errors"
	"math/big"
	"reflect"

	"github.com/mitchellh/mapstructure"
)

// BigRatDecodeFunc mapstructure decode func for big.Rat.
func BigRatDecodeFunc() mapstructure.DecodeHookFunc {
	return func(f reflect.Type, t reflect.Type, data any) (any, error) {
		if t != reflect.TypeOf(big.Rat{}) {
			return data, nil
		}

		switch f.Kind() {
		case reflect.String:
			v, ok := new(big.Rat).SetString(data.(string))
			if !ok {
				return nil, errors.New("malformed string representing big.Rat was provided")
			}

			return v, nil
		case reflect.Float64:
			return new(big.Rat).SetFloat64(data.(float64)), nil
		case reflect.Int64:
			return new(big.Rat).SetInt64(data.(int64)), nil
		case reflect.Uint64:
			return new(big.Rat).SetUint64(data.(uint64)), nil
		default:
			return data, nil
		}
	}
}
