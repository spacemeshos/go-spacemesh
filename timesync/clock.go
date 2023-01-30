package timesync

import (
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
)

// Clock defines the functionality needed from any clock type.
type Clock interface {
	Now() time.Time
}

// RealClock is the struct wrapping a local time struct.
type RealClock struct{}

// Now returns the current local time.
func (RealClock) Now() time.Time {
	return time.Now()
}

// TimeClock is the struct holding a real clock.
type TimeClock struct {
	*Ticker
	tickInterval time.Duration
	genesis      time.Time
	stop         chan struct{}
	once         sync.Once
	log          log.Log
	eg           errgroup.Group
}

// NewClock return TimeClock struct that notifies tickInterval has passed.
func NewClock(c Clock, tickInterval time.Duration, genesisTime time.Time, logger log.Log) *TimeClock {
	if tickInterval == 0 {
		logger.Panic("could not create new clock: bad configuration: tick interval is zero")
	}
	gtime := genesisTime.Local()
	logger.With().Info("converting genesis time to local time",
		log.Time("genesis", genesisTime),
		log.Time("local", gtime),
	)
	t := &TimeClock{
		Ticker:       NewTicker(c, LayerConv{duration: tickInterval, genesis: gtime}, WithLog(logger)),
		tickInterval: tickInterval,
		genesis:      gtime,
		stop:         make(chan struct{}),
		log:          logger,
	}

	t.eg.Go(t.startClock)
	return t
}

func (t *TimeClock) startClock() error {
	t.log.Info("starting global clock now=%v genesis=%v %p", t.clock.Now(), t.genesis, t)

	for {
		currLayer := t.Ticker.TimeToLayer(t.clock.Now())
		nextLayer := currLayer.Add(1)
		if time.Until(t.Ticker.LayerToTime(currLayer)) > 0 {
			nextLayer = currLayer
		}
		nextTickTime := t.Ticker.LayerToTime(nextLayer)
		t.log.With().Info("global clock going to sleep before next layer",
			log.Stringer("curr_layer", currLayer),
			log.Stringer("next_layer", nextLayer),
			log.Time("next_tick_time", nextTickTime),
		)

		select {
		case <-time.After(nextTickTime.Sub(t.clock.Now())):
		case <-t.stop:
			t.log.Info("stopping global clock %p", t)
			return nil
		}

		if err := t.Notify(); err != nil {
			t.log.With().Warning("error notifying clock subscribers", log.Err(err))
		}
	}
}

// GetGenesisTime returns at which time this clock has started (used to calculate current tick).
func (t *TimeClock) GetGenesisTime() time.Time {
	return t.genesis
}

// GetInterval returns the time interval between clock ticks.
func (t *TimeClock) GetInterval() time.Duration {
	return t.tickInterval
}

// Close closes the clock ticker.
func (t *TimeClock) Close() {
	t.once.Do(func() {
		t.log.Info("stopping clock")
		close(t.stop)
		if err := t.eg.Wait(); err != nil {
			t.log.Error("errgroup: %v", err)
		}
		t.log.Info("clock stopped")
	})
}
