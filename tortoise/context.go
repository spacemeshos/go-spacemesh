package tortoise

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func newContext(ctx context.Context) *tcontext {
	return &tcontext{
		Context:      ctx,
		LocalOpinion: map[types.LayerID]map[types.BlockID]vec{},
		LayerBlocks:  map[types.LayerID][]types.BlockID{},
		InputVectors: map[types.LayerID][]types.BlockID{},
		ValidBlocks:  map[types.LayerID][]types.BlockID{},
	}
}

type tcontext struct {
	context.Context
	LocalOpinion map[types.LayerID]map[types.BlockID]vec
	LayerBlocks  map[types.LayerID][]types.BlockID
	InputVectors map[types.LayerID][]types.BlockID
	ValidBlocks  map[types.LayerID][]types.BlockID
}

func (t *turtle) getLocalOpinion(ctx *tcontext, lid types.LayerID) (map[types.BlockID]vec, error) {
	opinion, exists := ctx.LocalOpinion[lid]
	if exists {
		return opinion, nil
	}
	opinion, err := t.computeLocalOpinion(ctx, lid)
	if err != nil {
		return nil, err
	}
	ctx.LocalOpinion[lid] = opinion
	return opinion, nil
}

func (t *turtle) getInputVector(ctx *tcontext, lid types.LayerID) ([]types.BlockID, error) {
	bids, exist := ctx.InputVectors[lid]
	if exist {
		return bids, nil
	}
	bids, err := t.bdp.GetLayerInputVectorByID(lid)
	if err != nil {
		return nil, fmt.Errorf("read input vector blocks for layer %s: %w", lid, err)
	}
	ctx.InputVectors[lid] = bids
	return bids, nil
}

func (t *turtle) getValidBlocks(ctx *tcontext, lid types.LayerID) ([]types.BlockID, error) {
	bids, exist := ctx.ValidBlocks[lid]
	if exist {
		return bids, nil
	}
	bidsmap, err := t.bdp.LayerContextuallyValidBlocks(ctx, lid)
	if err != nil {
		return nil, fmt.Errorf("read valid blocks for layer %s: %w", lid, err)
	}
	bids = blockMapToArray(bidsmap)
	ctx.ValidBlocks[lid] = bids
	return bids, nil
}

func (t *turtle) getLayerBlocksIDs(ctx *tcontext, lid types.LayerID) ([]types.BlockID, error) {
	bids, exist := ctx.LayerBlocks[lid]
	if exist {
		return bids, nil
	}
	bids, err := t.bdp.LayerBlockIds(lid)
	if err != nil {
		return nil, fmt.Errorf("read blocks for layer %s: %w", lid, err)
	}
	ctx.LayerBlocks[lid] = bids
	return bids, nil
}
