package syncer

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type layerTicker interface {
	GetCurrentLayer() types.LayerID
}

type layerFetcher interface {
	PollLayerData(context.Context, types.LayerID) chan fetch.LayerPromiseData
	PollLayerOpinions(context.Context, types.LayerID) chan fetch.LayerPromiseOpinions
	GetEpochATXs(context.Context, types.EpochID) error
	GetBlocks(context.Context, []types.BlockID) error
}

type layerPatrol interface {
	IsHareInCharge(types.LayerID) bool
}

type certHandler interface {
	HandleSyncedCertificate(context.Context, types.LayerID, *types.Certificate) error
}
