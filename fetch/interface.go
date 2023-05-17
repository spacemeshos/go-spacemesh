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

type MalfeasanceValidator interface {
	HandleSyncedMalfeasanceProof(context.Context, p2p.Peer, []byte) error
}

type AtxValidator interface {
	HandleAtxData(context.Context, p2p.Peer, []byte) error
}

type BlockValidator interface {
	HandleSyncedBlock(context.Context, p2p.Peer, []byte) error
}

type BallotValidator interface {
	HandleSyncedBallot(context.Context, p2p.Peer, []byte) error
}

type ProposalValidator interface {
	HandleSyncedProposal(context.Context, p2p.Peer, []byte) error
}

type TxValidator interface {
	HandleBlockTransaction(context.Context, p2p.Peer, []byte) error
	HandleProposalTransaction(context.Context, p2p.Peer, []byte) error
}

type PoetValidator interface {
	ValidateAndStoreMsg(context.Context, p2p.Peer, []byte) error
}

type meshProvider interface {
	LastVerified() types.LayerID
}

type host interface {
	ID() p2p.Peer
	GetPeers() []p2p.Peer
	Close() error
}
