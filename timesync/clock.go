package timesync

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"sync"
	"time"
)

//this package sends a tick each tickInterval to all consumers of the tick
//This also send the current layerID which is calculated from the number of ticks passed since epoch
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

func (t *Ticker) Stop() {
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
	log.Info("starting global clock")
	if t.time.Now().Before(t.startEpoch) {
		log.Info("global clock sleeping till epoch")
		sleepTill := t.startEpoch.Sub(t.time.Now())
		tmr := time.NewTimer(sleepTill)
		select {
		case <-tmr.C:
			break
		case <-t.stop:
			return
		}
	}

	t.updateLayerID()
	diff := ((t.time.Now().Sub(t.startEpoch)) / t.tickInterval) + t.tickInterval
	time.Sleep(diff)
	tick := time.NewTimer(t.tickInterval)
	for {
		select {
		case <-tick.C:
			log.Info("released tick layerId %v", t.currentLayer+1)
			t.currentLayer++
			t.notifyOnTick()
			tick.Reset(t.tickInterval)
		case <-t.stop:
			break
		}
	}
}
