package presets

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/config"
)

var presets = map[string]config.Config{}

func register(name string, preset config.Config) {
	if _, exist := presets[name]; exist {
		panic(fmt.Sprintf("preset with name %s already exists", name))
	}
	presets[name] = preset
}

// Options returns list of registered options.
func Options() []string {
	var options []string
	for name := range presets {
		options = append(options, name)
	}
	return options
}

// Get return one of the available preset.
func Get(name string) (config.Config, error) {
	config, exists := presets[name]
	if !exists {
		return config, fmt.Errorf("preset %s is not registered. select one from the options %+s",
			name, Options())
	}
	return config, nil
}
