package syncer

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/p2p"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/layerfetcher"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type layerTicker interface {
	GetCurrentLayer() types.LayerID
}

type layerFetcher interface {
	PollLayerContent(context.Context, types.LayerID) chan layerfetcher.LayerPromiseResult
	GetEpochATXs(context.Context, p2p.Peer, types.EpochID) error
}

type layerPatrol interface {
	IsHareInCharge(types.LayerID) bool
}

type layerProcessor interface {
	ProcessLayer(context.Context, types.LayerID) error
}
