package timesync

import (
	"errors"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
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

// Subscribe returns a channel on which the subscriber will be notified when a new layer starts
func (s *subs) Subscribe() LayerTimer {
	ch := make(LayerTimer)
	s.m.Lock()
	s.subscribers[ch] = struct{}{}
	s.m.Unlock()
	log.Info("subscribed to channel")
	return ch
}

// Unsubscribe removed subscriber channel ch from notification list
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

// Ticker is the struct responsible for notifying that a layer has been ticked to subscribers
type Ticker struct {
	*subs                 // the sub-unsub provider
	LayerConverter        // layer conversions provider
	clock           Clock // provides the time
	started         bool
	lastTickedLayer types.LayerID // track last ticked layer
	layerChannels   map[types.LayerID]chan struct{}
	log             log.Log
}

// NewTicker returns a new instance of ticker
func NewTicker(c Clock, lc LayerConverter) *Ticker {
	return &Ticker{
		subs:            newSubs(),
		lastTickedLayer: lc.TimeToLayer(c.Now()),
		clock:           c,
		LayerConverter:  lc,
		layerChannels:   make(map[types.LayerID]chan struct{}),
		log:             log.NewDefault("ticker"),
	}
}

var (
	errNotStarted     = errors.New("ticker is not started")
	errNotMonotonic   = errors.New("tried to tick a previously ticked layer")
	errMissedTicks    = errors.New("missed ticks for one or more subscribers")
	errMissedTickTime = errors.New("missed tick time by more than the allowed threshold")
)

// the limit on how late a notify can be
// an attempt to notify later than sendTickThreshold from the expected tick time will resulted in a missed tick error
const sendTickThreshold = 500 * time.Millisecond

// Notify notifies all the subscribers with the current layer
// if the tick time has passed notify is skipped and errMissedTickTime is returned
// notify may be skipped also for non-monotonic tick
// if some of the subscribers where not listening, they are skipped. In that case, errMissedTicks is returned along the number of subscribers not listening
func (t *Ticker) Notify() (int, error) {
	t.m.Lock()
	if !t.started {
		t.m.Unlock()
		return 0, errNotStarted
	}

	layer := t.TimeToLayer(t.clock.Now())
	// close prev layers
	for l := t.lastTickedLayer + 1; l <= layer; l++ {
		if layerChan, found := t.layerChannels[l]; found {
			close(layerChan)
			delete(t.layerChannels, l)
		}
	}

	// the tick was delayed by more than the threshold
	if t.timeSinceLastTick() > sendTickThreshold {
		t.log.With().Warning("skipping tick since we missed the time of the tick by more than the allowed threshold", log.String("threshold", sendTickThreshold.String()))
		t.m.Unlock()
		return 0, errMissedTickTime
	}

	// already ticked
	if layer <= t.lastTickedLayer {
		t.log.With().Warning("skipping tick to avoid double ticking the same layer (time was not monotonic)",
			log.FieldNamed("current_layer", layer), log.FieldNamed("last_ticked_layer", t.lastTickedLayer))
		t.m.Unlock()
		return 0, errNotMonotonic
	}
	missedTicks := 0
	t.log.Event().Info("release tick", layer)
	for ch := range t.subscribers { // notify all subscribers

		// non-blocking notify
		select {
		case ch <- layer:
			continue
		default:
			missedTicks++ // count subscriber that missed tick
			continue
		}
	}

	t.lastTickedLayer = layer // update last ticked layer
	t.m.Unlock()

	if missedTicks > 0 {
		t.log.With().Error("missed ticks for layer", layer, log.Int("missed_count", missedTicks))
		return missedTicks, errMissedTicks
	}

	return 0, nil
}

// TimeSinceLastTick returns the duration passed since the last layer that we ticked
// note: the call is not lock-protected
func (t *Ticker) timeSinceLastTick() time.Duration {
	timeOfLastTick := t.LayerToTime(t.TimeToLayer(t.clock.Now()))
	return t.clock.Now().Sub(timeOfLastTick)
}

// StartNotifying starts the clock notifying
func (t *Ticker) StartNotifying() {
	t.log.Info("started notifying")
	t.m.Lock()
	t.started = true
	t.m.Unlock()
}

// GetCurrentLayer gets the current layer
func (t *Ticker) GetCurrentLayer() types.LayerID {
	return t.TimeToLayer(t.clock.Now())
}

var closedChan = make(chan struct{})

func init() {
	close(closedChan)
}

// AwaitLayer returns a channel that will be signald when layer id layerID was ticked by the clock, or if this layer has passed
// while sleeping. it does so by closing the returned channel
func (t *Ticker) AwaitLayer(layerID types.LayerID) chan struct{} {
	t.m.Lock()
	defer t.m.Unlock()

	layerTime := t.LayerToTime(layerID)
	now := t.clock.Now()
	if now.After(layerTime) || now.Equal(layerTime) { // passed the time of layerID
		return closedChan
	}

	ch := t.layerChannels[layerID]
	if ch == nil {
		ch = make(chan struct{})
		t.layerChannels[layerID] = ch
	}
	return ch
}
