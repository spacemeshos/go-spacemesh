package hare

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type orphanMock struct {
	f func() []types.BlockID
}

func (op *orphanMock) GetOrphanBlocks() []types.BlockID {
	if op.f != nil {
		return op.f()
	}
	return []types.BlockID{}
}

func (op *orphanMock) LayerBlockIds(l types.LayerID) ([]types.BlockID, error) {
	if op.f != nil {
		return op.f(), nil
	}
	return []types.BlockID{}, nil
}
