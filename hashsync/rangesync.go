package hashsync

import (
	"reflect"
)

const (
	defaultMaxSendRange = 16
)

type SyncMessage interface {
	X() Ordered
	Y() Ordered
	Fingerprint() any
	Count() int
	HaveItems() bool
}

// Conduit handles receiving and sending peer messages
type Conduit interface {
	// NextMessage returns the next SyncMessage, or nil if there
	// are no more SyncMessages.
	NextMessage() (SyncMessage, error)
	// NextItem returns the next item in the set or nil if there
	// are no more items
	NextItem() (Ordered, error)
	// SendFingerprint sends range fingerprint to the peer.
	// Count must be > 0
	SendFingerprint(x, y Ordered, fingerprint any, count int)
	// SendEmptySet notifies the peer that it we don't have any items.
	// The corresponding SyncMessage has Count() == 0, X() == nil and Y() == nil
	SendEmptySet()
	// SendEmptyRange notifies the peer that the specified range
	// is empty on our side. The corresponding SyncMessage has Count() == 0
	SendEmptyRange(x, y Ordered)
	// SendItems sends the local items to the peer, requesting back
	// the items peer has in that range. The corresponding
	// SyncMessage has HaveItems() == true
	SendItems(x, y Ordered, count int, it Iterator)
	// SendItemsOnly sends just items without any message
	SendItemsOnly(count int, it Iterator)
}

type Option func(r *RangeSetReconciler)

func WithMaxSendRange(n int) Option {
	return func(r *RangeSetReconciler) {
		r.maxSendRange = n
	}
}

// Iterator points to in item in ItemStore
type Iterator interface {
	// Equal returns true if this iterator is equal to another Iterator
	Equal(other Iterator) bool
	// Key returns the key corresponding to iterator
	Key() Ordered
	// Next returns an iterator pointing to the next key or nil
	// if this key is the last one in the store
	Next()
}

type RangeInfo struct {
	Fingerprint any
	Count       int
	Start, End  Iterator
}

type ItemStore interface {
	// Add adds a key to the store
	Add(k Ordered)
	// GetRangeInfo returns RangeInfo for the item range in the tree.
	// If count >= 0, at most count items are returned, and RangeInfo
	// is returned for the corresponding subrange of the requested range
	GetRangeInfo(preceding Iterator, x, y Ordered, count int) RangeInfo
	// Min returns the iterator pointing at the minimum element
	// in the store. If the store is empty, it returns nil
	Min() Iterator
	// Max returns the iterator pointing at the maximum element
	// in the store. If the store is empty, it returns nil
	Max() Iterator
}

type RangeSetReconciler struct {
	is           ItemStore
	maxSendRange int
}

func NewRangeSetReconciler(is ItemStore, opts ...Option) *RangeSetReconciler {
	rsr := &RangeSetReconciler{
		is:           is,
		maxSendRange: defaultMaxSendRange,
	}
	for _, opt := range opts {
		opt(rsr)
	}
	if rsr.maxSendRange <= 0 {
		panic("bad maxSendRange")
	}
	return rsr
}

func (rsr *RangeSetReconciler) addItems(c Conduit) error {
	for {
		item, err := c.NextItem()
		if err != nil {
			return err
		}
		if item == nil {
			return nil
		}
		rsr.is.Add(item)
	}
}

// func qqqqRmmeK(it Iterator) any {
// 	if it == nil {
// 		return "<nil>"
// 	}
// 	if it.Key() == nil {
// 		return "<nilkey>"
// 	}
// 	return fmt.Sprintf("%s", it.Key())
// }

