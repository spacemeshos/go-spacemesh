package rangesync

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

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
	Middle types.KeyBytes
}

// OrderedSet represents the set that can be synced against a remote peer.
type OrderedSet interface {
	// Add adds a key to the set.
	Add(ctx context.Context, k types.KeyBytes) error
	// GetRangeInfo returns RangeInfo for the item range in the ordered set,
	// bounded by [x, y).
	// x == y indicates the whole set.
	// x < y indicates a normal range starting with x and ending below y.
	// x > y indicates a wrapped around range, that is from x (inclusive) to then end
	// of the set and from the beginning of the set to y, non-inclusive.
	// If count >= 0, at most count items are returned, and RangeInfo
	// is returned for the corresponding subrange of the requested range.
	// If both x and y are nil, the information for the entire set is returned.
	// If any of x or y is nil, the other one must be nil as well.
	GetRangeInfo(ctx context.Context, x, y types.KeyBytes, count int) (RangeInfo, error)
	// SplitRange splits the range roughly after the specified count of items,
	// returning RangeInfo for the first half and the second half of the range.
	SplitRange(ctx context.Context, x, y types.KeyBytes, count int) (SplitInfo, error)
	// Items returns the sequence of items in the set.
	Items(ctx context.Context) (types.Seq, error)
	// Empty returns true if the set is empty.
	Empty(ctx context.Context) (bool, error)
	// Copy makes a shallow copy of the OrderedSet.
	Copy() OrderedSet
	// Recent returns an Iterator that yields the items added since the specified
	// timestamp. Some OrderedSet implementations may not have Recent implemented, in
	// which case it should return an error.
	Recent(ctx context.Context, since time.Time) (types.Seq, int, error)
}

type Requester interface {
	Run(context.Context) error
	StreamRequest(context.Context, p2p.Peer, []byte, server.StreamRequestCallback, ...string) error
}

// SyncMessage is a message that is a part of the sync protocol.
type SyncMessage interface {
	// Type returns the type of the message.
	Type() MessageType
	// X returns the beginning of the range.
	X() types.KeyBytes
	// Y returns the end of the range.
	Y() types.KeyBytes
	// Fingerprint returns the fingerprint of the range.
	Fingerprint() types.Fingerprint
	// Count returns the number of items in the range.
	Count() int
	// Keys returns the keys of the items in the range.
	Keys() []types.KeyBytes
	// Since returns the time since when the recent items are being sent.
	Since() time.Time
	// Sample returns the minhash sample of the items in the range.
	Sample() []MinhashSampleItem
}

// Conduit handles receiving and sending peer messages.
type Conduit interface {
	// NextMessage returns the next SyncMessage, or nil if there are no more
	// SyncMessages for this session.
	NextMessage() (SyncMessage, error)
	// Send sends a SyncMessage to the peer.
	Send(SyncMessage) error
}
