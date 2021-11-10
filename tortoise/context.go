package tortoise

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func newContext(ctx context.Context) *tcontext {
	return &tcontext{
		Context:      ctx,
		LayerBlocks:  map[types.LayerID][]types.BlockID{},
		InputVectors: map[types.LayerID][]types.BlockID{},
		ValidBlocks:  map[types.LayerID]map[types.BlockID]struct{}{},
	}
}

type tcontext struct {
	context.Context
	LayerBlocks  map[types.LayerID][]types.BlockID
	InputVectors map[types.LayerID][]types.BlockID
	// contextually valid blocks per layer
	ValidBlocks map[types.LayerID]map[types.BlockID]struct{}
}
