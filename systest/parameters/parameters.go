package parameters

import (
	"strconv"
	"time"

	"github.com/spacemeshos/go-spacemesh/systest/parameters/fastnet"
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

func Get[T any](params *Parameters, param Parameter[T]) T {
	value, exist := params.values[param.Name]
	var rst T
	if !exist {
		return param.Default
	}
	rst, err := param.fromString(value)
	if err != nil {
		panic("not a valid param " + err.Error())
	}
	return rst
}

type Parameter[T any] struct {
	Name, Description string
	Default           T
	fromString        func(string) (T, error)
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

var (
	BootstrapDuration = Parameter[time.Duration]{
		Name:        "bootstrap-duration",
		Description: "bootstrap time is added to the genesis time. it may take longer on cloud environmens due to the additional resource management",
		Default:     30 * time.Second,
		fromString:  toDuration,
	}
	ClusterSize = Parameter[int]{
		Name:        "cluster-size",
		Description: "size of the cluster. all test must use at most this number of smeshers",
		Default:     10,
		fromString:  toInt,
	}
	PoetConfig = Parameter[string]{
		Name:        "poet-config",
		Description: "configuration for poet service",
		Default:     fastnet.PoetConfig,
		fromString:  toString,
	}
	SmesherConfig = Parameter[string]{
		Name:        "smesher-config",
		Description: "configuration for smesher service",
		Default:     fastnet.SmesherConfig,
		fromString:  toString,
	}
)
