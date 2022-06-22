package fetch

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	ftypes "github.com/spacemeshos/go-spacemesh/fetch/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/bootstrap"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type fetcher interface {
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
	HandleBlockData(context.Context, []byte) error
}

type ballotHandler interface {
	HandleBallotData(context.Context, []byte) error
}

type proposalHandler interface {
	HandleProposalData(context.Context, []byte) error
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
	SetZeroBlockLayer(types.LayerID) error
}

type network interface {
	bootstrap.Provider
	server.Host
}
