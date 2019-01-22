package config

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
)

var (
	Values = DefaultConfig()
)

func duration(duration string) (dur time.Duration) {
	dur, err := time.ParseDuration(duration)
	if err != nil {
		log.Error("Could not parse duration string returning 0, error:", err)
	}
	return dur
}

// Config specifies the timesync params for ntp.
type Config struct {
	MaxAllowedDrift       time.Duration `mapstructure:"max-allowed-time-drift"`
	NtpQueries            int           `mapstructure:"ntp-queries"`
	DefaultTimeoutLatency time.Duration `mapstructure:"default-timeout-latency"`
	RefreshNtpInterval    time.Duration `mapstructure:"refresh-ntp-interval"`
}

func DefaultConfig() Config {
	return Config{
		MaxAllowedDrift:       duration("10s"),
		NtpQueries:            5,
		DefaultTimeoutLatency: duration("10s"),
		RefreshNtpInterval:    duration("30m"),
	}
}
