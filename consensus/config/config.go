package config

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"time"
)

type Config struct {
	NodesPerLayer    int32         `mapstructure:"nodes-per-layer"`
	PhaseTime        time.Duration `mapstructure:"phase-time"`
	StartTime        time.Time     `mapstructure:"start-time"`
	NetworkDelayMax  time.Duration `mapstructure:"network-delay-time"`
	NumOfAdverseries int32         `mapstructure:"num-of-adverseries"`
}

//todo: this is a duplicate function found also in p2p config
func duration(duration string) (dur time.Duration) {
	dur, err := time.ParseDuration(duration)
	if err != nil {
		log.Error("Could not parse duration string returning 0, error:", err)
	}
	return dur
}

func DefaultConfig() Config {
	return Config{
		PhaseTime:        duration("1s"),
		NodesPerLayer:    200,
		NetworkDelayMax:  duration("500ms"),
		StartTime:        time.Now(),
		NumOfAdverseries: 10,
	}
}
