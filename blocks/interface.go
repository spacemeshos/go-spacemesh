package blocks

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type layerPatrol interface {
	CompleteHare(types.LayerID)
}

type meshProvider interface {
	ProcessedLayer() types.LayerID
	AddBlockWithTXs(context.Context, *types.Block) error
	ProcessLayerPerHareOutput(context.Context, types.LayerID, types.BlockID, bool) error
}

type executor interface {
	ExecuteOptimistic(
		context.Context,
		types.LayerID,
		uint64,
		[]types.AnyReward,
		[]types.TransactionID,
	) (*types.Block, error)
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) <-chan struct{}
	CurrentLayer() types.LayerID
}

type certifier interface {
	RegisterForCert(context.Context, types.LayerID, types.BlockID) error
	CertifyIfEligible(context.Context, types.LayerID, types.BlockID) error
}

type tortoiseProvider interface {
	GetMissingActiveSet(types.EpochID, []types.ATXID) []types.ATXID
}
