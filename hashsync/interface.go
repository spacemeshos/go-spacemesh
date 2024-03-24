package hashsync

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

//go:generate mockgen -typed -package=hashsync -destination=./mocks_test.go -source=./interface.go

type requester interface {
	Run(context.Context) error
	StreamRequest(context.Context, p2p.Peer, []byte, server.StreamRequestCallback, ...string) error
}

type peerSet interface {
	addPeer(p p2p.Peer)
	removePeer(p p2p.Peer)
	numPeers() int
	listPeers() []p2p.Peer
	havePeer(p p2p.Peer) bool
}

type syncBase interface {
	derive(p p2p.Peer) syncer
	probe(ctx context.Context, p p2p.Peer) (int, error)
}

type syncer interface {
	peer() p2p.Peer
	sync(ctx context.Context, x, y *types.Hash32) error
}
