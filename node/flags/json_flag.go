package flags

import (
	"encoding/json"
)

type JSONFlag struct {
	Value any
}

func (f *JSONFlag) String() string {
	b, err := json.Marshal(f.Value)
	if err != nil {
		panic("failed to marshal object")
	}
	return string(b)
}

func (f *JSONFlag) Set(v string) error {
	return json.Unmarshal([]byte(v), f.Value)
}

func (f *JSONFlag) Type() string {
	return "json"
}
