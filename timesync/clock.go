package timesync

import (
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
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
	t.log.Info("starting global clock now=%v genesis=%v %p", t.clock.Now(), t.startEpoch, t)

	for {
		t.log.With().Info("global clock next loop")

		currLayer := t.Ticker.TimeToLayer(t.clock.Now()) // get current layer
		t.log.With().Info("global clock calculated currLayer",
			log.Uint64("currLayer", uint64(currLayer)))

		nextTickTime := t.Ticker.LayerToTime(currLayer + 1) // get next tick time for the next layer
		t.log.With().Info("global clock calculated nextTickTime",
			log.String("nextTickTime", nextTickTime.String()))

		diff := nextTickTime.Sub(t.clock.Now())
		t.log.With().Info("global clock calculated diff",
			log.String("diff", diff.String()))

		//tmr := time.NewTimer(diff)
		t.log.With().Info("global clock going to sleep before next layer",
			log.String("diff", diff.String()),
			log.FieldNamed("curr_layer", currLayer))

		time.Sleep(diff)

		//select {
		//case <-tmr.C:
		t.log.With().Info("global clock going to notify in this layer",
			log.String("diff", diff.String()),
			log.FieldNamed("curr_layer", currLayer))

		// notify subscribers
		if missed, err := t.Notify(); err != nil {
			t.log.With().Error("could not notify subscribers",
				log.Err(err),
				log.Int("missed", missed))
		}

		t.log.With().Info("global clock finished notifying in this layer, stopping timer",
			log.String("diff", diff.String()),
			log.FieldNamed("curr_layer", currLayer))

		//tmr.Stop()

		t.log.With().Info("global clock finished notifying in this layer, stopped timer",
			log.String("diff", diff.String()),
			log.FieldNamed("curr_layer", currLayer))

		//case <-t.stop:
		//	t.log.With().Info("global clock stopping timer",
		//		log.String("diff", diff.String()),
		//		log.FieldNamed("curr_layer", currLayer))
		//
		//	tmr.Stop()
		//
		//	t.log.With().Info("global clock stopped timer",
		//		log.String("diff", diff.String()),
		//		log.FieldNamed("curr_layer", currLayer))
		//
		//	return
		//}
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
	t.log.Info("closing clock %p", t)
	t.once.Do(func() {
		t.log.Info("closed clock %p", t)
		close(t.stop)
	})
}
