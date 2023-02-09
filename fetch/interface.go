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

type malfeasanceHandler interface {
	HandleSyncedMalfeasanceProof(context.Context, p2p.Peer, []byte) error
}

type atxHandler interface {
	HandleAtxData(context.Context, p2p.Peer, []byte) error
}

type blockHandler interface {
	HandleSyncedBlock(context.Context, p2p.Peer, []byte) error
}

type ballotHandler interface {
	HandleSyncedBallot(context.Context, p2p.Peer, []byte) error
}

type proposalHandler interface {
	HandleSyncedProposal(context.Context, p2p.Peer, []byte) error
}

type txHandler interface {
	HandleBlockTransaction(context.Context, p2p.Peer, []byte) error
	HandleProposalTransaction(context.Context, p2p.Peer, []byte) error
}

type poetHandler interface {
	ValidateAndStoreMsg(context.Context, p2p.Peer, []byte) error
}

type meshProvider interface {
	LastVerified() types.LayerID
}

type host interface {
	GetPeers() []p2p.Peer
	Close() error
}
