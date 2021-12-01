package tortoise

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interfaces.go

type blockDataProvider interface {
	LayerContextuallyValidBlocks(context.Context, types.LayerID) (map[types.BlockID]struct{}, error)
	GetBlock(types.BlockID) (*types.Block, error)
	LayerBlockIds(types.LayerID) ([]types.BlockID, error)
	LayerBlocks(types.LayerID) ([]*types.Block, error)

	GetCoinflip(context.Context, types.LayerID) (bool, bool)
	GetLayerInputVectorByID(types.LayerID) ([]types.BlockID, error)
	SaveContextualValidity(types.BlockID, types.LayerID, bool) error
	ContextualValidity(types.BlockID) (bool, error)
}

type atxDataProvider interface {
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
	GetEpochWeight(epochID types.EpochID) (uint64, []types.ATXID, error)
}
