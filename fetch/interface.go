package fetch

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type requester interface {
	Request(context.Context, p2p.Peer, []byte, func([]byte), func(error)) error
}

type atxHandler interface {
	HandleAtxData(context.Context, []byte) error
}

type blockHandler interface {
	HandleSyncedBlock(context.Context, []byte) error
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
	LastVerified() types.LayerID
}

type host interface {
	GetPeers() []p2p.Peer
	Close() error
}
