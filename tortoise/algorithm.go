package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"sync"
)

type Algorithm struct {
	Tortoise
	sync.Mutex
}

type Tortoise interface {
	handleIncomingLayer(ll *types.Layer)
	latestComplete() types.LayerID
	getVote(id types.BlockID) vec
	getVotes() map[types.BlockID]vec
}

func NewAlgorithm(layerSize int, mdb *mesh.MeshDB, hdist int, lg log.Log) *Algorithm {
	alg := &Algorithm{Tortoise: NewNinjaTortoise(layerSize, mdb, hdist, lg)}
	alg.HandleIncomingLayer(mesh.GenesisLayer())
	return alg
}

func NewRecoveredAlgorithm(mdb *mesh.MeshDB, lg log.Log) *Algorithm {
	trtl := &NinjaTortoise{Log: lg, Mesh: mdb}

	ni, err := trtl.RecoverTortoise()
	if err != nil {
		lg.Panic("could not recover tortoise state from disc ", err)
	}

	lg.Info("recovered tortoise from disc")
	trtl.ninjaTortoise = ni.(*ninjaTortoise)

	alg := &Algorithm{Tortoise: trtl}

	return alg
}

func (alg *Algorithm) HandleLateBlock(b *types.Block) {
	//todo feed all layers from b's layer to tortoise
	l := types.NewLayer(b.Layer())
	l.AddBlock(b)
	alg.HandleIncomingLayer(l)
	log.With().Info("late block ", log.LayerId(uint64(b.Layer())), log.BlockId(b.Id().String()))
}

func (alg *Algorithm) HandleIncomingLayer(ll *types.Layer) (types.LayerID, types.LayerID) {
	alg.Lock()
	defer alg.Unlock()
	oldPbase := alg.latestComplete()
	alg.Tortoise.handleIncomingLayer(ll)
	newPbase := alg.latestComplete()
	updateMetrics(alg, ll)
	return oldPbase, newPbase
}

func updateMetrics(alg *Algorithm, ll *types.Layer) {
	pbaseCount.Set(float64(alg.latestComplete()))
	processedCount.Set(float64(ll.Index()))
	var valid float64
	var invalid float64
	for _, k := range alg.getVotes() {
		if k == Support {
			valid++
		} else {
			invalid++
		}
	}
	validBlocks.Set(valid)
	invalidBlocks.Set(invalid)
}

func (alg *Algorithm) ContextualValidity(id types.BlockID) bool {
	//todo after each layer we should persist alg.getVote(id) in mesh
	return alg.getVote(id) == Support
}
