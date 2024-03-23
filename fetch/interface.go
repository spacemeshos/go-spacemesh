package fetch

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type requester interface {
	Run(context.Context) error
	Request(context.Context, p2p.Peer, []byte, ...string) ([]byte, error)
	StreamRequest(context.Context, p2p.Peer, []byte, server.StreamRequestCallback, ...string) error
}

// The ValidatorFunc type is an adapter to allow the use of functions as
// SyncValidators so that we can mock the behavior of GossipHandlers. If we
// didn't need to mock GossipHandler behavior then we could use GossipHandlers
// directly and do away with both ValidatorFunc and SyncValidator.
type ValidatorFunc pubsub.SyncHandler

func (f ValidatorFunc) HandleMessage(
	ctx context.Context,
	hash types.Hash32,
	peer p2p.Peer,
	msg []byte,
) error {
	return f(ctx, hash, peer, msg)
}

// SyncValidator exists to allow for mocking of GossipHandlers through the use
// of ValidatorFunc.
type SyncValidator interface {
	HandleMessage(context.Context, types.Hash32, p2p.Peer, []byte) error
}

type PoetValidator interface {
	ValidateAndStoreMsg(context.Context, types.Hash32, p2p.Peer, []byte) error
}

type host interface {
	ID() p2p.Peer
}
