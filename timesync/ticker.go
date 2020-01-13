package timesync

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
	"time"
)

// subs implements a lock-protected Subscribe-Unsubscribe structure
// note: to access internal fields a lock must be obtained
type subs struct {
	subscribers map[LayerTimer]struct{} // map subscribers by channel
	m           sync.Mutex
}

func newSubs() *subs {
	return &subs{
		subscribers: make(map[LayerTimer]struct{}),
		m:           sync.Mutex{},
	}
}

func (s *subs) Subscribe() LayerTimer {
	ch := make(LayerTimer)
	s.m.Lock()
	s.subscribers[ch] = struct{}{}
	s.m.Unlock()
	log.Info("subscribed to channel")
	return ch
}

func (s *subs) Unsubscribe(ch LayerTimer) {
	s.m.Lock()
	delete(s.subscribers, ch)
	s.m.Unlock()
}

// LayerTimer is a channel of LayerIDs
// Subscribers will receive the ticked layer through such channel
type LayerTimer chan types.LayerID

// LayerConverter provides conversions from time to layer and vice versa
type LayerConverter interface {
	TimeToLayer(time.Time) types.LayerID
	LayerToTime(types.LayerID) time.Time
}

type Ticker struct {
	*subs                 // the sub-unsub provider
	clock           Clock // provides the time
	stop            chan struct{}
	started         bool
	once            sync.Once
	missedTicks     int            // counts the missed ticks since of the last tick
	lastTickedLayer types.LayerID  // track last ticked layer
	conv            LayerConverter // layer conversions provider
	log             log.Log
}

func NewTicker(c Clock, lc LayerConverter) *Ticker {
	return &Ticker{
		subs:        newSubs(),
		clock:       c,
		stop:        make(chan struct{}),
		started:     false,
		once:        sync.Once{},
		missedTicks: 0,
		conv:        lc,
		log:         log.NewDefault("ticker"),
	}
}

func (t *Ticker) Notify() {
	if !t.started {
		return
	}

	t.m.Lock()

	layer := t.conv.TimeToLayer(t.clock.Now())
	if layer <= t.lastTickedLayer {
		t.log.Warning("skipping tick to avoid double ticking the same layer (time was not monotonic)")
		return
	}
	t.missedTicks = 0
	t.log.Event().Info("release tick", log.LayerId(uint64(layer)))
	for ch := range t.subscribers { // notify all subscribers

		// non-blocking notify
		select {
		case ch <- layer:
			continue
		default:
			t.missedTicks++ // count subscriber that missed tick
			continue
		}
	}

	// update last ticked layer
	t.lastTickedLayer = layer

	if t.missedTicks > 0 {
		t.log.With().Error("missed ticks for layer", log.LayerId(uint64(layer)))
	}

	t.m.Unlock()
}

// TimeSinceLastTick returns the duration passed since the last layer that we ticked
func (t *Ticker) TimeSinceLastTick() time.Duration {
	t.m.Lock()
	d := t.clock.Now().Sub(t.conv.LayerToTime(t.lastTickedLayer))
	t.m.Unlock()
	return d
}

func (t *Ticker) StartNotifying() {
	t.log.Info("started notifying")
	t.started = true
}

func (t *Ticker) GetCurrentLayer() types.LayerID {
	return t.conv.TimeToLayer(t.clock.Now())
}

func (t *Ticker) Close() {
	t.once.Do(func() {
		close(t.stop)
	})
}
