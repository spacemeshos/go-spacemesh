package rangesync

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./interface.go

// RangeInfo contains information about a range of items in the OrderedSet as returned by
// OrderedSet.GetRangeInfo.
type RangeInfo struct {
	// Fingerprint of the interval
	Fingerprint types.Fingerprint
	// Number of items in the interval
	Count int
	// Items is the sequence of set elements in the interval.
	Items types.Seq
}

// SplitInfo contains information about range split in two.
type SplitInfo struct {
	// 2 parts of the range
	Parts [2]RangeInfo
	// Middle point between the ranges
	Middle types.Ordered
}

// OrderedSet represents the set that can be synced against a remote peer
type OrderedSet interface {
	// Add adds a key to the set
	Add(ctx context.Context, k types.Ordered) error
	// GetRangeInfo returns RangeInfo for the item range in the tree.
	// If count >= 0, at most count items are returned, and RangeInfo
	// is returned for the corresponding subrange of the requested range.
	// X and Y must not be nil.
	GetRangeInfo(ctx context.Context, x, y types.Ordered, count int) (RangeInfo, error)
	// SplitRange splits the range roughly after the specified count of items,
	// returning RangeInfo for the first half and the second half of the range.
	SplitRange(ctx context.Context, x, y types.Ordered, count int) (SplitInfo, error)
	// Items returns the sequence of items in the set.
	Items(ctx context.Context) (types.Seq, error)
	// Empty returns true if the set is empty.
	Empty(ctx context.Context) (bool, error)
	// Copy makes a shallow copy of the OrderedSet
	Copy() OrderedSet
	// Has returns true if the specified key is present in OrderedSet
	Has(ctx context.Context, k types.Ordered) (bool, error)
	// Recent returns an Iterator that yields the items added since the specified
	// timestamp. Some OrderedSet implementations may not have Recent implemented, in
	// which case it should return an error.
	Recent(ctx context.Context, since time.Time) (types.Seq, int, error)
}

type Requester interface {
	Run(context.Context) error
	StreamRequest(context.Context, p2p.Peer, []byte, server.StreamRequestCallback, ...string) error
}
