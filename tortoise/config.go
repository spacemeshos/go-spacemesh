package tortoise

import (
	"math/big"
	"time"
)

// Config is the configuration of the Tortoise.
type Config struct {
	Hdist           uint32        `mapstructure:"hdist"`            // hare/input vector lookback distance
	Zdist           uint32        `mapstructure:"zdist"`            // hare result wait distance
	ConfidenceParam uint32        `mapstructure:"confidence-param"` // layers to wait for global consensus
	WindowSize      uint32        `mapstructure:"window-size"`      // size of the tortoise sliding window (in layers)
	GlobalThreshold *big.Rat      `mapstructure:"global-threshold"` // threshold for finalizing blocks and layers
	LocalThreshold  *big.Rat      `mapstructure:"local-threshold"`  // threshold for choosing when to use weak coin
	RerunInterval   time.Duration `mapstructure:"rerun-interval"`
}

// DefaultConfig defines the default Tortoise configuration
func DefaultConfig() Config {
	return Config{
		Hdist:           10,
		Zdist:           5,
		ConfidenceParam: 5,
		WindowSize:      100,                 // should be "a few thousand layers" in production
		GlobalThreshold: big.NewRat(60, 100), // fraction
		LocalThreshold:  big.NewRat(20, 100), // fraction
		RerunInterval:   24 * time.Minute,    // in minutes, once per day
	}
}
