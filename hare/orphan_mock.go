package hare

import (
	"github.com/spacemeshos/go-spacemesh/mesh"
)

type orphanMock struct {
	f func() []mesh.BlockID
}

func (op *orphanMock) GetOrphanBlocks() []mesh.BlockID {
	if op.f != nil {
		return op.f()
	}
	return []mesh.BlockID{}
}

func (op *orphanMock) GetUnverifiedLayerBlocks(l mesh.LayerID) ([]mesh.BlockID, error) {
	if op.f != nil {
		return op.f(), nil
	}
	return []mesh.BlockID{}, nil
}
