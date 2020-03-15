package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

//Tortoise represents an instance of the vote counting algorithm
//this was done to allow more than one vote counting implementation
type Tortoise interface {
	HandleLateBlock(b *types.Block) (types.LayerID, types.LayerID)
	HandleIncomingLayer(ll *types.Layer) (types.LayerID, types.LayerID)
	LatestComplete() types.LayerID
	Persist() error
}

type tortoise struct {
	*ninjaTortoise
}

//NewTortoise returns a new Tortoise instance
func NewTortoise(layerSize int, mdb *mesh.MeshDB, hdist int, lg log.Log) Tortoise {
	alg := &tortoise{ninjaTortoise: NewNinjaTortoise(layerSize, mdb, hdist, lg)}
	alg.HandleIncomingLayer(mesh.GenesisLayer())
	return alg
}

//NewRecoveredTortoise recovers a previously persisted tortoise copy from mesh.MeshDB
func NewRecoveredTortoise(mdb *mesh.MeshDB, lg log.Log) Tortoise {
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

//HandleLateBlock processes a late blocks votes (for late block definition see white paper)
//returns the old pbase and new pbase after taking into account the blocks votes
func (alg *tortoise) HandleLateBlock(b *types.Block) (types.LayerID, types.LayerID) {
	//todo feed all layers from b's layer to tortoise
	l := types.NewLayer(b.Layer())
	l.AddBlock(b)
	oldPbase, newPbase := alg.HandleIncomingLayer(l)
	log.With().Info("late block ", log.LayerId(uint64(b.Layer())), log.BlockId(b.Id().String()))
	return oldPbase, newPbase
}

//Persist saves a copy of the current tortoise state to the database
func (alg *tortoise) Persist() error {
	alg.mutex.Lock()
	defer alg.mutex.Unlock()
	log.Info("persist tortoise ")
	return alg.ninjaTortoise.persist()
}

//HandleIncomingLayer processes all layer block votes
//returns the old pbase and new pbase after taking into account the blocks votes
func (alg *tortoise) HandleIncomingLayer(ll *types.Layer) (types.LayerID, types.LayerID) {
	alg.mutex.Lock()
	defer alg.mutex.Unlock()
	oldPbase := alg.latestComplete()
	alg.ninjaTortoise.handleIncomingLayer(ll)
	newPbase := alg.latestComplete()
	updateMetrics(alg, ll)
	return oldPbase, newPbase
}

//HandleLateBlock processes a late blocks votes (for late block definition see white paper)
//returns the old pbase and new pbase after taking into account the blocks votes
func (alg *tortoise) LatestComplete() types.LayerID {
	return alg.latestComplete()
}

func updateMetrics(alg *tortoise, ll *types.Layer) {
	pbaseCount.Set(float64(alg.latestComplete()))
	processedCount.Set(float64(ll.Index()))
}
