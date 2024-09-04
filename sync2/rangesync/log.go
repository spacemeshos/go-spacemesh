package rangesync

import (
	"encoding/hex"
	"fmt"
	"reflect"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

type seqFormatter struct {
	seq types.Seq
}

func (f seqFormatter) String() string {
	for k, err := range f.seq {
		if err != nil {
			return fmt.Sprintf("<error: %v>", err)
		}
		return fmt.Sprintf("[%s, ...]", hexStr(k))
	}
	return "<empty>"
}

func SeqField(name string, seq types.Seq) zap.Field {
	return zap.Stringer(name, seqFormatter{seq: seq})
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
	case types.KeyBytes:
		return h.String()
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
