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
	PollLayerData(context.Context, types.LayerID) chan fetch.LayerPromiseResult
	PollLayerOpinions(context.Context, types.LayerID) chan fetch.LayerPromiseResult
	GetEpochATXs(context.Context, types.EpochID) error
}

type layerPatrol interface {
	IsHareInCharge(types.LayerID) bool
}

type layerProcessor interface {
	ProcessLayer(context.Context, types.LayerID) error
}
