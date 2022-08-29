package fetch

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	ftypes "github.com/spacemeshos/go-spacemesh/fetch/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type fetcher interface {
	GetEpochATXIDs(context.Context, types.EpochID, func([]byte, p2p.Peer), func(error)) error
	GetLayerData(context.Context, types.LayerID, func([]byte, p2p.Peer, int), func(error, p2p.Peer, int)) error
	GetLayerOpinions(context.Context, types.LayerID, func([]byte, p2p.Peer, int), func(error, p2p.Peer, int)) error
	GetHash(types.Hash32, datastore.Hint, bool) chan ftypes.HashDataPromiseResult
	GetHashes([]types.Hash32, datastore.Hint, bool) map[types.Hash32]chan ftypes.HashDataPromiseResult
	Stop()
	Start()
	RegisterPeerHashes(p2p.Peer, []types.Hash32)
	AddPeersFromHash(types.Hash32, []types.Hash32)
}

type atxHandler interface {
	HandleAtxData(context.Context, []byte) error
}

type blockHandler interface {
	HandleSyncedBlock(context.Context, []byte) error
}

type certHandler interface {
	HandleSyncedCertificate(context.Context, types.LayerID, *types.Certificate) error
}

type ballotHandler interface {
	HandleSyncedBallot(context.Context, []byte) error
}

type proposalHandler interface {
	HandleSyncedProposal(context.Context, []byte) error
}

type txHandler interface {
	HandleBlockTransaction(context.Context, []byte) error
	HandleProposalTransaction(context.Context, []byte) error
}

type poetHandler interface {
	ValidateAndStoreMsg(data []byte) error
}

type meshProvider interface {
	ProcessedLayer() types.LayerID
	SetZeroBlockLayer(context.Context, types.LayerID) error
}

type host interface {
	GetPeers() []p2p.Peer
	Close() error
}
