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

// Config holds the arguments and dependencies to create a verifying tortoise instance.
type Config struct {
	LayerSize                uint32
	Database                 database.Database
	MeshDatabase             blockDataProvider
	Beacons                  blocks.BeaconGetter
	ATXDB                    atxDataProvider
	Hdist                    uint32   // hare lookback distance: the distance over which we use the input vector/hare results
	Zdist                    uint32   // hare result wait distance: the distance over which we're willing to wait for hare results
	ConfidenceParam          uint32   // confidence wait distance: how long we wait for global consensus to be established
	GlobalThreshold          *big.Rat // threshold required to finalize blocks and layers
	LocalThreshold           *big.Rat // threshold that determines whether a node votes based on local or global opinion
	WindowSize               uint32   // tortoise sliding window: how many layers we store data for
	Log                      log.Log
	RerunInterval            time.Duration // how often to rerun from genesis
	BadBeaconVoteDelayLayers uint32        // number of layers to delay votes for blocks with bad beacon values during self-healing
}

// ThreadSafeVerifyingTortoise is a thread safe verifying tortoise wrapper, it just locks all actions.
type ThreadSafeVerifyingTortoise struct {
	logger log.Log

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

// NewVerifyingTortoise creates ThreadSafeVerifyingTortoise instance.
func NewVerifyingTortoise(ctx context.Context, cfg Config) *ThreadSafeVerifyingTortoise {
	if cfg.Hdist < cfg.Zdist {
		cfg.Log.With().Panic("hdist must be >= zdist", log.Uint32("hdist", cfg.Hdist), log.Uint32("zdist", cfg.Zdist))
	}
	alg := &ThreadSafeVerifyingTortoise{
		trtl: newTurtle(
			cfg.Log,
			cfg.Database,
			cfg.MeshDatabase,
			cfg.ATXDB,
			cfg.Beacons,
			cfg.Hdist,
			cfg.Zdist,
			cfg.ConfidenceParam,
			cfg.WindowSize,
			cfg.LayerSize,
			cfg.GlobalThreshold,
			cfg.LocalThreshold,
			cfg.BadBeaconVoteDelayLayers,
		),
		logger: cfg.Log,
	}
	if err := alg.trtl.Recover(); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			alg.trtl.init(ctx, mesh.GenesisLayer())
		} else {
			cfg.Log.With().Panic("can't recover turtle state", log.Err(err))
		}
	}
	alg.org = organizer.New(
		organizer.WithLogger(cfg.Log),
		organizer.WithLastLayer(alg.trtl.Last),
	)
	ctx, cancel := context.WithCancel(ctx)
	alg.cancel = cancel
	// TODO(dshulyak) with low rerun interval it is possible to start a rerun
	// when initial sync is in progress, or right after sync
	if cfg.RerunInterval != 0 {
		alg.eg.Go(func() error {
			alg.rerunLoop(ctx, cfg.RerunInterval)
			return nil
		})
	}
	return alg
}

// LatestComplete returns the latest verified layer.
func (trtl *ThreadSafeVerifyingTortoise) LatestComplete() types.LayerID {
	trtl.mu.RLock()
	verified := trtl.trtl.Verified
	trtl.mu.RUnlock()
	return verified
}

// BaseBlock chooses a base block and creates a differences list. needs the hare results for latest layers.
func (trtl *ThreadSafeVerifyingTortoise) BaseBlock(ctx context.Context) (types.BlockID, [][]types.BlockID, error) {
	trtl.mu.RLock()
	defer trtl.mu.RUnlock()
	block, diffs, err := trtl.trtl.BaseBlock(ctx)
	if err != nil {
		return types.BlockID{}, nil, err
	}
	return block, diffs, err
}

// HandleLateBlocks processes votes and goodness for late blocks (for late block definition see white paper).
// Returns the old verified layer and new verified layer after taking into account the blocks' votes.
// DEPRECATED: don't use this method it will be completely removed.
func (trtl *ThreadSafeVerifyingTortoise) HandleLateBlocks(ctx context.Context, blocks []*types.Block) (types.LayerID, types.LayerID) {
	trtl.mu.Lock()
	defer trtl.mu.Unlock()
	oldVerified := trtl.trtl.Verified
	if err := trtl.trtl.ProcessNewBlocks(ctx, blocks); err != nil {
		// consider panicking here instead, since it means tortoise is stuck
		trtl.logger.WithContext(ctx).With().Error("tortoise errored handling late blocks", log.Err(err))
	}
	newVerified := trtl.trtl.Verified
	return oldVerified, newVerified
}

// HandleIncomingLayer processes all layer block votes
// returns the old verified layer and new verified layer after taking into account the blocks votes.
func (trtl *ThreadSafeVerifyingTortoise) HandleIncomingLayer(ctx context.Context, layerID types.LayerID) (types.LayerID, types.LayerID, bool) {
	trtl.mu.Lock()
	defer trtl.mu.Unlock()

	var (
		oldVerified = trtl.trtl.Verified
		logger      = trtl.logger.WithContext(ctx).With()
	)
	reverted, observed := trtl.updateFromRerun(ctx)
	if reverted {
		// make sure state is reapplied from far enough back if there was a state reversion.
		// this is the first changed layer. subtract one to indicate that the layer _prior_ was the old
		// pBase, since we never reapply the state of oldPbase.
		oldVerified = observed.Sub(1)
	}

	trtl.org.Iterate(ctx, layerID, func(lid types.LayerID) {
		logger.Info("handling incoming layer",
			log.FieldNamed("old_pbase", oldVerified),
			log.FieldNamed("incoming_layer", layerID))
		if err := trtl.trtl.HandleIncomingLayer(ctx, layerID); err != nil {
			logger.Error("tortoise errored handling incoming layer", log.Err(err))
		}
	})

	newVerified := trtl.trtl.Verified
	logger.Info("finished handling incoming layer",
		log.FieldNamed("old_pbase", oldVerified),
		log.FieldNamed("new_pbase", newVerified),
		log.FieldNamed("incoming_layer", layerID))
	return oldVerified, newVerified, reverted
}

// Persist saves a copy of the current tortoise state to the database.
func (trtl *ThreadSafeVerifyingTortoise) Persist(ctx context.Context) error {
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
func (trtl *ThreadSafeVerifyingTortoise) Stop() {
	trtl.cancel()
	trtl.eg.Wait()
}
