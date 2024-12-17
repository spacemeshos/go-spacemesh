package multipeer

import (
	"context"
	"io"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

//go:generate mockgen -typed -package=multipeer_test -destination=./mocks_test.go -source=./interface.go

// SyncBase is a synchronization base which holds the original OrderedSet.
// It is used to sync against peers using derived OrderedSets.
// It can also probe peers to decide on the synchronization strategy.
type SyncBase interface {
	// Count returns the number of items in the set.
	Count() (int, error)
	// Sync synchronizes the set with the peer.
	// It returns a sequence of new keys that were received from the peer and the
	// number of received items.
	Sync(ctx context.Context, p p2p.Peer, x, y rangesync.KeyBytes) error
	// Serve serves a synchronization request on the specified stream.
	// It returns a sequence of new keys that were received from the peer and the
	// number of received items.
	Serve(ctx context.Context, p p2p.Peer, stream io.ReadWriter) error
	// Probe probes the specified peer, obtaining its set fingerprint,
	// the number of items and the similarity value.
	Probe(ctx context.Context, p p2p.Peer) (rangesync.ProbeResult, error)
}

// SyncKeyHandler is a handler for keys that are received from peers.
type SyncKeyHandler interface {
	// Commit is invoked at the end of synchronization to apply the changes.
	Commit(ctx context.Context, peer p2p.Peer, base rangesync.OrderedSet, received rangesync.SeqResult) error
}

// PairwiseSyncer is used to probe a peer or sync against a single peer.
// It does not contain a copy of the set.
type PairwiseSyncer interface {
	// Probe probes the peer using the specified range, to check how different the
	// peer's set is from the local set.
	Probe(
		ctx context.Context,
		peer p2p.Peer,
		os rangesync.OrderedSet,
		x, y rangesync.KeyBytes,
	) (rangesync.ProbeResult, error)
	// Sync synchronizes the set with the peer using the specified range.
	Sync(
		ctx context.Context,
		peer p2p.Peer,
		os rangesync.OrderedSet,
		x, y rangesync.KeyBytes,
	) error
	// Serve serves an incoming synchronization request.
	Serve(context context.Context, stream io.ReadWriter, os rangesync.OrderedSet) error
}

type syncRunner interface {
	SplitSync(ctx context.Context, syncPeers []p2p.Peer) error
	FullSync(ctx context.Context, syncPeers []p2p.Peer) error
}
