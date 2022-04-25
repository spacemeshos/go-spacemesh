package config

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
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

// todo: this is a duplicate function found also in p2p config
func duration(duration string) (dur time.Duration) {
	dur, err := time.ParseDuration(duration)
	if err != nil {
		log.Error("Could not parse duration string returning 0, error:", err)
	}
	return dur
}

// DefaultConfig defines the default tymesync configuration.
func DefaultConfig() TimeConfig {
	// TimeConfigValues defines default values for all time and ntp related params.
	TimeConfigValues := TimeConfig{
		Peersync: peersync.DefaultConfig(),
	}

	return TimeConfigValues
}
