package tortoise

import (
	"context"
	"math/big"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/tortoise/organizer"
)

// Config for protocol parameters.
type Config struct {
	Hdist           uint32        `mapstructure:"tortoise-hdist"`            // hare/input vector lookback distance
	Zdist           uint32        `mapstructure:"tortoise-zdist"`            // hare result wait distance
	ConfidenceParam uint32        `mapstructure:"tortoise-confidence-param"` // layers to wait for global consensus
	WindowSize      uint32        `mapstructure:"tortoise-window-size"`      // size of the tortoise sliding window (in layers)
	GlobalThreshold *big.Rat      `mapstructure:"tortoise-global-threshold"` // threshold for finalizing blocks and layers
	LocalThreshold  *big.Rat      `mapstructure:"tortoise-local-threshold"`  // threshold for choosing when to use weak coin
	RerunInterval   time.Duration `mapstructure:"tortoise-rerun-interval"`
	MaxExceptions   int           `mapstructure:"tortoise-max-exceptions"` // if candidate for base block has more than max exceptions it will be ignored

	LayerSize                uint32
	BadBeaconVoteDelayLayers uint32 // number of layers to delay votes for blocks with bad beacon values during self-healing
}

// DefaultConfig for Tortoise.
func DefaultConfig() Config {
	return Config{
		LayerSize:                30,
		Hdist:                    10,
		Zdist:                    8,
		ConfidenceParam:          2,
		WindowSize:               100,
		GlobalThreshold:          big.NewRat(60, 100),
		LocalThreshold:           big.NewRat(20, 100),
		RerunInterval:            24 * time.Hour,
		BadBeaconVoteDelayLayers: 6,
		MaxExceptions:            30 * 100, // 100 layers of average size
	}
}

// Tortoise is a thread safe verifying tortoise wrapper, it just locks all actions.
type Tortoise struct {
	logger log.Log
	ctx    context.Context
	cfg    Config

	eg     errgroup.Group
	cancel context.CancelFunc

	mu  sync.RWMutex
	org *organizer.Organizer

	// update will be set to non-nil after rerun completes, and must be set to nil once
	// used to replace trtl.
	update *rerunResult
	// persistMu is needed to allow concurrent BaseBallot and Persist call
	// but to prevent multiple concurrent Persist calls
	persistMu sync.Mutex
	trtl      *turtle
}

// Opt for configuring tortoise.
type Opt func(t *Tortoise)

// WithLogger defines logger for tortoise.
func WithLogger(logger log.Log) Opt {
	return func(t *Tortoise) {
		t.logger = logger
	}
}

// WithContext defines context for tortoise.
func WithContext(ctx context.Context) Opt {
	return func(t *Tortoise) {
		t.ctx = ctx
	}
}

// WithConfig defines protocol parameters.
func WithConfig(cfg Config) Opt {
	return func(t *Tortoise) {
		t.cfg = cfg
	}
}

// New creates Tortoise instance.
func New(mdb blockDataProvider, atxdb atxDataProvider, beacons system.BeaconGetter, opts ...Opt) *Tortoise {
	t := &Tortoise{
		ctx:    context.Background(),
		logger: log.NewNop(),
		cfg:    DefaultConfig(),
	}
	for _, opt := range opts {
		opt(t)
	}

	if t.cfg.Hdist < t.cfg.Zdist {
		t.logger.With().Panic("hdist must be >= zdist",
			log.Uint32("hdist", t.cfg.Hdist),
			log.Uint32("zdist", t.cfg.Zdist),
		)
	}
	t.trtl = newTurtle(
		t.logger,
		mdb,
		atxdb,
		beacons,
		t.cfg,
	)
	t.trtl.init(t.ctx, mesh.GenesisLayer())

	t.org = organizer.New(
		organizer.WithLogger(t.logger),
		organizer.WithLastLayer(t.trtl.last),
	)
	ctx, cancel := context.WithCancel(t.ctx)
	t.cancel = cancel
	// TODO(dshulyak) with low rerun interval it is possible to start a rerun
	// when initial sync is in progress, or right after sync
	if t.cfg.RerunInterval != 0 {
		t.eg.Go(func() error {
			t.rerunLoop(ctx, t.cfg.RerunInterval)
			return nil
		})
	}
	return t
}

// LatestComplete returns the latest verified layer.
func (trtl *Tortoise) LatestComplete() types.LayerID {
	trtl.mu.RLock()
	defer trtl.mu.RUnlock()
	return trtl.trtl.verified
}

// BaseBallot chooses a base ballot and creates a differences list. needs the hare results for latest layers.
func (trtl *Tortoise) BaseBallot(ctx context.Context) (types.BallotID, [][]types.BlockID, error) {
	trtl.mu.RLock()
	defer trtl.mu.RUnlock()
	return trtl.trtl.BaseBallot(ctx)
}

// HandleIncomingLayer processes all layer block votes
// returns the old verified layer and new verified layer after taking into account the blocks votes.
func (trtl *Tortoise) HandleIncomingLayer(ctx context.Context, layerID types.LayerID) (types.LayerID, types.LayerID, bool) {
	trtl.mu.Lock()
	defer trtl.mu.Unlock()

	var (
		old    = trtl.trtl.verified
		logger = trtl.logger.WithContext(ctx).With()
	)

	reverted, observed := trtl.updateFromRerun(ctx)
	if reverted {
		// make sure state is reapplied from far enough back if there was a state reversion.
		// this is the first changed layer. subtract one to indicate that the layer _prior_ was the old
		// pBase, since we never reapply the state of oldPbase.
		old = observed.Sub(1)
	}
	trtl.org.Iterate(ctx, layerID, func(lid types.LayerID) {
		logger.Info("handling incoming layer",
			log.FieldNamed("old_pbase", old),
			log.FieldNamed("incoming_layer", lid))
		if err := trtl.trtl.HandleIncomingLayer(ctx, lid); err != nil {
			logger.Error("tortoise errored handling incoming layer", log.Err(err))
		}
		logger.Info("finished handling incoming layer",
			log.FieldNamed("old_pbase", old),
			log.FieldNamed("new_pbase", trtl.trtl.verified),
			log.FieldNamed("incoming_layer", lid))
	})

	return old, trtl.trtl.verified, reverted
}

// Persist saves a copy of the current tortoise state to the database.
func (trtl *Tortoise) Persist(ctx context.Context) error {
	trtl.mu.RLock()
	defer trtl.mu.RUnlock()
	trtl.persistMu.Lock()
	defer trtl.persistMu.Unlock()
	start := time.Now()

	err := trtl.trtl.persist()
	trtl.logger.WithContext(ctx).With().Info("persist tortoise",
		log.Duration("duration", time.Since(start)))
	return err
}

// Stop background workers.
func (trtl *Tortoise) Stop() {
	trtl.cancel()
	trtl.eg.Wait()
}
