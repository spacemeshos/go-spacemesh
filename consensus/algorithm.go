package consensus

import (
	"github.com/spacemeshos/go-spacemesh/block"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

type Algorithm struct {
	Tortoise
}

type Tortoise interface {
	handleIncomingLayer(ll *block.Layer)
	latestComplete() block.LayerID
	getVote(id block.BlockID) vec
	getVotes() map[block.BlockID]vec
}

func NewAlgorithm(trtl Tortoise) *Algorithm {
	alg := &Algorithm{Tortoise: trtl}
	alg.HandleIncomingLayer(mesh.GenesisLayer())
	return alg
}
func (alg *Algorithm) HandleLateBlock(b *block.Block) {
	//todo feed all layers from b's layer to tortoise
	log.Info("received block with mesh.LayerID %v block id: %v ", b.Layer(), b.ID())
}

func (alg *Algorithm) HandleIncomingLayer(ll *block.Layer) (block.LayerID, block.LayerID) {
	oldPbase := alg.latestComplete()
	alg.Tortoise.handleIncomingLayer(ll)
	newPbase := alg.latestComplete()
	updateMetrics(alg, ll)
	return oldPbase, newPbase
}

func updateMetrics(alg *Algorithm, ll *block.Layer) {
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

func (alg *Algorithm) ContextualValidity(id block.BlockID) bool {
	return alg.getVote(id) == Support
}
