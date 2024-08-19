package hashsync

import (
	"context"
	"io"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

//go:generate mockgen -typed -package=hashsync -destination=./mocks_test.go -source=./interface.go

// Iterator points to in item in ItemStore
type Iterator interface {
	// Key returns the key corresponding to iterator position. It returns
	// nil if the ItemStore is empty
	// If the iterator is returned along with a count, the return value of Key()
	// after calling Next() count times is dependent on the implementation.
	Key() (Ordered, error)
	// Next advances the iterator
	Next() error
	// Clone returns a copy of the iterator
	Clone() Iterator
}

// RangeInfo contains information about a range of items in the ItemStore as returned by
// ItemStore.GetRangeInfo.
type RangeInfo struct {
	// Fingerprint of the interval
	Fingerprint any
	// Number of items in the interval
	Count int
	// An iterator pointing to the beginning of the interval or nil if count is zero.
	Start Iterator
	// An iterator pointing to the end of the interval or nil if count is zero.
	End Iterator
}

// SplitInfo contains information about range split in two.
type SplitInfo struct {
	// 2 parts of the range
	Parts [2]RangeInfo
	// Middle point between the ranges
	Middle Ordered
}

// ItemStore represents the data store that can be synced against a remote peer
type ItemStore interface {
	// Add adds a key to the store
	Add(ctx context.Context, k Ordered) error
	// GetRangeInfo returns RangeInfo for the item range in the tree.
	// If count >= 0, at most count items are returned, and RangeInfo
	// is returned for the corresponding subrange of the requested range.
	// If both x and y is nil, the whole set of items is used.
	// If only x or only y is nil, GetRangeInfo panics
	GetRangeInfo(ctx context.Context, preceding Iterator, x, y Ordered, count int) (RangeInfo, error)
	// SplitRange splits the range roughly after the specified count of items,
	// returning RangeInfo for the first half and the second half of the range.
	SplitRange(ctx context.Context, preceding Iterator, x, y Ordered, count int) (SplitInfo, error)
	// Min returns the iterator pointing at the minimum element
	// in the store. If the store is empty, it returns nil
	Min(ctx context.Context) (Iterator, error)
	// Copy makes a shallow copy of the ItemStore
	Copy() ItemStore
	// Has returns true if the specified key is present in ItemStore
	Has(ctx context.Context, k Ordered) (bool, error)
}

type Requester interface {
	Run(context.Context) error
	StreamRequest(context.Context, p2p.Peer, []byte, server.StreamRequestCallback, ...string) error
}

type SyncBase interface {
	Count(ctx context.Context) (int, error)
	Derive(p p2p.Peer) Syncer
	Probe(ctx context.Context, p p2p.Peer) (ProbeResult, error)
	Wait() error
}

type Syncer interface {
	Peer() p2p.Peer
	Sync(ctx context.Context, x, y *types.Hash32) error
	Serve(ctx context.Context, req []byte, stream io.ReadWriter) error
}

type PairwiseSyncer interface {
	Probe(ctx context.Context, peer p2p.Peer, is ItemStore, x, y *types.Hash32) (ProbeResult, error)
	SyncStore(ctx context.Context, peer p2p.Peer, is ItemStore, x, y *types.Hash32) error
	Serve(ctx context.Context, req []byte, stream io.ReadWriter, is ItemStore) error
}

type syncRunner interface {
	splitSync(ctx context.Context, syncPeers []p2p.Peer) error
	fullSync(ctx context.Context, syncPeers []p2p.Peer) error
}
