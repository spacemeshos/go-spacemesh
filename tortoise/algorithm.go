package tortoise

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/tortoise/organizer"
)

// Config for protocol parameters.
type Config struct {
	LayerSize                uint32
	Hdist                    uint32        // hare lookback distance: the distance over which we use the input vector/hare results
	Zdist                    uint32        // hare result wait distance: the distance over which we're willing to wait for hare results
	ConfidenceParam          uint32        // confidence wait distance: how long we wait for global consensus to be established
	GlobalThreshold          *big.Rat      // threshold required to finalize blocks and layers
	LocalThreshold           *big.Rat      // threshold that determines whether a node votes based on local or global opinion
	WindowSize               uint32        // tortoise sliding window: how many layers we store data for
	RerunInterval            time.Duration // how often to rerun from genesis
	BadBeaconVoteDelayLayers uint32        // number of layers to delay votes for blocks with bad beacon values during self-healing
	MaxExceptions            int           // if candidate for base block has more than max exceptions it will be ignored
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
	// persistMu is needed to allow concurrent BaseBlock and Persist call
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
func New(db database.Database, mdb blockDataProvider, atxdb atxDataProvider, beacons blocks.BeaconGetter, opts ...Opt) *Tortoise {
	t := &Tortoise{
		ctx:    context.Background(),
		logger: log.NewNop(),
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
		db,
		mdb,
		atxdb,
		beacons,
		t.cfg,
	)

	if err := t.trtl.Recover(); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			t.trtl.init(t.ctx, mesh.GenesisLayer())
		} else {
			t.logger.With().Panic("can't recover turtle state", log.Err(err))
		}
	}
	t.org = organizer.New(
		organizer.WithLogger(t.logger),
		organizer.WithLastLayer(t.trtl.Last),
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
	return trtl.trtl.Verified
}

// BaseBlock chooses a base block and creates a differences list. needs the hare results for latest layers.
func (trtl *Tortoise) BaseBlock(ctx context.Context) (types.BlockID, [][]types.BlockID, error) {
	trtl.mu.RLock()
	defer trtl.mu.RUnlock()
	return trtl.trtl.BaseBlock(ctx)
}

// HandleIncomingLayer processes all layer block votes
// returns the old verified layer and new verified layer after taking into account the blocks votes.
func (trtl *Tortoise) HandleIncomingLayer(ctx context.Context, layerID types.LayerID) (types.LayerID, types.LayerID, bool) {
	trtl.mu.Lock()
	defer trtl.mu.Unlock()

	var (
		old    = trtl.trtl.Verified
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
			log.FieldNamed("new_pbase", trtl.trtl.Verified),
			log.FieldNamed("incoming_layer", lid))
	})

	return old, trtl.trtl.Verified, reverted
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
