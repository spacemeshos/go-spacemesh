package flags

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// StringToUint64Value is a flag type for string to uint64 values.
type StringToUint64Value struct {
	value map[string]uint64
}

// NewStringToUint64Value creates instance.
func NewStringToUint64Value(p map[string]uint64) *StringToUint64Value {
	return &StringToUint64Value{value: p}
}

// Set expects value as "smth=101,else=102".
func (s *StringToUint64Value) Set(val string) error {
	ss := strings.Split(val, ",")
	for _, pair := range ss {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("%s must be formatted as key=value", pair)
		}
		out, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return err
		}
		key := strings.TrimSpace(parts[0])
		s.value[key] = out
	}
	return nil
}

// Type returns stringToUint64 type.
func (s *StringToUint64Value) Type() string {
	return "string=uint64"
}

// String marshals value of the StringToUint64Value instance.
func (s *StringToUint64Value) String() string {
	var buf bytes.Buffer
	for k, v := range s.value {
		buf.WriteString(k)
		buf.WriteRune('=')
		buf.WriteString(strconv.FormatUint(v, 10))
		buf.WriteRune(',')
	}
	if buf.Len() == 0 {
		return ""
	}
	return buf.String()[:buf.Len()-1]
}

// CastStringToMapStringUint64 casts string with comma separated values to map[string]uint64.
// Nil is returned if value fails format validation (consistnt with viper behavior).
func CastStringToMapStringUint64(value string) map[string]uint64 {
	val := map[string]uint64{}
	parser := NewStringToUint64Value(val)
	if err := parser.Set(value); err != nil {
		return nil
	}
	return val
}
