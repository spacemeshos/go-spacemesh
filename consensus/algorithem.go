package consensus

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

type Algorithem struct {
	Tortoise
	callback func(mesh.LayerID)
}

type Tortoise interface {
	handleIncomingLayer(ll *mesh.Layer)
}

func NewAlgorithem(trtl Tortoise) *Algorithem {
	return &Algorithem{Tortoise: trtl}
}

func (alg *Algorithem) RegisterLayerCallback(callback func(mesh.LayerID)) {
	alg.callback = callback
}

func (alg *Algorithem) HandleLateBlock(b *mesh.Block) {
	log.Info("received block with layer Id %v block id: %v ", b.Layer(), b.ID())
}

func (alg *Algorithem) HandleIncomingLayer(ll *mesh.Layer) {
	alg.Tortoise.handleIncomingLayer(ll)
	alg.callback(ll.Index())
}
