package timesync

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"sync"
	"time"
)

//this package sends a tick each tickInterval to all consumers of the tick
//This also send the current mesh.LayerID  which is calculated from the number of ticks passed since epoch
type LayerTimer chan mesh.LayerID

type Ticker struct {
	subscribes   []LayerTimer
	currentLayer mesh.LayerID
	m            sync.Mutex
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
	return &Ticker{
		subscribes:   make([]LayerTimer, 0, 0),
		currentLayer: 0,
		tickInterval: tickInterval,
		startEpoch:   startEpoch,
		time:         time,
		stop:         make(chan struct{}),
		ids:          make(map[LayerTimer]int),
	}
}

func (t *Ticker) Start() {
	go t.StartClock()
}

func (t *Ticker) Close() {
	close(t.stop)
}

func (t *Ticker) notifyOnTick() {
	t.m.Lock()
	defer t.m.Unlock()
	for _, ch := range t.subscribes {

		ch <- t.currentLayer
		log.Debug("iv'e notified number : %v", t.ids[ch])
	}
	log.Debug("Ive notified all")

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
	tks := (tksa / t.tickInterval).Nanoseconds()
	//todo: need to unify all LayerIDs definitions and set them to uint64
	t.currentLayer = mesh.LayerID(tks)
}

func (t *Ticker) StartClock() {
	log.Info("starting global clock now=%v genesis=%v", t.time.Now(), t.startEpoch)

	var diff time.Duration
	if t.time.Now().Before(t.startEpoch) {
		diff = t.startEpoch.Sub(t.time.Now())
	} else {
		t.updateLayerID()
		diff = ((t.time.Now().Sub(t.startEpoch)) / t.tickInterval) + t.tickInterval
	}

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
			log.Info("released tick mesh.LayerID  %v", t.currentLayer+1)
			t.currentLayer++
			t.notifyOnTick()
		case <-t.stop:
			return
		}
	}
}
