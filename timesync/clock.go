package timesync

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
	"time"
)

// Clock defines the functionality needed from any clock type
type Clock interface {
	Now() time.Time
}

// RealClock is the struct wrapping a local time struct
type RealClock struct{}

// Now returns the current local time
func (RealClock) Now() time.Time {
	return time.Now()
}

// TimeClock is the struct holding a real clock
type TimeClock struct {
	*Ticker
	tickInterval time.Duration
	startEpoch   time.Time
	stop         chan struct{}
	once         sync.Once
	log          log.Log
}

// NewClock return TimeClock struct that notifies tickInterval has passed
func NewClock(c Clock, tickInterval time.Duration, genesisTime time.Time, logger log.Log) *TimeClock {
	if tickInterval == 0 {
		logger.Panic("could not create new clock: bad configuration: tick interval is zero")
	}

	t := &TimeClock{
		Ticker:       NewTicker(c, LayerConv{duration: tickInterval, genesis: genesisTime}),
		tickInterval: tickInterval,
		startEpoch:   genesisTime,
		stop:         make(chan struct{}),
		once:         sync.Once{},
		log:          logger,
	}
	go t.startClock()
	return t
}

func (t *TimeClock) startClock() {
	t.log.Info("starting global clock now=%v genesis=%v", t.clock.Now(), t.startEpoch)

	for {
		currLayer := t.Ticker.TimeToLayer(t.clock.Now())    // get current layer
		nextTickTime := t.Ticker.LayerToTime(currLayer + 1) // get next tick time for the next layer
		diff := nextTickTime.Sub(t.clock.Now())
		tmr := time.NewTimer(diff)
		t.log.With().Info("global clock going to sleep before next layer", log.String("diff", diff.String()),
			log.FieldNamed("next_layer", currLayer))
		select {
		case <-tmr.C:
			// notify subscribers
			if missed, err := t.Notify(); err != nil {
				t.log.With().Error("could not notify subscribers", log.Err(err), log.Int("missed", missed))
			}
		case <-t.stop:
			tmr.Stop()
			return
		}
	}
}

// GetGenesisTime returns at which time this clock has started (used to calculate current tick)
func (t *TimeClock) GetGenesisTime() time.Time {
	return t.startEpoch
}

// GetInterval returns the time interval between clock ticks
func (t *TimeClock) GetInterval() time.Duration {
	return t.tickInterval
}

// Close closes the clock ticker
func (t *TimeClock) Close() {
	t.once.Do(func() {
		close(t.stop)
	})
}
