package rangesync

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

// RangeInfo contains information about a range of items in the OrderedSet as returned by
// OrderedSet.GetRangeInfo.
type RangeInfo struct {
	// Fingerprint of the interval
	Fingerprint Fingerprint
	// Number of items in the interval
	Count int
	// Items is the sequence of set elements in the interval.
	Items SeqResult
}

// SplitInfo contains information about range split in two.
type SplitInfo struct {
	// 2 parts of the range
	Parts [2]RangeInfo
	// Middle point between the ranges
	Middle KeyBytes
}

// OrderedSet represents the set that can be synced against a remote peer.
type OrderedSet interface {
	// Add adds a new key to the set.
	// It should not perform any additional actions related to handling
	// the received key.
	Add(k KeyBytes) error
	// Receive handles a new key received from the peer.
	// It should not add the key to the set.
	Receive(k KeyBytes) error
	// Received returns the sequence containing all the items received from the peer.
	Received() SeqResult
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
	GetRangeInfo(x, y KeyBytes) (RangeInfo, error)
	// SplitRange splits the range roughly after the specified count of items,
	// returning RangeInfo for the first half and the second half of the range.
	SplitRange(x, y KeyBytes, count int) (SplitInfo, error)
	// Items returns the sequence of items in the set.
	Items() SeqResult
	// Empty returns true if the set is empty.
	Empty() (bool, error)
	// Copy makes a shallow copy of the OrderedSet.
	// syncScope argument is a hint that can be used to optimize resource usage.
	// If syncScope is true, then the copy is intended to be used for the duration of
	// a synchronization run.
	// If syncScope if false, then the lifetime of the copy is not clearly defined.
	// The list of received items as returned by Received is also inherited by the copy.
	Copy(syncScope bool) OrderedSet
	// Recent returns an Iterator that yields the items added since the specified
	// timestamp. Some OrderedSet implementations may not have Recent implemented, in
	// which case it should return an empty sequence.
	Recent(since time.Time) (SeqResult, int)
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
	X() KeyBytes
	// Y returns the end of the range.
	Y() KeyBytes
	// Fingerprint returns the fingerprint of the range.
	Fingerprint() Fingerprint
	// Count returns the number of items in the range.
	Count() int
	// Keys returns the keys of the items in the range.
	Keys() []KeyBytes
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
