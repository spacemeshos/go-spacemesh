package hare

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type orphanMock struct {
	f                 func() []types.BlockID
	recordCoinflipsFn func(context.Context, types.LayerID, bool)
}

func (op *orphanMock) HandleValidatedLayer(context.Context, types.LayerID, []types.BlockID) {
}

func (op *orphanMock) GetOrphanBlocks() []types.BlockID {
	if op.f != nil {
		return op.f()
	}
	return []types.BlockID{}
}

func (op *orphanMock) LayerBlockIds(types.LayerID) ([]types.BlockID, error) {
	if op.f != nil {
		return op.f(), nil
	}
	return []types.BlockID{}, nil
}

func (op *orphanMock) RecordCoinflip(ctx context.Context, layerID types.LayerID, coinflip bool) {
	if op.recordCoinflipsFn != nil {
		op.recordCoinflipsFn(ctx, layerID, coinflip)
	}
}
