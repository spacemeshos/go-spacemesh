package syncer

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type layerTicker interface {
	GetCurrentLayer() types.LayerID
}

type meshProvider interface {
	SetZeroBlockLayer(context.Context, types.LayerID)
}

type dataFetcher interface {
	PollLayerData(context.Context, types.LayerID) error
	PollLayerOpinions(context.Context, types.LayerID) ([]*fetch.LayerOpinion, error)
	GetEpochATXs(context.Context, types.EpochID) error
	GetBlocks(context.Context, []types.BlockID) error
}

type fetcher interface {
	GetEpochATXIDs(context.Context, p2p.Peer, types.EpochID, func([]byte), func(error)) error
	GetLayerData(context.Context, []p2p.Peer, types.LayerID, func([]byte, p2p.Peer), func(error, p2p.Peer)) error
	GetLayerOpinions(context.Context, []p2p.Peer, types.LayerID, func([]byte, p2p.Peer), func(error, p2p.Peer)) error

	GetAtxs(context.Context, []types.ATXID) error
	GetBallots(context.Context, []types.BallotID) error
	GetBlocks(context.Context, []types.BlockID) error
	RegisterPeerHashes(peer p2p.Peer, hashes []types.Hash32)

	GetPeers() []p2p.Peer
}

type layerPatrol interface {
	IsHareInCharge(types.LayerID) bool
}

type certHandler interface {
	HandleSyncedCertificate(context.Context, types.LayerID, *types.Certificate) error
}
