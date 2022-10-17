package testcontext

import "bytes"

type stringSet map[string]struct{}

func (s stringSet) Set(val string) error {
	if len(val) == 0 {
		return nil
	}
	s[val] = struct{}{}
	return nil
}

func (s stringSet) Type() string {
	return "[]string"
}

func (s stringSet) String() string {
	var buf bytes.Buffer
	for k := range s {
		buf.WriteString(k)
		buf.WriteRune(',')
	}
	if buf.Len() == 0 {
		return ""
	}
	return buf.String()[:buf.Len()-1]
}
