package hare

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type orphanMock struct {
	f func() []types.BlockID
}

func (op *orphanMock) HandleValidatedLayer(context.Context, types.LayerID, []types.BlockID) {
}

func (op *orphanMock) InvalidateLayer(context.Context, types.LayerID) {
}

func (op *orphanMock) RecordCoinflip(context.Context, types.LayerID, bool) {
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
