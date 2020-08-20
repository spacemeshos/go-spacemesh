package tortoise

import (
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

type ThreadSafeVerifyingTortoise interface {
	HandleLateBlock(b *types.Block) (types.LayerID, types.LayerID)
	HandleIncomingLayer(ll *types.Layer, inputVector []types.BlockID) (types.LayerID, types.LayerID)
	LatestComplete() types.LayerID
	BaseBlock(getres func(id types.LayerID) ([]types.BlockID, error)) (types.BlockID, [][]types.BlockID, error)
	Persist() error
}

type verifyingTortoiseWrapper struct {
	trtl  *turtle
	mutex sync.Mutex
}

func NewVerifyingTortoise(layerSize int, mdb blockDataProvider, hdist int, lg log.Log) ThreadSafeVerifyingTortoise {
	alg := &verifyingTortoiseWrapper{trtl: NewTurtle(mdb, hdist, layerSize)}
	alg.trtl.SetLogger(lg)
	alg.trtl.init(mesh.GenesisLayer())
	return alg
}

//NewRecoveredTortoise recovers a previously persisted tortoise copy from mesh.DB
func NewRecoveredVerifyingTortoise(mdb blockDataProvider, lg log.Log) ThreadSafeVerifyingTortoise {
	tmp, err := RecoverVerifyingTortoise(mdb)
	if err != nil {
		lg.Panic("could not recover tortoise state from disc ", err)
	}

	trtl := tmp.(*turtle)

	lg.Info("recovered tortoise from disc")
	trtl.bdp = mdb
	trtl.logger = lg

	return &verifyingTortoiseWrapper{trtl: trtl}
}

func (trtl *verifyingTortoiseWrapper) LatestComplete() types.LayerID {
	trtl.mutex.Lock()
	verified := trtl.trtl.Verified
	trtl.mutex.Unlock()
	return verified
}

func (trtl *verifyingTortoiseWrapper) BaseBlock(getres func(id types.LayerID) ([]types.BlockID, error)) (types.BlockID, [][]types.BlockID, error) {
	trtl.mutex.Lock()
	block, diffs, err := trtl.trtl.BaseBlock(getres)
	trtl.mutex.Unlock()
	if err != nil {
		return types.BlockID{}, nil, err
	}
	return block, diffs, err
}

//HandleIncomingLayer processes all layer block votes
//returns the old pbase and new pbase after taking into account the blocks votes
func (trtl *verifyingTortoiseWrapper) HandleIncomingLayer(ll *types.Layer, inputVector []types.BlockID) (types.LayerID, types.LayerID) {
	trtl.mutex.Lock()
	defer trtl.mutex.Unlock()
	oldPbase := trtl.trtl.Verified
	trtl.trtl.HandleIncomingLayer(ll, inputVector)
	newPbase := trtl.trtl.Verified
	return oldPbase, newPbase
}

//HandleLateBlock processes a late blocks votes (for late block definition see white paper)
//returns the old pbase and new pbase after taking into account the blocks votes
func (trtl *verifyingTortoiseWrapper) HandleLateBlock(b *types.Block) (types.LayerID, types.LayerID) {
	//todo feed all layers from b's layer to tortoise
	l := types.NewLayer(b.Layer())
	l.AddBlock(b)
	oldPbase, newPbase := trtl.HandleIncomingLayer(l, []types.BlockID{}) // block wasn't in input vector for sure.
	log.With().Info("late block ", log.LayerID(uint64(b.Layer())), log.BlockID(b.ID().String()))
	return oldPbase, newPbase
}

//Persist saves a copy of the current tortoise state to the database
func (trtl *verifyingTortoiseWrapper) Persist() error {
	trtl.mutex.Lock()
	defer trtl.mutex.Unlock()
	log.Info("persist tortoise ")
	return trtl.trtl.persist()
}
