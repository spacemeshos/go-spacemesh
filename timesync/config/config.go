package config

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
)

// ConfigValues specifies  default values for node config params.
var (
	TimeConfigValues = DefaultConfig()
)

// TimeConfig specifies the timesync params for ntp.
type TimeConfig struct {
	MaxAllowedDrift       time.Duration `mapstructure:"max-allowed-time-drift"`
	NtpQueries            int           `mapstructure:"ntp-queries"`
	DefaultTimeoutLatency time.Duration `mapstructure:"default-timeout-latency"`
	RefreshNtpInterval    time.Duration `mapstructure:"refresh-ntp-interval"`
}

//todo: this is a duplicate function found also in p2p config
func duration(duration string) (dur time.Duration) {
	dur, err := time.ParseDuration(duration)
	if err != nil {
		log.Error("Could not parse duration string returning 0, error:", err)
	}
	return dur
}

// DefaultConfig defines the default tymesync configuration
func DefaultConfig() TimeConfig {

	// TimeConfigValues defines default values for all time and ntp related params.
	var TimeConfigValues = TimeConfig{
		MaxAllowedDrift:       duration("10s"),
		NtpQueries:            5,
		DefaultTimeoutLatency: duration("10s"),
		RefreshNtpInterval:    duration("30m"),
	}

	return TimeConfigValues
}
