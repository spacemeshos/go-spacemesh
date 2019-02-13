package consensus

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

type MeshValidator struct {
	Tortoise
	callback func(mesh.LayerID)
}

type Tortoise interface {
	handleIncomingLayer(ll *mesh.Layer)
}

func NewMeshValidator(trtl Tortoise) *MeshValidator {
	return &MeshValidator{Tortoise: trtl}
}

func (alg *MeshValidator) RegisterLayerCallback(callback func(mesh.LayerID)) {
	alg.callback = callback
}

func (alg *MeshValidator) HandleLateBlock(b *mesh.Block) {
	log.Info("received block with layer Id %v block id: %v ", b.Layer(), b.ID())
}

func (alg *MeshValidator) HandleIncomingLayer(ll *mesh.Layer) {
	alg.Tortoise.handleIncomingLayer(ll)
	alg.callback(ll.Index())
}
