package timesync

import (
	"fmt"
	"time"

	"github.com/benbjohnson/clock"

	"github.com/spacemeshos/go-spacemesh/log"
)

type option struct {
	clock         clock.Clock
	genesisTime   time.Time
	layerDuration time.Duration

	log *log.Log
}

func (o *option) validate() error {
	if o.genesisTime.IsZero() {
		return fmt.Errorf("bad configuration: genesis time is zero")
	}

	if o.layerDuration == 0 {
		return fmt.Errorf("bad configuration: tick interval is zero")
	}

	if o.log == nil {
		return fmt.Errorf("bad configuration: logger is nil")
	}
	return nil
}

type OptionFunc func(*option) error

// withClock specifies which clock the NodeClock should use. Defaults to the real clock.
func withClock(clock clock.Clock) OptionFunc {
	return func(opts *option) error {
		opts.clock = clock
		return nil
	}
}

// WithGenesisTime sets the genesis time for the NodeClock.
func WithGenesisTime(genesis time.Time) OptionFunc {
	return func(opts *option) error {
		opts.genesisTime = genesis
		return nil
	}
}

// WithLayerDuration sets the layer duration for the NodeClock.
func WithLayerDuration(d time.Duration) OptionFunc {
	return func(opts *option) error {
		opts.layerDuration = d
		return nil
	}
}

// WithLogger sets the logger for the NodeClock.
func WithLogger(logger log.Log) OptionFunc {
	return func(opts *option) error {
		opts.log = &logger
		return nil
	}
}
