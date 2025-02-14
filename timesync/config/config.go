package config

import (
	"github.com/spacemeshos/go-spacemesh/timesync/peersync"
)

// TimeConfig specifies the timesync params for ntp.
type TimeConfig struct {
	Peersync peersync.Config `mapstructure:"peersync"`
}

// DefaultConfig defines the default tymesync configuration.
func DefaultConfig() TimeConfig {
	// TimeConfigValues defines default values for all time and ntp related params.
	return TimeConfig{
		Peersync: peersync.DefaultConfig(),
	}
}
