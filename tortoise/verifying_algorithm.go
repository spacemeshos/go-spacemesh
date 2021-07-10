package tortoise

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

// ThreadSafeVerifyingTortoise is a thread safe verifying tortoise wrapper, it just locks all actions.
type ThreadSafeVerifyingTortoise struct {
	trtl      *turtle
	logger    log.Log
	lastRerun time.Time
	mutex     sync.RWMutex
}

// Config holds the arguments and dependencies to create a verifying tortoise instance.
type Config struct {
	LayerSize       int
	Database        blockDataProvider
	ATXDB           atxDataProvider
	Clock           layerClock
	Hdist           int   // hare lookback distance: the distance over which we use the input vector/hare results
	Zdist           int   // hare result wait distance: the distance over which we're willing to wait for hare results
	ConfidenceParam int   // confidence wait distance: how long we wait for global consensus to be established
	GlobalThreshold uint8 // threshold required to finalize blocks and layers (0-100)
	LocalThreshold  uint8 // threshold that determines whether a node votes based on local or global opinion (0-100)
	WindowSize      int   // tortoise sliding window: how many layers we store data for
	Log             log.Log
	Recovered       bool
	RerunInterval   time.Duration // how often to rerun from genesis
}

// NewVerifyingTortoise creates a new verifying tortoise wrapper
func NewVerifyingTortoise(ctx context.Context, cfg Config) *ThreadSafeVerifyingTortoise {
	if cfg.Recovered {
		return recoveredVerifyingTortoise(cfg.Database, cfg.Log)
	}
	return verifyingTortoise(
		ctx,
		cfg.LayerSize,
		cfg.Database,
		cfg.ATXDB,
		cfg.Clock,
		cfg.Hdist,
		cfg.Zdist,
		cfg.ConfidenceParam,
		cfg.WindowSize,
		cfg.GlobalThreshold,
		cfg.LocalThreshold,
		cfg.RerunInterval,
		cfg.Log)
}

// verifyingTortoise creates a new verifying tortoise wrapper
func verifyingTortoise(
	ctx context.Context,
	layerSize int,
	mdb blockDataProvider,
	atxdb atxDataProvider,
	clock layerClock,
	hdist,
	zdist,
	confidenceParam,
	windowSize int,
	globalThreshold,
	localThreshold uint8,
	rerunInterval time.Duration,
	logger log.Log,
) *ThreadSafeVerifyingTortoise {
	if hdist < zdist {
		logger.With().Panic("hdist must be >= zdist", log.Int("hdist", hdist), log.Int("zdist", zdist))
	}
	if globalThreshold > 100 || localThreshold > 100 {
		logger.With().Panic("global and local threshold values must be in the interval [0, 100]")
	}
	alg := &ThreadSafeVerifyingTortoise{
		trtl: newTurtle(mdb, atxdb, clock, hdist, zdist, confidenceParam, windowSize, layerSize, globalThreshold, localThreshold, rerunInterval),
	}
	alg.logger = logger
	alg.lastRerun = time.Now()
	alg.trtl.SetLogger(logger.WithFields(log.String("tortoise_rerun", "false")))
	alg.trtl.init(ctx, mesh.GenesisLayer())
	return alg
}

// NewRecoveredVerifyingTortoise recovers a previously persisted tortoise copy from mesh.DB
func recoveredVerifyingTortoise(mdb blockDataProvider, logger log.Log) *ThreadSafeVerifyingTortoise {
	tmp, err := RecoverVerifyingTortoise(mdb)
	if err != nil {
		logger.With().Panic("could not recover tortoise state from disk", log.Err(err))
	}

	trtl, ok := tmp.(*turtle)
	if !ok {
		logger.Panic("type error reading recovered tortoise state")
	}

	logger.Info("recovered tortoise from disk")
	trtl.bdp = mdb
	trtl.logger = logger

	return &ThreadSafeVerifyingTortoise{trtl: trtl, lastRerun: time.Now(), logger: logger}
}

// LatestComplete returns the latest verified layer
func (trtl *ThreadSafeVerifyingTortoise) LatestComplete() types.LayerID {
	trtl.mutex.RLock()
	verified := trtl.trtl.Verified
	trtl.mutex.RUnlock()
	return verified
}

// BaseBlock chooses a base block and creates a differences list. needs the hare results for latest layers.
func (trtl *ThreadSafeVerifyingTortoise) BaseBlock(ctx context.Context) (types.BlockID, [][]types.BlockID, error) {
	trtl.mutex.Lock()
	block, diffs, err := trtl.trtl.BaseBlock(ctx)
	trtl.mutex.Unlock()
	if err != nil {
		return types.BlockID{}, nil, err
	}
	return block, diffs, err
}

// HandleLateBlocks processes votes and goodness for late blocks (for late block definition see white paper)
// returns the old verified layer and new verified layer after taking into account the blocks votes
func (trtl *ThreadSafeVerifyingTortoise) HandleLateBlocks(ctx context.Context, blocks []*types.Block) (types.LayerID, types.LayerID) {
	trtl.mutex.Lock()
	defer trtl.mutex.Unlock()
	oldVerified := trtl.trtl.Verified
	if err := trtl.trtl.ProcessNewBlocks(ctx, blocks); err != nil {
		// consider panicking here instead, since it means tortoise is stuck
		trtl.logger.WithContext(ctx).With().Error("tortoise errored handling late blocks", log.Err(err))
	}
	newVerified := trtl.trtl.Verified
	return oldVerified, newVerified
}