func (rsr *RangeSetReconciler) processSubrange(c Conduit, preceding, start, end Iterator, x, y Ordered) Iterator {
	if preceding != nil && preceding.Key().Compare(x) > 0 {
		preceding = nil
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: preceding=%q\n",
	// 	qqqqRmmeK(preceding))
	info := rsr.is.GetRangeInfo(preceding, x, y, -1)
	// fmt.Fprintf(os.Stderr, "QQQQQ: start=%q end=%q info.Start=%q info.End=%q info.FP=%q x=%q y=%q\n",
	// 	qqqqRmmeK(start), qqqqRmmeK(end), qqqqRmmeK(info.Start), qqqqRmmeK(info.End), info.Fingerprint, x, y)
	switch {
	// case info.Count != 0 && info.Count <= rsr.maxSendRange:
	// 	// If the range is small enough, we send its contents.
	// 	// The peer may have more items of its own in that range,
	// 	// so we can't use SendItemsOnly(), instead we use SendItems,
	// 	// which includes our items and asks the peer to send any
	// 	// items it has in the range.
	// 	c.SendItems(x, y, info.Count, info.Start)
	case info.Count == 0:
		// We have no more items in this subrange.
		// Ask peer to send any items it has in the range
		c.SendEmptyRange(x, y)
	default:
		// The range is non-empty and large enough.
		// Send fingerprint so that the peer can further subdivide it.
		c.SendFingerprint(x, y, info.Fingerprint, info.Count)
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: info.End=%q\n", qqqqRmmeK(info.End))
	return info.End
}

func (rsr *RangeSetReconciler) handleMessage(c Conduit, preceding Iterator, msg SyncMessage) Iterator {
	x := msg.X()
	y := msg.Y()
	if x == nil && y == nil {
		// The peer has no items at all so didn't
		// even send X & Y (SendEmptySet)
		it := rsr.is.Min()
		if it == nil {
			// We don't have any items at all, too
			return nil
		}
		x = it.Key()
		y = x
	} else if x == nil || y == nil {
		// TBD: never pass just one nil when decoding!!!
		panic("invalid range")
	}
	info := rsr.is.GetRangeInfo(preceding, x, y, -1)
	// fmt.Fprintf(os.Stderr, "msg %s fp %v start %#v end %#v count %d\n", msg, info.Fingerprint, info.Start, info.End, info.Count)
	switch {
	case msg.HaveItems() || msg.Count() == 0:
		// The peer has no more items to send in this range after this
		// message, as it is either empty or it has sent all of its
		// items in the range to us, but there may be some items on our
		// side. In the latter case, send only the items themselves b/c
		// the range doesn't need any further handling by the peer.
		if info.Count != 0 {
			c.SendItemsOnly(info.Count, info.Start)
		}
	case fingerprintEqual(info.Fingerprint, msg.Fingerprint()):
		// The range is synced
	// case (info.Count+1)/2 <= rsr.maxSendRange:
	case info.Count <= rsr.maxSendRange:
		// The range differs from the peer's version of it, but the it
		// is small enough (or would be small enough after split) or
		// empty on our side
		if info.Count != 0 {
			// fmt.Fprintf(os.Stderr, "small incoming range: %s -> SendItems\n", msg)
			c.SendItems(x, y, info.Count, info.Start)
		} else {
			// fmt.Fprintf(os.Stderr, "small incoming range: %s -> empty range msg\n", msg)
			c.SendEmptyRange(x, y)
		}
	default:
		// Need to split the range.
		// Note that there's no special handling for rollover ranges with x >= y
		// These need to be handled by ItemStore.GetRangeInfo()
		count := (info.Count + 1) / 2
		part := rsr.is.GetRangeInfo(preceding, x, y, count)
		if part.End == nil {
			panic("BUG: can't split range with count > 1")
		}
		middle := part.End.Key()
		next := rsr.processSubrange(c, info.Start, part.Start, part.End, x, middle)
		// fmt.Fprintf(os.Stderr, "QQQQQ: next=%q\n", qqqqRmmeK(next))
		rsr.processSubrange(c, next, part.End, info.End, middle, y)
		// fmt.Fprintf(os.Stderr, "normal: split X %s - middle %s - Y %s:\n  %s",
		// 	msg.X(), middle, msg.Y(), msg)
	}
	return info.End
}

func (rsr *RangeSetReconciler) Initiate(c Conduit) {
	it := rsr.is.Min()
	if it == nil {
		c.SendEmptySet()
		return
	}
	min := it.Key()
	info := rsr.is.GetRangeInfo(nil, min, min, -1)
	if info.Count != 0 && info.Count < rsr.maxSendRange {
		c.SendItems(min, min, info.Count, info.Start)
	} else {
		c.SendFingerprint(min, min, info.Fingerprint, info.Count)
	}
}

func (rsr *RangeSetReconciler) Process(c Conduit) error {
	var msgs []SyncMessage
	for {
		msg, err := c.NextMessage()
		if err != nil {
			return err
		}
		if msg == nil {
			break
		}
		msgs = append(msgs, msg)
	}

	if err := rsr.addItems(c); err != nil {
		return err
	}

	for _, msg := range msgs {
		// TODO: need to sort the ranges, but also need to be careful
		rsr.handleMessage(c, nil, msg)
	}

	return nil
}

func fingerprintEqual(a, b any) bool {
	// FIXME: use Fingerprint interface with Equal() method for fingerprints
	// but still allow nil fingerprints
	return reflect.DeepEqual(a, b)
}

// TBD: successive messages with payloads can be combined!
// TBD: limit the number of rounds (outside RangeSetReconciler)
// TBD: process ascending ranges properly
// TBD: bounded reconcile
// TBD: limit max N of received unconfirmed items
// TBD: streaming sync with sequence numbers or timestamps
// TBD: never pass just one of X and Y as nil when decoding the messages!!!
