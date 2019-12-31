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

type Ticker struct {
	nextLayerToTick types.LayerID
	m               sync.RWMutex
	tickInterval    time.Duration
	startEpoch      time.Time
	time            Clock
	stop            chan struct{}
	subscribers     map[LayerTimer]struct{} // map subscribers by channel
	layerChannels   map[types.LayerID]chan struct{}
	started         bool
}

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now()
}

func NewTicker(time Clock, tickInterval time.Duration, startEpoch time.Time) *Ticker {
	t := &Ticker{
		nextLayerToTick: 1,
		tickInterval:    tickInterval,
		startEpoch:      startEpoch,
		time:            time,
		stop:            make(chan struct{}),
		subscribers:     make(map[LayerTimer]struct{}),
		layerChannels:   make(map[types.LayerID]chan struct{}),
	}
	t.init()
	return t
}

func (t *Ticker) init() {
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

func (t *Ticker) StartNotifying() {
	t.started = true
}

func (t *Ticker) Close() {
	close(t.stop)
}

func (t *Ticker) notifyOnTick() {
	if !t.started {
		return
	}

	t.m.Lock()
	log.Event().Info("release tick", log.LayerId(uint64(t.nextLayerToTick)))
	count := 1
	for ch := range t.subscribers {
		ch <- t.nextLayerToTick
		log.Debug("I've notified number: %v", count)
		count++
	}
	if layerChan, found := t.layerChannels[t.nextLayerToTick]; found {
		close(layerChan)
		delete(t.layerChannels, t.nextLayerToTick)
	}
	log.Debug("I've notified all")
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

	return ch
}

func (t *Ticker) Unsubscribe(ch LayerTimer) {
	t.m.Lock()
	delete(t.subscribers, ch)
	t.m.Unlock()
}

var closedChan = make(chan struct{})

func init() {
	close(closedChan)
}

func (t *Ticker) SubscribeLayer(layerId types.LayerID) chan struct{} {
	t.m.Lock()
	defer t.m.Unlock()

	if t.nextLayerToTick > layerId {
		return closedChan
	}

	ch := t.layerChannels[layerId]
	if ch == nil {
		ch = make(chan struct{})
		t.layerChannels[layerId] = ch
	}
	return ch
}

func (t *Ticker) updateLayerID() {
	t.nextLayerToTick = types.LayerID((t.time.Now().Sub(t.startEpoch) / t.tickInterval) + 2)
}

func (t *Ticker) startClock(diff time.Duration) {
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
			return
		}
	}
}

func (t Ticker) GetGenesisTime() time.Time {
	return t.startEpoch
}
