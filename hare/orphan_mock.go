package hare

import (
	"github.com/spacemeshos/go-spacemesh/block"
)

type orphanMock struct {
	f func() []block.BlockID
}

func (op *orphanMock) GetOrphanBlocks() []block.BlockID {
	if op.f != nil {
		return op.f()
	}
	return []block.BlockID{}
}

func (op *orphanMock) GetUnverifiedLayerBlocks(l block.LayerID) ([]block.BlockID, error) {
	if op.f != nil {
		return op.f(), nil
	}
	return []block.BlockID{}, nil
}
