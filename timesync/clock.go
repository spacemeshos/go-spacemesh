package timesync

import (
	"github.com/spacemeshos/go-spacemesh/mesh"
	"sync"
	"time"
)
//this package sends a tick each tickInterval to all consumers of the tick
//This also send the current layerID which is calculated from the number of ticks passed since epoch
type LayerTimer chan mesh.LayerID

type Ticker struct {
	subscribes []LayerTimer
	currentLayer mesh.LayerID
	m sync.Mutex
	tickInterval time.Duration
	startEpoch time.Time
	time Clock
	stop chan struct{}
}

type Clock interface {
	Now() time.Time
}

func NewTicker(time Clock, tickInterval time.Duration, startEpoch time.Time) *Ticker {
	return &Ticker{
		subscribes: make([]LayerTimer,0,0),
		currentLayer: 0,
		tickInterval:tickInterval,
		startEpoch:startEpoch,
		time : time,
		stop : make(chan struct{}),
	}
}

func (t *Ticker) Start(){
	go t.StartClock()
}

func (t *Ticker) Stop(){
	close(t.stop)
}

func (t *Ticker) notifyOnTick(){
	t.m.Lock()
	for _, ch := range t.subscribes {
		ch <- t.currentLayer
	}
	t.m.Unlock()
}

func (t *Ticker) Subscribe() LayerTimer {
	ch := make(LayerTimer, 2)
	t.m.Lock()
	t.subscribes = append(t.subscribes, ch)
	t.m.Unlock()

	return ch
}

func (t *Ticker) updateLayerID(){
	tksa := t.time.Now().Sub(t.startEpoch)
	tks := (tksa / t.tickInterval).Nanoseconds()
	//todo: need to unify all LayerIDs definitions and set them to uint64
	t.currentLayer = mesh.LayerID(tks)
}

func (t *Ticker) StartClock(){
	if t.time.Now().Before(t.startEpoch) {
		sleepTill := t.startEpoch.Sub(t.time.Now())
		tmr := time.NewTimer(sleepTill)
		select {
			case <- tmr.C:
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
				t.currentLayer++
				t.notifyOnTick()
				tick.Reset(t.tickInterval)
			case <-t.stop:
				break
		}
	}
}
