package timesync

import (
	"sync"
	"time"

	"github.com/benbjohnson/clock"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// NodeClock is the struct holding a real clock.
type NodeClock struct {
	LayerConverter // layer conversions provider

	clock   clock.Clock // provides the time
	genesis time.Time

	mu              sync.Mutex    // protects the following fields
	lastTickedLayer types.LayerID // track last ticked layer
	layerChannels   map[types.LayerID]chan struct{}

	stop chan struct{}
	once sync.Once

	log log.Log
	eg  errgroup.Group
}

// NewClock return TimeClock struct that notifies tickInterval has passed.
func NewClock(opts ...OptionFunc) (*NodeClock, error) {
	cfg := &option{
		clock: clock.New(),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	gtime := cfg.genesisTime.Local()
	cfg.log.With().Info("converting genesis time to local time",
		log.Time("genesis", cfg.genesisTime),
		log.Time("local", gtime),
	)
	t := &NodeClock{
		LayerConverter: LayerConverter{duration: cfg.layerDuration, genesis: gtime},
		clock:          cfg.clock,
		layerChannels:  make(map[types.LayerID]chan struct{}),
		genesis:        gtime,
		stop:           make(chan struct{}),
		log:            *cfg.log,
	}

	t.eg.Go(t.startClock)
	return t, nil
}

func (t *NodeClock) startClock() error {
	t.log.Info("starting global clock now=%v genesis=%v %p", t.clock.Now(), t.genesis, t)

	for {
		currLayer := t.TimeToLayer(t.clock.Now())
		nextLayer := currLayer.Add(1)
		if time.Until(t.LayerToTime(currLayer)) > 0 {
			nextLayer = currLayer
		}
		nextTickTime := t.LayerToTime(nextLayer)
		t.log.With().Info("global clock going to sleep before next layer",
			log.Stringer("curr_layer", currLayer),
			log.Stringer("next_layer", nextLayer),
			log.Time("next_tick_time", nextTickTime),
		)

		for {
			// `time.After` sometimes unblocks bit too soon. In this case - wait again.
			// See https://github.com/spacemeshos/go-spacemesh/issues/3617.
			if t.clock.Now().After(nextTickTime) || t.clock.Now().Equal(nextTickTime) {
				break
			}
			select {
			case <-t.clock.After(nextTickTime.Sub(t.clock.Now())):
			case <-t.stop:
				t.log.Info("stopping global clock %p", t)
				return nil
			}
		}

		t.tick()
	}
}

// GenesisTime returns at which time this clock has started (used to calculate current tick).
func (t *NodeClock) GenesisTime() time.Time {
	return t.genesis
}

// Close closes the clock ticker.
func (t *NodeClock) Close() {
	t.once.Do(func() {
		t.log.Info("stopping clock")
		close(t.stop)
		if err := t.eg.Wait(); err != nil {
			t.log.Error("errgroup: %v", err)
		}
		t.log.Info("clock stopped")
	})
}

// tick processes the current tick. It iterates over all layers that have passed since the last tick and notifies
// listeners that are awaiting these layers.
func (t *NodeClock) tick() {
	t.mu.Lock()
	defer t.mu.Unlock()

	layer := t.TimeToLayer(t.clock.Now())

	// close await channel for prev layers
	for l := t.lastTickedLayer; !l.After(layer); l = l.Add(1) {
		if layerChan, found := t.layerChannels[l]; found {
			close(layerChan)
			delete(t.layerChannels, l)
		}
	}

	t.lastTickedLayer = layer // update last ticked layer
}

// CurrentLayer gets the current layer.
func (t *NodeClock) CurrentLayer() types.LayerID {
	return t.TimeToLayer(t.clock.Now())
}

// AwaitLayer returns a channel that will be signaled when layer id layerID was ticked by the clock, or if this layer has passed
// while sleeping. it does so by closing the returned channel.
func (t *NodeClock) AwaitLayer(layerID types.LayerID) chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()

	layerTime := t.LayerToTime(layerID)
	now := t.clock.Now()
	if now.After(layerTime) || now.Equal(layerTime) { // passed the time of layerID
		ch := make(chan struct{})
		close(ch)
		return ch
	}

	ch := t.layerChannels[layerID]
	if ch == nil {
		ch = make(chan struct{})
		t.layerChannels[layerID] = ch
	}
	return ch
}
