package mapstructureutil

import (
	"errors"
	"reflect"

	"github.com/mitchellh/mapstructure"

	"github.com/spacemeshos/go-spacemesh/log"
)

// Deprecated is an interface for deprecated config fields.
// A deprecated config field should implement this interface (with a value receiver)
// and return a message explaining the deprecation.
type Deprecated interface {
	DeprecatedMsg() string
}

func DeprecatedHook() mapstructure.DecodeHookFunc {
	return func(f, t reflect.Type, data any) (any, error) {
		if t.Implements(reflect.TypeOf((*Deprecated)(nil)).Elem()) {
			instance := reflect.New(t).Elem().Interface().(Deprecated)
			log.GetLogger().Error(instance.DeprecatedMsg())
			return nil, errors.New("use of deprecated config field")
		}
		return data, nil
	}
}
