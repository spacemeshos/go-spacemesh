package timesync

import (
	"errors"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// LayerTimer is a channel of LayerIDs
// Subscribers will receive the ticked layer through such channel.
type LayerTimer chan types.LayerID

// LayerConverter provides conversions from time to layer and vice versa.
type LayerConverter interface {
	TimeToLayer(time.Time) types.LayerID
	LayerToTime(types.LayerID) time.Time
}

// Ticker is the struct responsible for notifying that a layer has been ticked to subscribers.
type Ticker struct {
	LayerConverter // layer conversions provider

	mu              sync.Mutex
	clock           Clock // provides the time
	started         bool
	lastTickedLayer types.LayerID // track last ticked layer
	layerChannels   map[types.LayerID]chan struct{}
	log             log.Log
}

// TickerOption to configure Ticker.
type TickerOption func(*Ticker)

// WithLog configures logger for Ticker.
func WithLog(lg log.Log) TickerOption {
	return func(t *Ticker) {
		t.log = lg
	}
}

// NewTicker returns a new instance of ticker.
func NewTicker(c Clock, lc LayerConverter, opts ...TickerOption) *Ticker {
	t := &Ticker{
		lastTickedLayer: lc.TimeToLayer(c.Now()),
		clock:           c,
		LayerConverter:  lc,
		layerChannels:   make(map[types.LayerID]chan struct{}),
		log:             log.NewNop(),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

var (
	errNotStarted     = errors.New("ticker is not started")
	errNotMonotonic   = errors.New("tried to tick a previously ticked layer")
	errMissedTicks    = errors.New("missed ticks for one or more subscribers")
	errMissedTickTime = errors.New("missed tick time by more than the allowed threshold")
)

// the limit on how late a layer notification can be
// an attempt to notify later than sendTickThreshold from the expected tick time will result in a missed tick error.
const sendTickThreshold = 500 * time.Millisecond

// Notify notifies all the subscribers with the current layer.
// if the tick time has passed by more than sendTickThreshold, notify is skipped and errMissedTickTime is returned.
// if some subscribers were not listening, they are skipped and errMissedTicks/number of missed ticks is returned.
// notify may be skipped also for non-monotonic tick. (the clock can go backward when the system clock gets calibrated).
func (t *Ticker) Notify() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.started {
		return errNotStarted
	}

	layer := t.TimeToLayer(t.clock.Now())
	// close prev layers
	for l := t.lastTickedLayer; !l.After(layer); l = l.Add(1) {
		if layerChan, found := t.layerChannels[l]; found {
			select {
			case <-layerChan:
			default:
				close(layerChan)
			}
			delete(t.layerChannels, l)
		}
	}

	// already ticked
	if t.lastTickedLayer.After(types.LayerID{}) {
		// since layers start from 0, this check runs the risk of ticking layer 0 more than once.
		// the risk is worth it in favor of code simplicity. otherwise lastTickedLayer needs to start at -1.
		if !layer.After(t.lastTickedLayer) {
			t.log.With().Warning("skipping tick to avoid double ticking the same layer (time was not monotonic)",
				log.Stringer("current_layer", layer),
				log.Stringer("last_ticked_layer", t.lastTickedLayer),
			)
			return errNotMonotonic
		}
	}

	t.lastTickedLayer = layer // update last ticked layer

	return nil
}

// StartNotifying starts the clock notifying.
func (t *Ticker) StartNotifying() {
	t.log.Info("started notifying")
	t.mu.Lock()
	t.started = true
	t.mu.Unlock()
}

// GetCurrentLayer gets the current layer.
func (t *Ticker) GetCurrentLayer() types.LayerID {
	return t.TimeToLayer(t.clock.Now())
}

// AwaitLayer returns a channel that will be signaled when layer id layerID was ticked by the clock, or if this layer has passed
// while sleeping. it does so by closing the returned channel.
func (t *Ticker) AwaitLayer(layerID types.LayerID) chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()

	layerTime := t.LayerToTime(layerID)
	now := t.clock.Now()

	ch := t.layerChannels[layerID]
	if ch == nil {
		ch = make(chan struct{})
		t.layerChannels[layerID] = ch
	}
	if now.After(layerTime) || now.Equal(layerTime) { // passed the time of layerID
		close(ch)
	}
	return ch
}
