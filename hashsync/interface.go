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

type syncBase interface {
	count() int
	derive(p p2p.Peer) syncer
	probe(ctx context.Context, p p2p.Peer) (ProbeResult, error)
	run(ctx context.Context) error
}

type syncer interface {
	peer() p2p.Peer
	sync(ctx context.Context, x, y *types.Hash32) error
}

type syncRunner interface {
	splitSync(ctx context.Context, syncPeers []p2p.Peer) error
	fullSync(ctx context.Context, syncPeers []p2p.Peer) error
}
