package consensus

import (
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

type Algorithm struct {
	Tortoise
}

type Tortoise interface {
	handleIncomingLayer(ll *mesh.Layer)
	latestComplete() mesh.LayerID
	getVote(id mesh.BlockID) vec
}

func NewAlgorithm(trtl Tortoise) *Algorithm {
	return &Algorithm{Tortoise: trtl}
}

func (alg *Algorithm) HandleLateBlock(b *mesh.Block) {
	//todo feed all layers from b's layer to tortoise
	log.Info("received block with layer Id %v block id: %v ", b.Layer(), b.ID())
}

func (alg *Algorithm) HandleIncomingLayer(ll *mesh.Layer) (mesh.LayerID, mesh.LayerID) {
	oldPbase := alg.latestComplete()
	alg.Tortoise.handleIncomingLayer(ll)
	newPbase := alg.latestComplete()
	return oldPbase, newPbase
}

func (alg *Algorithm) ContextualValidity(id mesh.BlockID) bool {
	return alg.getVote(id) == Support
}

func CreateGenesisBlock() *mesh.Block {
	log.Info("Creating genesis")
	bl := &mesh.Block{
		Id:         mesh.BlockID(config.GenesisId),
		LayerIndex: 0,
		Data:       []byte("genesis"),
	}
	return bl
}

func createGenesisLayer() *mesh.Layer {
	log.Info("Creating genesis")
	l := mesh.NewLayer(Genesis)
	l.AddBlock(CreateGenesisBlock())
	return l
}
