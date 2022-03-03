package tortoise

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interfaces.go

type blockDataProvider interface {
	LayerContextuallyValidBlocks(context.Context, types.LayerID) (map[types.BlockID]struct{}, error)
	GetBlock(types.BlockID) (*types.Block, error)
	GetBallot(id types.BallotID) (*types.Ballot, error)
	LayerBallots(types.LayerID) ([]*types.Ballot, error)
	LayerBlockIds(layerID types.LayerID) ([]types.BlockID, error)
	GetCoinflip(context.Context, types.LayerID) (bool, bool)
	GetHareConsensusOutput(types.LayerID) (types.BlockID, error)
	ContextualValidity(types.BlockID) (bool, error)
}

type blockValidityUpdater interface {
	UpdateBlockValidity(types.BlockID, types.LayerID, bool) error
}

type atxDataProvider interface {
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
	GetEpochWeight(epochID types.EpochID) (uint64, []types.ATXID, error)
}
