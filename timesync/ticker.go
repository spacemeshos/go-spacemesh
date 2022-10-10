package timesync

import (
	"context"
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
	mu          sync.Mutex
}

func newSubs() *subs {
	return &subs{
		subscribers: make(map[LayerTimer]struct{}),
	}
}

// Subscribe returns a channel on which the subscriber will be notified when a new layer starts.
func (s *subs) Subscribe() LayerTimer {
	ch := make(LayerTimer)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscribers[ch] = struct{}{}
	return ch
}

// Unsubscribe removed subscriber channel ch from notification list.
func (s *subs) Unsubscribe(ch LayerTimer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subscribers, ch)
}

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
	*subs                     // the sub-unsub provider
	LayerConverter            // layer conversions provider
	clock               Clock // provides the time
	started             bool
	lastTickedLayer     types.LayerID // track last ticked layer
	layersSubscriptions map[types.LayerID][]context.CancelFunc
	log                 log.Log
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
		subs:                newSubs(),
		lastTickedLayer:     lc.TimeToLayer(c.Now()),
		clock:               c,
		LayerConverter:      lc,
		layersSubscriptions: make(map[types.LayerID][]context.CancelFunc),
		log:                 log.NewNop(),
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
func (t *Ticker) Notify() (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.started {
		return 0, errNotStarted
	}

	layer := t.TimeToLayer(t.clock.Now())
	// close prev layers
	for l := t.lastTickedLayer; !l.After(layer); l = l.Add(1) {
		if cancels, found := t.layersSubscriptions[l]; found {
			for _, cancel := range cancels {
				cancel()
			}
			delete(t.layersSubscriptions, l)
		}
	}

	// the tick was delayed by more than the threshold
	if t.timeSinceCurrentStart() > sendTickThreshold {
		t.log.With().Error("skipping tick since we missed the time of the tick by more than the allowed threshold",
			log.FieldNamed("current_layer", layer),
			log.String("threshold", sendTickThreshold.String()))
		return 0, errMissedTickTime
	}

	// already ticked
	if t.lastTickedLayer.After(types.LayerID{}) {
		// since layers start from 0, this check runs the risk of ticking layer 0 more than once.
		// the risk is worth it in favor of code simplicity. otherwise lastTickedLayer needs to start at -1.
		if !layer.After(t.lastTickedLayer) {
			t.log.With().Warning("skipping tick to avoid double ticking the same layer (time was not monotonic)",
				log.FieldNamed("current_layer", layer),
				log.FieldNamed("last_ticked_layer", t.lastTickedLayer))
			return 0, errNotMonotonic
		}
	}
	missedTicks := 0
	t.log.Event().Info("release tick", layer)
	for ch := range t.subscribers { // notify all subscribers
		// non-blocking notify
		select {
		case ch <- layer:
		default:
			missedTicks++ // count subscriber that missed tick
		}
	}

	t.lastTickedLayer = layer // update last ticked layer

	if missedTicks > 0 {
		return missedTicks, errMissedTicks
	}

	return 0, nil
}

func (t *Ticker) timeSinceCurrentStart() time.Duration {
	layerStart := t.LayerToTime(t.TimeToLayer(t.clock.Now()))
	return t.clock.Now().Sub(layerStart)
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

var closedChan = make(chan struct{})

func init() {
	close(closedChan)
}

// AwaitLayer returns a channel that will be signaled when layer id layerID was ticked by the clock, or if this layer has passed
// while sleeping. it does so by closing the returned channel.
func (t *Ticker) AwaitLayer(ctx context.Context, layerID types.LayerID) context.Context {
	t.mu.Lock()
	defer t.mu.Unlock()

	layerCtx, cancel := context.WithCancel(ctx)

	layerTime := t.LayerToTime(layerID)
	now := t.clock.Now()
	if now.After(layerTime) || now.Equal(layerTime) { // passed the time of layerID
		cancel()
		return layerCtx
	}

	cancels := t.layersSubscriptions[layerID]
	if cancels == nil {
		cancels = make([]context.CancelFunc, 0, 1)
	}

	cancels = append(cancels, cancel)
	t.layersSubscriptions[layerID] = cancels

	return layerCtx
}
