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
	time         Clock
}

func NewClock(time Clock, tickInterval time.Duration, startEpoch time.Time) *TimeClock {
	t := &TimeClock{
		Ticker:       NewTicker(time, LayerConv{duration: tickInterval, genesis: startEpoch}),
		tickInterval: tickInterval,
		startEpoch:   startEpoch,
		time:         time,
	}
	t.init()
	return t
}

func (t *TimeClock) init() {
	var diff time.Duration
	log.Info("start clock interval is %v", t.tickInterval)
	if t.time.Now().Before(t.startEpoch) {
		diff = t.startEpoch.Sub(t.time.Now())
	} else {
		diff = t.tickInterval - (t.time.Now().Sub(t.startEpoch) % t.tickInterval)
	}

	go t.startClock(diff)
}

func (t *TimeClock) startClock(diff time.Duration) {
	log.Info("starting global clock now=%v genesis=%v", t.time.Now(), t.startEpoch)
	log.Info("global clock going to sleep for %v", diff)

	tmr := time.NewTimer(diff)
	select {
	case <-tmr.C:
		break
	case <-t.stop:
		return
	}
	t.onTick()
	tick := time.NewTicker(t.tickInterval)
	log.Info("clock waiting on event, tick interval is %v", t.tickInterval)
	for {
		select {
		case <-tick.C:
			t.onTick()
		case <-t.stop:
			t.started = false
			return
		}
	}
}

func (t TimeClock) GetGenesisTime() time.Time {
	return t.startEpoch
}
