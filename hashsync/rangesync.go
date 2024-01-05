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

type Conduit interface {
	// NextMessage returns the next SyncMessage, or nil if there
	// are no more SyncMessages.
	NextMessage() (SyncMessage, error)
	// NextItem returns the next item in the set or nil if there
	// are no more items
	NextItem() (Ordered, error)
	// SendFingerprint sends range fingerprint to the peer
	SendFingerprint(x, y Ordered, fingerprint any, count int)
	// SendItems sends range fingerprint to the peer along with the items
	SendItems(x, y Ordered, fingerprint any, count int, start, end Iterator)
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
	if rsr.maxSendRange == 0 {
		panic("zero maxSendRange")
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

func (rsr *RangeSetReconciler) processSubrange(c Conduit, preceding, start, end Iterator, x, y Ordered) Iterator {
	if preceding != nil && preceding.Key().Compare(x) > 0 {
		preceding = nil
	}
	info := rsr.is.GetRangeInfo(preceding, x, y, -1)
	// If the range is small enough, we send its contents
	if info.Count != 0 && info.Count <= rsr.maxSendRange {
		c.SendItems(x, y, info.Fingerprint, info.Count, start, end)
	} else {
		c.SendFingerprint(x, y, info.Fingerprint, info.Count)
	}
	return info.End
}

func (rsr *RangeSetReconciler) processFingerprint(c Conduit, preceding Iterator, msg SyncMessage) Iterator {
	x := msg.X()
	y := msg.Y()
	if x == nil && y == nil {
		// The peer has no items at all so didn't
		// even send X & Y
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
	// FIXME: use Fingerprint interface for fingerprints
	// with Equal() method
	case reflect.DeepEqual(info.Fingerprint, msg.Fingerprint()):
		// fmt.Fprintf(os.Stderr, "range synced: %s\n", msg)
		// the range is synced
		return info.End
	case info.Count <= rsr.maxSendRange || msg.Count() == 0:
		// The other side is missing some items, and either
		// range is small enough or empty on the other side
		if info.Count != 0 {
			// fmt.Fprintf(os.Stderr, "small/empty incoming range: %s -> SendItems\n", msg)
			c.SendItems(x, y, info.Fingerprint, info.Count, info.Start, info.End)
		} else {
			// fmt.Fprintf(os.Stderr, "small/empty incoming range: %s -> zero count msg\n", msg)
			c.SendFingerprint(x, y, info.Fingerprint, info.Count)
		}
		return info.End
	default:
		// Need to split the range.
		// Note that there's no special handling for rollover ranges with x >= y
		// These need to be handled by ItemStore.GetRangeInfo()
		count := info.Count / 2
		part := rsr.is.GetRangeInfo(preceding, x, y, count)
		if part.End == nil {
			panic("BUG: can't split range with count > 1")
		}
		middle := part.End.Key()
		next := rsr.processSubrange(c, info.Start, part.Start, part.End, x, middle)
		rsr.processSubrange(c, next, part.End, info.End, middle, y)
		// fmt.Fprintf(os.Stderr, "normal: split X %s - middle %s - Y %s:\n  %s",
		// 	msg.X(), middle, msg.Y(), msg)
		return info.End
	}
}

func (rsr *RangeSetReconciler) Initiate(c Conduit) {
	it := rsr.is.Min()
	if it == nil {
		// Create a message with count 0
		c.SendFingerprint(nil, nil, nil, 0)
		return
	}
	min := it.Key()
	info := rsr.is.GetRangeInfo(nil, min, min, -1)
	if info.Count != 0 && info.Count < rsr.maxSendRange {
		c.SendItems(min, min, info.Fingerprint, info.Count, info.Start, info.End)
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
		// TODO: need to sort ranges, but also need to be careful
		rsr.processFingerprint(c, nil, msg)
	}

	return nil
}

// TBD: limit the number of rounds (outside RangeSetReconciler)
// TBD: process ascending ranges properly
// TBD: bounded reconcile
// TBD: limit max N of received unconfirmed items
