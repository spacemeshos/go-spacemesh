package tortoise

import (
	"context"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

// ThreadSafeVerifyingTortoise is a thread safe verifying tortoise wrapper, it just locks all actions.
type ThreadSafeVerifyingTortoise struct {
	trtl  *turtle
	mutex sync.RWMutex
}

// Config holds the arguments and dependencies to create a verifying tortoise instance.
type Config struct {
	LayerSyze int
	Database  blockDataProvider
	Hdist     int // hare lookback distance: the distance over which we use the input vector/hare results
	Zdist     int // hare result wait distance: the distance over which we're willing to wait for hare results
	Log       log.Log
	Recovered bool
}

// NewVerifyingTortoise creates a new verifying tortoise wrapper
func NewVerifyingTortoise(ctx context.Context, cfg Config) *ThreadSafeVerifyingTortoise {
	if cfg.Recovered {
		return recoveredVerifyingTortoise(cfg.Database, cfg.Log)
	}
	return verifyingTortoise(ctx, cfg.LayerSyze, cfg.Database, cfg.Hdist, cfg.Zdist, cfg.Log)
}

// verifyingTortoise creates a new verifying tortoise wrapper
func verifyingTortoise(ctx context.Context, layerSize int, mdb blockDataProvider, hdist int, zdist int, logger log.Log) *ThreadSafeVerifyingTortoise {
	if hdist < zdist {
		logger.With().Panic("hdist must be >= zdist", log.Int("hdist", hdist), log.Int("zdist", zdist))
	}
	alg := &ThreadSafeVerifyingTortoise{trtl: newTurtle(mdb, hdist, zdist, layerSize)}
	alg.trtl.SetLogger(logger)
	alg.trtl.init(ctx, mesh.GenesisLayer())
	return alg
}

// NewRecoveredVerifyingTortoise recovers a previously persisted tortoise copy from mesh.DB
func recoveredVerifyingTortoise(mdb blockDataProvider, lg log.Log) *ThreadSafeVerifyingTortoise {
	tmp, err := RecoverVerifyingTortoise(mdb)
	if err != nil {
		lg.With().Panic("could not recover tortoise state from disk", log.Err(err))
	}

	trtl := tmp.(*turtle)

	lg.Info("recovered tortoise from disk")
	trtl.bdp = mdb
	trtl.logger = lg

	return &ThreadSafeVerifyingTortoise{trtl: trtl}
}

// LatestComplete returns the latest verified layer. TODO: rename?
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

//HandleIncomingLayer processes all layer block votes
// returns the old verified layer and new verified layer after taking into account the blocks votes
func (trtl *ThreadSafeVerifyingTortoise) HandleIncomingLayer(ctx context.Context, ll *types.Layer) (types.LayerID, types.LayerID) {
	trtl.mutex.Lock()
	defer trtl.mutex.Unlock()
	oldVerified := trtl.trtl.Verified
	trtl.trtl.HandleIncomingLayer(ctx, ll)
	newVerified := trtl.trtl.Verified
	return oldVerified, newVerified
}

// HandleLateBlock processes a late blocks votes (for late block definition see white paper)
// returns the old verified layer and new verified layer after taking into account the blocks votes
func (trtl *ThreadSafeVerifyingTortoise) HandleLateBlock(ctx context.Context, b *types.Block) (types.LayerID, types.LayerID) {
	//todo feed all layers from b's layer to tortoise
	l := types.NewLayer(b.Layer())
	l.AddBlock(b)
	oldVerified, newVerified := trtl.HandleIncomingLayer(ctx, l) // block wasn't in input vector for sure
	trtl.trtl.logger.WithContext(ctx).With().Info("late block", b.Layer(), b.ID())
	return oldVerified, newVerified
}

// Persist saves a copy of the current tortoise state to the database
func (trtl *ThreadSafeVerifyingTortoise) Persist(ctx context.Context) error {
	trtl.mutex.Lock()
	defer trtl.mutex.Unlock()
	trtl.trtl.logger.WithContext(ctx).Info("persist tortoise")
	return trtl.trtl.persist()
}
