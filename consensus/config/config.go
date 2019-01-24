package config

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"time"
)

// Config is the main configuration of the dolev strong parameters
type Config struct {
	NodesPerLayer    int32         `mapstructure:"nodes-per-layer"`
	RoundTime        time.Duration `mapstructure:"phase-time"`
	NumOfAdversaries int32         `mapstructure:"num-of-adversaries"`
}

//todo: this is a duplicate function found also in p2p config
func duration(duration string) (dur time.Duration) {
	dur, err := time.ParseDuration(duration)
	if err != nil {
		log.Error("Could not parse duration string returning 0, error:", err)
	}
	return dur
}

// DefaultConfig returns the default values of dolev strong configuration
func DefaultConfig() Config {
	return Config{
		RoundTime:        duration("1s"),
		NodesPerLayer:    200,
		NumOfAdversaries: 10,
	}
}
