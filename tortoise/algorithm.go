package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/types"
)

type Algorithm struct {
	Tortoise
}

type Tortoise interface {
	handleIncomingLayer(ll *types.Layer)
	latestComplete() types.LayerID
	getVote(id types.BlockID) vec
	getVotes() map[types.BlockID]vec
}

func NewAlgorithm(layerSize int, mdb *mesh.MeshDB, lg log.Log) *Algorithm {
	alg := &Algorithm{Tortoise: NewNinjaTortoise(layerSize, mdb, lg)}
	alg.HandleIncomingLayer(mesh.GenesisLayer())
	return alg
}
func (alg *Algorithm) HandleLateBlock(b *types.Block) {
	//todo feed all layers from b's layer to tortoise
	log.Info("received block with mesh.LayerID %v block id: %v ", b.Layer(), b.ID())
}

func (alg *Algorithm) HandleIncomingLayer(ll *types.Layer) (types.LayerID, types.LayerID) {
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
	return alg.getVote(id) == Support
}
