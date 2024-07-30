package timesync

import (
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/metrics"
)

var tickDistance = metrics.NewHistogramWithBuckets(
	"tick_distance",
	"clock",
	"distance between layer ticks",
	[]string{},
	prometheus.ExponentialBuckets(1, 2, 10),
).WithLabelValues()

// NodeClock is the struct holding a real clock.
type NodeClock struct {
	LayerConverter // layer conversions provider

	clock        clockwork.Clock // provides the time
	genesis      time.Time
	tickInterval time.Duration

	mu            sync.Mutex    // protects the following fields
	lastTicked    types.LayerID // track last ticked layer
	minLayer      types.LayerID // track earliest layer that has a channel waiting for tick
	layerChannels map[types.LayerID]chan struct{}

	stop chan struct{}
	once sync.Once

	log *zap.Logger
	eg  errgroup.Group
}

// NewClock return TimeClock struct that notifies tickInterval has passed.
func NewClock(opts ...OptionFunc) (*NodeClock, error) {
	cfg := &option{
		clock: clockwork.NewRealClock(),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	gtime := cfg.genesisTime.Local()
	cfg.log.Info("converting genesis time to local time",
		zap.Time("genesis", cfg.genesisTime),
		zap.Time("local", gtime),
	)
	t := &NodeClock{
		LayerConverter: LayerConverter{duration: cfg.layerDuration, genesis: gtime},
		clock:          cfg.clock,
		tickInterval:   cfg.tickInterval,
		layerChannels:  make(map[types.LayerID]chan struct{}),
		genesis:        gtime,
		stop:           make(chan struct{}),
		log:            cfg.log,
	}

	t.eg.Go(t.startClock)
	return t, nil
}

func (t *NodeClock) startClock() error {
	t.log.Info("starting global clock",
		zap.Time("now", t.clock.Now()),
		zap.Time("genesis", t.genesis),
		zap.Duration("layer_duration", t.duration),
		zap.Duration("tick_interval", t.tickInterval),
	)

	ticker := t.clock.NewTicker(t.tickInterval)
	for {
		currLayer := t.TimeToLayer(t.clock.Now())
		t.log.Debug("global clock going to sleep before next tick",
			zap.Stringer("curr_layer", currLayer),
		)

		select {
		case <-ticker.Chan():
		case <-t.stop:
			t.log.Debug("stopping global clock")
			ticker.Stop()
			return nil
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
		t.log.Debug("stopping clock")
		close(t.stop)
		if err := t.eg.Wait(); err != nil {
			t.log.Error("failed to stop clock", zap.Error(err))
		}
		t.log.Debug("clock stopped")
	})
}

// tick processes the current tick. It iterates over all layers that have passed since the last tick and notifies
// listeners that are awaiting these layers.
func (t *NodeClock) tick() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.clock.Now().Before(t.genesis) {
		return
	}

	layer := t.TimeToLayer(t.clock.Now())
	switch {
	case layer.Before(t.lastTicked):
		t.log.Warn("clock ticked back in time",
			zap.Stringer("layer", layer),
			zap.Stringer("last_ticked_layer", t.lastTicked),
		)
		d := t.lastTicked.Difference(layer)
		tickDistance.Observe(float64(-d))
	// don't warn right after fresh startup
	case layer.Difference(t.lastTicked) > 1 && t.lastTicked > 0:
		t.log.Warn("clock skipped layers",
			zap.Stringer("layer", layer),
			zap.Stringer("last_ticked_layer", t.lastTicked),
		)
		d := layer.Difference(t.lastTicked)
		tickDistance.Observe(float64(d))
	case layer == t.lastTicked:
		tickDistance.Observe(0)
	}

	// close await channel for prev layers
	for l := t.minLayer; !l.After(layer); l = l.Add(1) {
		if layerChan, found := t.layerChannels[l]; found {
			close(layerChan)
			delete(t.layerChannels, l)
		}
	}

	t.lastTicked = layer
	t.minLayer = layer.Add(1)
}

// CurrentLayer gets the current layer.
func (t *NodeClock) CurrentLayer() types.LayerID {
	return t.TimeToLayer(t.clock.Now())
}

// AwaitLayer returns a channel that will be signaled when layer id layerID was ticked by the clock,
// or if this layer has passed while sleeping. it does so by closing the returned channel.
func (t *NodeClock) AwaitLayer(layerID types.LayerID) <-chan struct{} {
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

	if t.minLayer.After(layerID) {
		t.minLayer = layerID
	}

	return ch
}
