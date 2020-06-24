package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

// Tortoise represents an instance of a vote counting algorithm
type Tortoise interface {
	HandleLateBlock(b *types.Block) (types.LayerID, types.LayerID)
	HandleIncomingLayer(ll *types.Layer) (types.LayerID, types.LayerID)
	LatestComplete() types.LayerID
	Persist() error
}

type tortoise struct {
	*ninjaTortoise
}

// NewTortoise returns a new Tortoise instance
func NewTortoise(layerSize int, mdb *mesh.DB, hdist int, lg log.Log) Tortoise {
	alg := &tortoise{ninjaTortoise: newNinjaTortoise(layerSize, mdb, hdist, lg)}
	alg.HandleIncomingLayer(mesh.GenesisLayer())
	return alg
}

// NewRecoveredTortoise recovers a previously persisted tortoise copy from mesh.DB
func NewRecoveredTortoise(mdb *mesh.DB, lg log.Log) Tortoise {
	tmp, err := RecoverTortoise(mdb)
	if err != nil {
		lg.Panic("could not recover tortoise state from disc ", err)
	}

	trtl := tmp.(*ninjaTortoise)

	lg.Info("recovered tortoise from disc")
	trtl.db = mdb
	trtl.logger = lg

	return &tortoise{ninjaTortoise: trtl}
}

// HandleLateBlock processes a late blocks votes (for late block definition see white paper)
// returns the old pbase and new pbase after taking into account the blocks votes
func (trtl *tortoise) HandleLateBlock(b *types.Block) (types.LayerID, types.LayerID) {
	// todo feed all layers from b's layer to tortoise
	l := types.NewLayer(b.Layer())
	l.AddBlock(b)
	oldPbase, newPbase := trtl.HandleIncomingLayer(l)
	log.With().Info("late block", b.Layer(), b.ID())
	return oldPbase, newPbase
}

// Persist saves a copy of the current tortoise state to the database
func (trtl *tortoise) Persist() error {
	trtl.mutex.Lock()
	defer trtl.mutex.Unlock()
	log.Info("persist tortoise ")
	return trtl.ninjaTortoise.persist()
}

// HandleIncomingLayer processes all layer block votes
// returns the old pbase and new pbase after taking into account the blocks votes
func (trtl *tortoise) HandleIncomingLayer(ll *types.Layer) (types.LayerID, types.LayerID) {
	trtl.mutex.Lock()
	defer trtl.mutex.Unlock()
	oldPbase := trtl.latestComplete()
	trtl.ninjaTortoise.handleIncomingLayer(ll)
	newPbase := trtl.latestComplete()
	updateMetrics(trtl, ll)
	return oldPbase, newPbase
}

// LatestComplete returns the latest complete (a.k.a irreversible) layer
func (trtl *tortoise) LatestComplete() types.LayerID {
	return trtl.latestComplete()
}

func updateMetrics(alg *tortoise, ll *types.Layer) {
	pbaseCount.Set(float64(alg.latestComplete()))
	processedCount.Set(float64(ll.Index()))
}
