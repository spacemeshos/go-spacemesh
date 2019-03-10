package config

import (
	"time"
)

type Config struct {
	N             int           `mapstructure:"hare-committee-size"`     // total number of active parties
	F             int           `mapstructure:"hare-max-adversaries"`    // number of dishonest parties
	RoundDuration time.Duration `mapstructure:"hare-round-duration-sec"` // the duration of a single round
}

func DefaultConfig() Config {
	return Config{10, 5, 2 * time.Second}
}
