package parameters

import (
	"fmt"
	"strconv"
	"time"
)

// New creates Parameters instance with default values.
func New() *Parameters {
	return &Parameters{values: map[string]string{}}
}

// FromValues instantiates parameters with a cope of values.
func FromValues(values map[string]string) *Parameters {
	p := New()
	p.Update(values)
	return p
}

type Parameters struct {
	values map[string]string
}

func (p *Parameters) Update(values map[string]string) {
	for name, value := range values {
		p.values[name] = value
	}
}

type Parser[T any] func(string) (T, error)

type Parameter[T any] struct {
	name, desc string
	defaults   T
	parser     Parser[T]
}

func (p *Parameter[T]) Get(params *Parameters) T {
	value, exist := params.values[p.name]
	var rst T
	if !exist {
		return p.defaults
	}
	rst, err := p.parser(value)
	if err != nil {
		panic("not a valid param " + err.Error())
	}
	return rst
}

func NewParameter[T any](name, desc string, defaults T, parser Parser[T]) Parameter[T] {
	return Parameter[T]{name: name, desc: desc, defaults: defaults, parser: parser}
}

func String(name, desc, defaults string) Parameter[string] {
	return NewParameter(name, desc, defaults, toString)
}

func Bytes(name, desc string, defaults []byte) Parameter[[]byte] {
	return NewParameter(name, desc, defaults, toBytes)
}

func Duration(name, desc string, defaults time.Duration) Parameter[time.Duration] {
	return NewParameter(name, desc, defaults, toDuration)
}

func Int(name, desc string, defaults int) Parameter[int] {
	return NewParameter(name, desc, defaults, toInt)
}

func Bool(name, desc string) Parameter[bool] {
	return NewParameter(name, desc, false, func(value string) (bool, error) {
		switch value {
		case "true":
			return true, nil
		case "1":
			return true, nil
		case "false":
			return false, nil
		case "0":
			return false, nil
		}
		return false, fmt.Errorf("not a boolean %v", value)
	})
}

func toBytes(value string) ([]byte, error) {
	return []byte(value), nil
}

func toString(value string) (string, error) {
	return value, nil
}

func toInt(value string) (int, error) {
	rst, err := strconv.ParseInt(value, 0, 0)
	if err != nil {
		return 0, err
	}
	return int(rst), nil
}

func toDuration(value string) (time.Duration, error) {
	rst, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	return rst, nil
}
