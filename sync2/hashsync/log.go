package hashsync

import (
	"encoding/hex"
	"reflect"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func IteratorField(name string, it Iterator) zap.Field {
	if it == nil {
		return zap.String(name, "<nil>")
	}
	return HexField(name, it.Key())
}

// based on code from testify
func isNil(object any) bool {
	if object == nil {
		return true
	}

	value := reflect.ValueOf(object)
	switch value.Kind() {
	case
		reflect.Chan, reflect.Func,
		reflect.Interface, reflect.Map,
		reflect.Ptr, reflect.Slice, reflect.UnsafePointer:

		return value.IsNil()
	}

	return false
}

func HexField(name string, k any) zap.Field {
	switch h := k.(type) {
	case types.Hash32:
		return zap.String(name, h.ShortString())
	case types.Hash12:
		return zap.String(name, hex.EncodeToString(h[:5]))
	case []byte:
		if len(h) > 5 {
			h = h[:5]
		}
		return zap.String(name, hex.EncodeToString(h[:5]))
	default:
		if isNil(k) {
			return zap.String(name, "<nil>")
		}
		panic("unexpected type")
	}
}
