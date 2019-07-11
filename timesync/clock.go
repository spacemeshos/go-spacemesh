package timesync

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
	"time"
)

//this package sends a tick each tickInterval to all consumers of the tick
//This also send the current mesh.LayerID  which is calculated from the number of ticks passed since epoch
type LayerTimer chan types.LayerID

const GenesisLayer = types.LayerID(1)

type Ticker struct {
	subscribes   []LayerTimer
	currentLayer types.LayerID
	m            sync.RWMutex
	tickInterval time.Duration
	startEpoch   time.Time
	time         Clock
	stop         chan struct{}
	ids          map[LayerTimer]int
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
		subscribes:   make([]LayerTimer, 0, 0),
		currentLayer: 1, //todo we dont need a tick for layer 0
		tickInterval: tickInterval,
		startEpoch:   startEpoch,
		time:         time,
		stop:         make(chan struct{}),
		ids:          make(map[LayerTimer]int),
	}

	if !time.Now().Before(startEpoch) {
		t.updateLayerID()
	}

	return t
}

func (t *Ticker) Start() {
	var diff time.Duration
	log.Info("start clock interval is %v", t.tickInterval)
	if t.time.Now().Before(t.startEpoch) {
		t.currentLayer = 1
		diff = t.startEpoch.Sub(t.time.Now())
	} else {
		t.updateLayerID()
		diff = ((t.time.Now().Sub(t.startEpoch)) / t.tickInterval) + t.tickInterval
	}

	go t.StartClock(diff)
}

func (t *Ticker) Close() {
	close(t.stop)
}

func (t *Ticker) notifyOnTick() {
	t.m.Lock()
	defer t.m.Unlock()
	log.Info("release tick mesh.LayerID  %v", t.currentLayer)
	for _, ch := range t.subscribes {
		ch <- t.currentLayer
		log.Debug("iv'e notified number : %v", t.ids[ch])
	}
	log.Debug("Ive notified all")
	t.currentLayer++
}

func (t *Ticker) GetCurrentLayer() types.LayerID {
	t.m.RLock()
	defer t.m.RUnlock()
	return t.currentLayer
}

func (t *Ticker) Subscribe() LayerTimer {
	ch := make(LayerTimer)
	t.m.Lock()
	t.ids[ch] = len(t.ids)
	t.subscribes = append(t.subscribes, ch)
	t.m.Unlock()

	return ch
}

func (t *Ticker) updateLayerID() {
	tksa := t.time.Now().Sub(t.startEpoch)
	tks := (tksa / t.tickInterval).Nanoseconds() + 1
	//todo: need to unify all LayerIDs definitions and set them to uint64
	t.currentLayer = GenesisLayer + types.LayerID(tks)
}

func (t *Ticker) StartClock(diff time.Duration) {
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
