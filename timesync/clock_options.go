package timesync

import (
	"errors"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
)

type option struct {
	clock         clockwork.Clock
	genesisTime   time.Time
	layerDuration time.Duration
	tickInterval  time.Duration

	log *zap.Logger
}

func (o *option) validate() error {
	if o.genesisTime.IsZero() {
		return errors.New("bad configuration: genesis time is zero")
	}

	if o.layerDuration == 0 {
		return errors.New("bad configuration: layer duration is zero")
	}

	if o.tickInterval == 0 {
		return errors.New("bad configuration: tick interval is zero")
	}

	if o.tickInterval < 0 || o.tickInterval > o.layerDuration {
		return errors.New("bad configuration: tick interval must be between 0 and layer duration")
	}

	if o.log == nil {
		return errors.New("bad configuration: logger is nil")
	}
	return nil
}

type OptionFunc func(*option) error

// withClock specifies which clock the NodeClock should use. Defaults to the real clock.
func withClock(clock clockwork.Clock) OptionFunc {
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

func WithTickInterval(d time.Duration) OptionFunc {
	return func(opts *option) error {
		opts.tickInterval = d
		return nil
	}
}

// WithLogger sets the logger for the NodeClock.
func WithLogger(logger *zap.Logger) OptionFunc {
	return func(opts *option) error {
		opts.log = logger
		return nil
	}
}
