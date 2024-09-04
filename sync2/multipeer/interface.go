package multipeer

import (
	"context"
	"io"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

//go:generate mockgen -typed -package=multipeer -destination=./mocks_test.go -source=./interface.go

type SyncBase interface {
	Count(ctx context.Context) (int, error)
	Derive(p p2p.Peer) Syncer
	Probe(ctx context.Context, p p2p.Peer) (rangesync.ProbeResult, error)
	Wait() error
}

type Syncer interface {
	Peer() p2p.Peer
	Sync(ctx context.Context, x, y types.KeyBytes) error
	Serve(ctx context.Context, req []byte, stream io.ReadWriter) error
}

type PairwiseSyncer interface {
	Probe(
		ctx context.Context,
		peer p2p.Peer,
		os rangesync.OrderedSet,
		x, y types.KeyBytes,
	) (rangesync.ProbeResult, error)
	Sync(
		ctx context.Context,
		peer p2p.Peer,
		os rangesync.OrderedSet,
		x, y types.KeyBytes,
	) error
	Serve(ctx context.Context, req []byte, stream io.ReadWriter, os rangesync.OrderedSet) error
}

type syncRunner interface {
	splitSync(ctx context.Context, syncPeers []p2p.Peer) error
	fullSync(ctx context.Context, syncPeers []p2p.Peer) error
}
