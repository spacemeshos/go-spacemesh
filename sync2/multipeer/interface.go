package multipeer

import (
	"context"
	"io"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

//go:generate mockgen -typed -package=multipeer_test -destination=./mocks_test.go -source=./interface.go

// SyncBase is a synchronization base which holds the original OrderedSet.
// It is used to derive per-peer PeerSyncers with their own copies of the OrderedSet,
// copy operation being O(1) in terms of memory and time complexity.
// It can also probe peers to decide on the synchronization strategy.
type SyncBase interface {
	// Count returns the number of items in the set.
	Count() (int, error)
	// WithPeerSyncer creates a Syncer for the specified peer and passes it to the specified function.
	// When the function returns, the syncer is discarded, releasing the resources associated with it.
	WithPeerSyncer(ctx context.Context, p p2p.Peer, toCall func(PeerSyncer) error) error
	// Probe probes the specified peer, obtaining its set fingerprint,
	// the number of items and the similarity value.
	Probe(ctx context.Context, p p2p.Peer) (rangesync.ProbeResult, error)
	// Wait waits for all the derived syncers' handlers to finish.
	Wait() error
}

// PeerSyncer is a synchronization interface for a single peer.
type PeerSyncer interface {
	// Peer returns the peer this syncer is for.
	Peer() p2p.Peer
	// Sync synchronizes the set with the peer.
	Sync(ctx context.Context, x, y rangesync.KeyBytes) error
	// Serve serves a synchronization request on the specified stream.
	Serve(ctx context.Context, stream io.ReadWriter) error
}

// SyncKeyHandler is a handler for keys that are received from peers.
type SyncKeyHandler interface {
	// Receive handles a key that was received from a peer.
	Receive(k rangesync.KeyBytes, peer p2p.Peer) (bool, error)
	// Commit is invoked at the end of synchronization to apply the changes.
	Commit(ctx context.Context, peer p2p.Peer, base, new rangesync.OrderedSet) error
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
