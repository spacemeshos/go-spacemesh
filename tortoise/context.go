package tortoise

import (
	"context"

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
