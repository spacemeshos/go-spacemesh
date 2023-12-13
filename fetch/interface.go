package fetch

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type requester interface {
	Request(context.Context, p2p.Peer, []byte, func([]byte), func(error)) error
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

type meshProvider interface {
	LastVerified() types.LayerID
}

type host interface {
	ID() p2p.Peer
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) <-chan struct{}
	CurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}
