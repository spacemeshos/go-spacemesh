package tortoise

import (
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
	LayerSize int
	Database  blockDataProvider
	Hdist     int
	Log       log.Log
	Recovered bool
}

// NewVerifyingTortoise creates a new verifying tortoise wrapper
func NewVerifyingTortoise(cfg Config) *ThreadSafeVerifyingTortoise {
	if cfg.Recovered {
		return recoveredVerifyingTortoise(cfg.Database, cfg.Log)
	}
	return verifyingTortoise(cfg.LayerSize, cfg.Database, cfg.Hdist, cfg.Log)
}

// verifyingTortoise creates a new verifying tortoise wrapper
func verifyingTortoise(layerSize int, mdb blockDataProvider, hdist int, lg log.Log) *ThreadSafeVerifyingTortoise {
	alg := &ThreadSafeVerifyingTortoise{trtl: newTurtle(mdb, hdist, layerSize)}
	alg.trtl.SetLogger(lg)
	alg.trtl.init(mesh.GenesisLayer())
	return alg
}

// NewRecoveredVerifyingTortoise recovers a previously persisted tortoise copy from mesh.DB
func recoveredVerifyingTortoise(mdb blockDataProvider, lg log.Log) *ThreadSafeVerifyingTortoise {
	tmp, err := RecoverVerifyingTortoise(mdb)
	if err != nil {
		lg.Panic("could not recover tortoise state from disc ", err)
	}

	trtl := tmp.(*turtle)

	lg.Info("recovered tortoise from disc")
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
func (trtl *ThreadSafeVerifyingTortoise) BaseBlock() (types.BlockID, [][]types.BlockID, error) {
	trtl.mutex.Lock()
	block, diffs, err := trtl.trtl.BaseBlock()
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
	log.With().Info("late block ", b.Layer(), b.ID())
	return oldVerified, newVerified
}

// Persist saves a copy of the current tortoise state to the database
func (trtl *ThreadSafeVerifyingTortoise) Persist() error {
	trtl.mutex.Lock()
	defer trtl.mutex.Unlock()
	log.Info("persist tortoise ")
	return trtl.trtl.persist()
}
