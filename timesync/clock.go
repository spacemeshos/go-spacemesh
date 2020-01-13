package timesync

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"time"
)

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

type TimeClock struct {
	*Ticker
	tickInterval time.Duration
	startEpoch   time.Time
	log          log.Log
}

func NewClock(c Clock, tickInterval time.Duration, startEpoch time.Time) *TimeClock {
	t := &TimeClock{
		Ticker:       NewTicker(c, LayerConv{duration: tickInterval, genesis: startEpoch}),
		tickInterval: tickInterval,
		startEpoch:   startEpoch,
		log:          log.NewDefault("clock"),
	}
	go t.startClock()
	return t
}

func (t *TimeClock) startClock() {
	t.log.Info("starting global clock now=%v genesis=%v", t.clock.Now(), t.startEpoch)

	for {
		currLayer := t.Ticker.conv.TimeToLayer(t.clock.Now())    // get current layer
		nextTickTime := t.Ticker.conv.LayerToTime(currLayer + 1) // get next tick time for the next layer
		diff := nextTickTime.Sub(t.clock.Now())
		tmr := time.NewTimer(diff)
		t.log.Info("global clock going to sleep for %v", diff)
		select {
		case <-tmr.C:
			t.Notify() // notify subscribers
			continue
		case <-t.stop:
			tmr.Stop()
			return
		}
	}
}

func (t TimeClock) GetGenesisTime() time.Time {
	return t.startEpoch
}
