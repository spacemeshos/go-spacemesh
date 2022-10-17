package testcontext

import (
	"bytes"
	"fmt"
	"strings"
)

type stringToString map[string]string

func (s stringToString) Set(val string) error {
	if len(val) == 0 {
		return nil
	}
	ss := strings.Split(val, ",")
	for _, pair := range ss {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("%s must be formatted as key=value", pair)
		}
		key := strings.TrimSpace(parts[0])
		out := strings.TrimSpace(parts[1])
		s[key] = out
	}
	return nil
}

func (s stringToString) Type() string {
	return "string=string"
}

func (s stringToString) String() string {
	var buf bytes.Buffer
	for k, v := range s {
		buf.WriteString(k)
		buf.WriteRune('=')
		buf.WriteString(v)
		buf.WriteRune(',')
	}
	if buf.Len() == 0 {
		return ""
	}
	return buf.String()[:buf.Len()-1]
}
