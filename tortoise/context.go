package tortoise

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func wrapContext(ctx context.Context) *tcontext {
	return &tcontext{
		Context:      ctx,
		localOpinion: map[types.LayerID]map[types.BlockID]vec{},
		layerBlocks:  map[types.LayerID][]types.BlockID{},
		inputVectors: map[types.LayerID][]types.BlockID{},
		validBlocks:  map[types.LayerID][]types.BlockID{},
	}
}

// tcontext is meant for caching db queries within a single logical operation, therefore it is context.
type tcontext struct {
	context.Context
	// generated local opinion from computeLocalOpinon
	localOpinion map[types.LayerID]map[types.BlockID]vec
	// all blocks that we know about
	layerBlocks map[types.LayerID][]types.BlockID
	// hare's input vectors
	inputVectors map[types.LayerID][]types.BlockID
	// contextually valid blocks
	validBlocks map[types.LayerID][]types.BlockID
}

func (t *turtle) getLocalOpinion(ctx *tcontext, lid types.LayerID) (map[types.BlockID]vec, error) {
	opinion, exists := ctx.localOpinion[lid]
	if exists {
		return opinion, nil
	}
	opinion, err := t.computeLocalOpinion(ctx, lid)
	if err != nil {
		return nil, err
	}
	ctx.localOpinion[lid] = opinion
	return opinion, nil
}

func (t *turtle) getInputVector(ctx *tcontext, lid types.LayerID) ([]types.BlockID, error) {
	bids, exist := ctx.inputVectors[lid]
	if exist {
		return bids, nil
	}
	bids, err := t.bdp.GetLayerInputVectorByID(lid)
	if err != nil {
		return nil, fmt.Errorf("read input vector blocks for layer %s: %w", lid, err)
	}
	ctx.inputVectors[lid] = bids
	return bids, nil
}

func (t *turtle) getValidBlocks(ctx *tcontext, lid types.LayerID) ([]types.BlockID, error) {
	bids, exist := ctx.validBlocks[lid]
	if exist {
		return bids, nil
	}
	bidsmap, err := t.bdp.LayerContextuallyValidBlocks(ctx, lid)
	if err != nil {
		return nil, fmt.Errorf("read valid blocks for layer %s: %w", lid, err)
	}
	bids = blockMapToArray(bidsmap)
	ctx.validBlocks[lid] = bids
	return bids, nil
}

func (t *turtle) getLayerBlocksIDs(ctx *tcontext, lid types.LayerID) ([]types.BlockID, error) {
	bids, exist := ctx.layerBlocks[lid]
	if exist {
		return bids, nil
	}
	bids, err := t.bdp.LayerBlockIds(lid)
	if err != nil {
		return nil, fmt.Errorf("read blocks for layer %s: %w", lid, err)
	}
	ctx.layerBlocks[lid] = bids
	return bids, nil
}
