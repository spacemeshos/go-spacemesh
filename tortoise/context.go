package tortoise

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func wrapContext(ctx context.Context) *tcontext {
	return &tcontext{
		Context:      ctx,
		localOpinion: map[types.LayerID]map[types.BlockID]sign{},
		inputVectors: map[types.LayerID][]types.BlockID{},
		validBlocks:  map[types.LayerID][]types.BlockID{},
	}
}

// tcontext is meant for caching db queries within a single logical operation, therefore it is context.
type tcontext struct {
	context.Context
	// generated local opinion from computeLocalOpinon
	localOpinion map[types.LayerID]map[types.BlockID]sign
	// hare's input vectors
	inputVectors map[types.LayerID][]types.BlockID
	// contextually valid blocks
	validBlocks map[types.LayerID][]types.BlockID
}

func (t *turtle) getLocalOpinion(ctx *tcontext, lid types.LayerID) (map[types.BlockID]sign, error) {
	opinion, exists := ctx.localOpinion[lid]
	if exists {
		return opinion, nil
	}
	opinion = Opinion{}
	err := t.addLocalOpinion(ctx, lid, opinion)
	if err != nil {
		return nil, err
	}
	ctx.localOpinion[lid] = opinion
	return opinion, nil
}

func getInputVector(ctx *tcontext, bdp blockDataProvider, lid types.LayerID) ([]types.BlockID, error) {
	bids, exist := ctx.inputVectors[lid]
	if exist {
		return bids, nil
	}
	bids, err := bdp.GetLayerInputVectorByID(lid)
	if err != nil {
		return nil, fmt.Errorf("read input vector blocks for layer %s: %w", lid, err)
	}
	ctx.inputVectors[lid] = bids
	return bids, nil
}

func getValidBlocks(ctx *tcontext, bdp blockDataProvider, lid types.LayerID) ([]types.BlockID, error) {
	bids, exist := ctx.validBlocks[lid]
	if exist {
		return bids, nil
	}
	bidsmap, err := bdp.LayerContextuallyValidBlocks(ctx, lid)
	if err != nil {
		return nil, fmt.Errorf("read valid blocks for layer %s: %w", lid, err)
	}
	bids = blockMapToArray(bidsmap)
	ctx.validBlocks[lid] = bids
	return bids, nil
}
