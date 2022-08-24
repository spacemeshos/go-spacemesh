package config

import (
	"github.com/spacemeshos/go-spacemesh/timesync/peersync"
)

// ConfigValues specifies  default values for node config params.
var (
	TimeConfigValues = DefaultConfig()
)

// TimeConfig specifies the timesync params for ntp.
type TimeConfig struct {
	Peersync peersync.Config `mapstructure:"peersync"`
}

// DefaultConfig defines the default tymesync configuration.
func DefaultConfig() TimeConfig {
	// TimeConfigValues defines default values for all time and ntp related params.
	TimeConfigValues := TimeConfig{
		Peersync: peersync.DefaultConfig(),
	}

	return TimeConfigValues
}