// HandleIncomingLayer processes all layer block votes
// returns the old verified layer and new verified layer after taking into account the blocks votes
func (trtl *ThreadSafeVerifyingTortoise) HandleIncomingLayer(ctx context.Context, layerID types.LayerID) (oldVerified, newVerified types.LayerID, reverted bool) {
	trtl.mutex.Lock()
	defer trtl.mutex.Unlock()

	oldVerified = trtl.trtl.Verified

	// first check if it's time for a total rerun
	trtl.logger.With().Debug("checking if tortoise needs to rerun from genesis",
		log.Duration("rerun_interval", trtl.trtl.RerunInterval),
		log.Time("last_rerun", trtl.lastRerun))

	// TODO: in future we can do something more sophisticated, using accounting to determine when enough changes to old
	//   layers have accumulated (in terms of block weight) that our opinion could actually change. For now, we do the
	//   Simplest Possible Thing (TM) and just rerun from genesis once in a while. This requires a different instance of
	//   tortoise since we don't want to mess with the state of the main tortoise. We re-stream layer data from genesis
	//   using the sliding window, simulating a full resync.
	if time.Now().Sub(trtl.lastRerun) > trtl.trtl.RerunInterval {
		var revertLayer types.LayerID
		if reverted, revertLayer = trtl.rerunFromGenesis(ctx); reverted {
			// make sure state is reapplied from far enough back if there was a state reversion
			oldVerified = revertLayer
		}
		trtl.lastRerun = time.Now()
	}

	// Even after a rerun, we still need to process the new incoming layer
	trtl.logger.WithContext(ctx).With().Info("handling incoming layer",
		log.FieldNamed("old_pbase", oldVerified),
		log.FieldNamed("incoming_layer", layerID))
	if err := trtl.trtl.HandleIncomingLayer(ctx, layerID); err != nil {
		// consider panicking here instead, since it means tortoise is stuck
		trtl.logger.WithContext(ctx).With().Error("tortoise errored handling incoming layer", log.Err(err))
	}

	newVerified = trtl.trtl.Verified
	trtl.logger.WithContext(ctx).With().Info("finished handling incoming layer",
		log.FieldNamed("old_pbase", oldVerified),
		log.FieldNamed("new_pbase", newVerified),
		log.FieldNamed("incoming_layer", layerID))

	return
}

// this wrapper monitors the tortoise rerun for database changes that would cause us to need to revert state
type bdpWrapper struct {
	blockDataProvider
	firstUpdatedLayer *types.LayerID
}

// SaveContextualValidity overrides the method in the embedded type to check if we've made changes
func (bdp *bdpWrapper) SaveContextualValidity(bid types.BlockID, lid types.LayerID, validityNew bool) error {
	// we only need to know about the first updated layer
	if bdp.firstUpdatedLayer == nil {
		// first, get current value
		validityCur, err := bdp.ContextualValidity(bid)
		if err != nil {
			return fmt.Errorf("error reading contextual validity of block %v: %w", bid, err)
		}
		if validityCur != validityNew {
			bdp.firstUpdatedLayer = &lid
		}
	}
	return bdp.blockDataProvider.SaveContextualValidity(bid, lid, validityNew)
}

// trigger a rerun from genesis once in a while
func (trtl *ThreadSafeVerifyingTortoise) rerunFromGenesis(ctx context.Context) (reverted bool, revertLayer types.LayerID) {
	// TODO: should this happen "in the background" in a separate goroutine? Should it hold the mutex?
	logger := trtl.logger.WithContext(ctx)
	logger.With().Info("triggering tortoise full rerun from genesis")

	// start from scratch with a new tortoise instance for each rerun
	trtlForRerun := trtl.trtl.cloneTurtle()
	trtlForRerun.SetLogger(logger.WithFields(log.String("tortoise_rerun", "true")))
	trtlForRerun.init(ctx, mesh.GenesisLayer())
	bdp := bdpWrapper{blockDataProvider: trtlForRerun.bdp}
	trtlForRerun.bdp = &bdp

	for layerID := types.GetEffectiveGenesis(); layerID <= trtl.trtl.Last; layerID++ {
		logger.With().Debug("rerunning tortoise for layer", layerID)
		if err := trtlForRerun.HandleIncomingLayer(ctx, layerID); err != nil {
			logger.With().Error("tortoise rerun errored", log.Err(err))
			// bail out completely if we encounter an error: don't revert state and don't swap out the trtl
			// TODO: give this some more thought
			return
		}
	}

	// revert state if necessary
	// state will be reapplied in mesh after we return, no need to reapply here
	if bdp.firstUpdatedLayer != nil {
		logger.With().Warning("turtle rerun detected state changes, attempting to reapply state from first changed layer",
			log.FieldNamed("first_layer", bdp.firstUpdatedLayer))
		reverted = true
		revertLayer = *bdp.firstUpdatedLayer
	}

	// swap out the turtle instances so its state is up to date
	trtlForRerun.bdp = trtl.trtl.bdp
	trtlForRerun.logger = trtl.trtl.logger
	trtl.trtl = trtlForRerun
	return
}

// Persist saves a copy of the current tortoise state to the database
func (trtl *ThreadSafeVerifyingTortoise) Persist(ctx context.Context) error {
	trtl.mutex.Lock()
	defer trtl.mutex.Unlock()
	trtl.trtl.logger.WithContext(ctx).Info("persist tortoise")
	return trtl.trtl.persist()
}
