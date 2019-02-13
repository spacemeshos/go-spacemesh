package consensus

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

type Algorithm struct {
	Tortoise
}

type Tortoise interface {
	handleIncomingLayer(ll *mesh.Layer)
	latestComplete() mesh.LayerID
}

func NewAlgorithm(trtl Tortoise) *Algorithm {
	return &Algorithm{Tortoise: trtl}
}

func (alg *Algorithm) HandleLateBlock(b *mesh.Block) {
	log.Info("received block with layer Id %v block id: %v ", b.Layer(), b.ID())
}

func (alg *Algorithm) HandleIncomingLayer(ll *mesh.Layer) (mesh.LayerID, mesh.LayerID) {
	old := alg.latestComplete()
	alg.Tortoise.handleIncomingLayer(ll)
	new := alg.latestComplete()
	return old, new
}

func (alg *Algorithm) ContextualValidity(id mesh.BlockID) bool {
	return true
}

func CreateGenesisLayer() *mesh.Layer {
	log.Info("Creating genesis")
	bl := &mesh.Block{
		Id:         mesh.BlockID(0),
		LayerIndex: 0,
		Data:       []byte("genesis"),
	}
	l := mesh.NewLayer(Genesis)
	l.AddBlock(bl)
	return l
}
