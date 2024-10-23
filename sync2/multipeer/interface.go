package multipeer

import (
	"context"
	"io"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

//go:generate mockgen -typed -package=multipeer_test -destination=./mocks_test.go -source=./interface.go

// OrdredSet is an interface for a set that can be synced against a remote peer.
// It extends rangesync.OrderedSet with methods which are needed for multi-peer
// reconciliation.
type OrderedSet interface {
	rangesync.OrderedSet
	// EnsureLoaded ensures that the set is loaded and ready for use.
	// It may do nothing in case of in-memory sets, but may trigger loading
	// from database in case of database-backed sets.
	EnsureLoaded() error
	// Advance advances the set by including the items since the set was last loaded
	// or advanced.
	Advance() error
	// Has returns true if the specified key is present in OrderedSet.
	Has(rangesync.KeyBytes) (bool, error)
	// Release releases the resources associated with the set.
	// Calling Release on a set that is already released is a no-op.
	Release() error
}

// SyncBase is a synchronization base which holds the original OrderedSet.
type SyncBase interface {
	// Count returns the number of items in the set.
	Count() (int, error)
	// Derive creates a Syncer for the specified peer.
	Derive(p p2p.Peer) Syncer
	// Probe probes the specified peer, obtaining its set fingerprint,
	// the number of items and the similarity value.
	Probe(ctx context.Context, p p2p.Peer) (rangesync.ProbeResult, error)
	// Wait waits for all the derived syncers' handlers to finish.
	Wait() error
}

// Syncer is a synchronization interface for a single peer.
type Syncer interface {
	// Peer returns the peer this syncer is for.
	Peer() p2p.Peer
	// Sync synchronizes the set with the peer.
	Sync(ctx context.Context, x, y rangesync.KeyBytes) error
	// Serve serves a synchronization request on the specified stream.
	Serve(ctx context.Context, stream io.ReadWriter) error
	// Release releases the resources associated with the syncer.
	// Calling Release on a syncer that is already released is a no-op.
	Release() error
}

// SyncKeyHandler is a handler for keys that are received from peers.
type SyncKeyHandler interface {
	// Receive handles a key that was received from a peer.
	Receive(k rangesync.KeyBytes, peer p2p.Peer) (bool, error)
	// Commit is invoked at the end of synchronization to apply the changes.
	Commit(peer p2p.Peer, base, new OrderedSet) error
}

type PairwiseSyncer interface {
	Probe(
		ctx context.Context,
		peer p2p.Peer,
		os rangesync.OrderedSet,
		x, y rangesync.KeyBytes,
	) (rangesync.ProbeResult, error)
	Sync(
		ctx context.Context,
		peer p2p.Peer,
		os rangesync.OrderedSet,
		x, y rangesync.KeyBytes,
	) error
	Serve(context context.Context, stream io.ReadWriter, os rangesync.OrderedSet) error
}

type syncRunner interface {
	SplitSync(ctx context.Context, syncPeers []p2p.Peer) error
	FullSync(ctx context.Context, syncPeers []p2p.Peer) error
}
