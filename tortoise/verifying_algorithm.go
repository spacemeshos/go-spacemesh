package tortoise

import (
	"context"
	"errors"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
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
	LayerSize    int
	Database     database.Database
	MeshDatabase blockDataProvider
	Hdist        uint32
	Log          log.Log
}

// NewVerifyingTortoise creates a new verifying tortoise wrapper
func NewVerifyingTortoise(cfg Config) *ThreadSafeVerifyingTortoise {
	alg := &ThreadSafeVerifyingTortoise{
		trtl: newTurtle(cfg.Log, cfg.Database, cfg.MeshDatabase, cfg.Hdist, cfg.LayerSize),
	}
	if err := alg.trtl.Recover(); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			alg.trtl.init(mesh.GenesisLayer())
		} else {
			cfg.Log.Panic("can't recover turtle state", log.Err(err))
		}
	}
	return alg
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
func (trtl *ThreadSafeVerifyingTortoise) HandleIncomingLayer(ll *types.Layer) (types.LayerID, types.LayerID) {
	trtl.mutex.Lock()
	defer trtl.mutex.Unlock()
	oldVerified := trtl.trtl.Verified
	trtl.trtl.HandleIncomingLayer(ll)
	newVerified := trtl.trtl.Verified
	return oldVerified, newVerified
}

// HandleLateBlock processes a late blocks votes (for late block definition see white paper)
// returns the old verified layer and new verified layer after taking into account the blocks votes
func (trtl *ThreadSafeVerifyingTortoise) HandleLateBlock(b *types.Block) (types.LayerID, types.LayerID) {
	//todo feed all layers from b's layer to tortoise
	l := types.NewLayer(b.Layer())
	l.AddBlock(b)
	oldVerified, newVerified := trtl.HandleIncomingLayer(l) // block wasn't in input vector for sure.
	trtl.trtl.logger.With().Debug("late block ", b.Layer(), b.ID())
	return oldVerified, newVerified
}

// Persist saves a copy of the current tortoise state to the database
func (trtl *ThreadSafeVerifyingTortoise) Persist() error {
	trtl.mutex.Lock()
	defer trtl.mutex.Unlock()
	trtl.trtl.logger.Debug("persist tortoise ")
	return trtl.trtl.persist()
}
