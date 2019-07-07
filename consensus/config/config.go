package config

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"time"
)

// Config is the main configuration of the dolev strong parameters
type Config struct {
	NodesPerLayer    int32         `mapstructure:"nodes-per-layer"`
	LayersPerEpoch   int           `mapstructure:"layers-per-epoch"`
	RoundTime        time.Duration `mapstructure:"phase-time"`
	StartTime        time.Time     `mapstructure:"start-time"`
	NetworkDelayMax  time.Duration `mapstructure:"network-delay-time"`
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
		NodesPerLayer:    200,
		LayersPerEpoch:   3, // 12 layers/hr * 24 hrs/day * 14 days/epoch TODO: make it 4096 so it's 2^12?
		RoundTime:        duration("1s"),
		StartTime:        time.Now(),
		NetworkDelayMax:  duration("500ms"),
		NumOfAdversaries: 10,
	}
}
