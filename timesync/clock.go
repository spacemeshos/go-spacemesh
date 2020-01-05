package timesync

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
	"time"
)

//this package sends a tick each tickInterval to all consumers of the tick
//This also send the current mesh.LayerID  which is calculated from the number of ticks passed since epoch
type LayerTimer chan types.LayerID

type ManualClock struct {
	*Ticker
	genesisTime time.Time
}

func (t *ManualClock) Tick() {
	t.notifyOnTick()
}

func NewManualClock(genesisTime time.Time) *ManualClock {
	t := &ManualClock{
		Ticker: &Ticker{
			nextLayerToTick: 1,
			stop:            make(chan struct{}),
			subscribers:     make(map[LayerTimer]struct{}),
		},
		genesisTime: genesisTime,
	}
	t.StartNotifying()
	return t
}

func (t ManualClock) GetGenesisTime() time.Time {
	return t.genesisTime
}

type TimeClock struct {
	*Ticker
	tickInterval time.Duration
	startEpoch   time.Time
	time         Clock
}

func NewTicker(time Clock, tickInterval time.Duration, startEpoch time.Time) *TimeClock {
	t := &TimeClock{
		Ticker: &Ticker{
			nextLayerToTick: 1,
			stop:            make(chan struct{}),
			subscribers:     make(map[LayerTimer]struct{}),
		},
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
		t.nextLayerToTick = 1
		diff = t.startEpoch.Sub(t.time.Now())
	} else {
		t.updateLayerID()
		diff = t.tickInterval - (t.time.Now().Sub(t.startEpoch) % t.tickInterval)
	}

	go t.startClock(diff)
}

func (t *TimeClock) updateLayerID() {
	t.nextLayerToTick = types.LayerID((t.time.Now().Sub(t.startEpoch) / t.tickInterval) + 2)
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
	t.notifyOnTick()
	tick := time.NewTicker(t.tickInterval)
	log.Info("clock waiting on event, tick interval is %v", t.tickInterval)
	for {
		select {
		case <-tick.C:
			t.notifyOnTick()
		case <-t.stop:
			t.started = false
			return
		}
	}
}

func (t TimeClock) GetGenesisTime() time.Time {
	return t.startEpoch
}

type Ticker struct {
	nextLayerToTick types.LayerID
	m               sync.RWMutex
	stop            chan struct{}
	subscribers     map[LayerTimer]struct{} // map subscribers by channel
	started         bool
	once            sync.Once
}

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

func (t *Ticker) StartNotifying() {
	log.Info("started notifying")
	t.started = true
}

func (t *Ticker) Close() {
	t.once.Do(func() {
		close(t.stop)
	})
}

func (t *Ticker) notifyOnTick() {
	if !t.started {
		return
	}

	t.m.Lock()
	log.Event().Info("release tick", log.LayerId(uint64(t.nextLayerToTick)))
	count := 1
	wg := sync.WaitGroup{}
	wg.Add(len(t.subscribers))
	for ch := range t.subscribers {
		c := ch
		go func(count int) {
			c <- t.nextLayerToTick
			log.Debug("iv'e notified number : %v", count)
			wg.Done()
		}(count)
		count++
	}
	wg.Wait()
	log.Debug("Ive notified all")
	t.nextLayerToTick++
	t.m.Unlock()
}

func (t *Ticker) GetCurrentLayer() types.LayerID {
	t.m.RLock()
	currentLayer := t.nextLayerToTick - 1 // nextLayerToTick is ensured to be >= 1
	t.m.RUnlock()
	return currentLayer
}

func (t *Ticker) Subscribe() LayerTimer {
	ch := make(LayerTimer)
	t.m.Lock()
	t.subscribers[ch] = struct{}{}
	t.m.Unlock()
	log.Info("subscribed to channel")
	return ch
}

func (t *Ticker) Unsubscribe(ch LayerTimer) {
	t.m.Lock()
	delete(t.subscribers, ch)
	t.m.Unlock()
}
