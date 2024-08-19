package hashsync

import (
	"encoding/hex"
	"fmt"
	"reflect"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type itFormatter struct {
	it Iterator
}

func (f itFormatter) String() string {
	k, err := f.it.Key()
	if err != nil {
		return fmt.Sprintf("<error: %v>", err)
	}
	return hexStr(k)
}

func IteratorField(name string, it Iterator) zap.Field {
	if it == nil {
		return zap.String(name, "<nil>")
	}
	return zap.Stringer(name, itFormatter{it: it})
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

func hexStr(k any) string {
	switch h := k.(type) {
	case types.Hash32:
		return h.ShortString()
	case types.Hash12:
		return hex.EncodeToString(h[:5])
	case []byte:
		if len(h) > 5 {
			h = h[:5]
		}
		return hex.EncodeToString(h[:5])
	case string:
		return h
	case fmt.Stringer:
		return h.String()
	default:
		if isNil(k) {
			return "<nil>"
		}
		return fmt.Sprintf("<unexpected type: %T>", k)
	}
}

func HexField(name string, k any) zap.Field {
	return zap.String(name, hexStr(k))
}
