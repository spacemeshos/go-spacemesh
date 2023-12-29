package hashsync

import (
	"fmt"
	"reflect"
)

const (
	defaultMaxSendRange = 16
)

type RangeMessage struct {
	X, Y        Ordered
	Fingerprint any
	Count       int
	Items       []Ordered
}

func (m RangeMessage) String() string {
	itemsStr := ""
	if len(m.Items) != 0 {
		itemsStr = fmt.Sprintf(" +%d items", len(m.Items))
	}
	return fmt.Sprintf("<X %v Y %v Count %d Fingerprint %v%s>",
		m.X, m.Y, m.Count, m.Fingerprint, itemsStr)
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
	Next() Iterator
}

type RangeInfo struct {
	Fingerprint any
	Count       int
	Start, End  Iterator
}

type ItemStore interface {
	Add(v Ordered)
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

func (rsr *RangeSetReconciler) addItems(in []RangeMessage) {
	for _, msg := range in {
		for _, item := range msg.Items {
			rsr.is.Add(item)
		}
	}
}

func (rsr *RangeSetReconciler) getItems(start, end Iterator) []Ordered {
	var r []Ordered
	it := start
	for {
		r = append(r, it.Key())
		it = it.Next()
		if it == end {
			return r
		}
	}
}

func (rsr *RangeSetReconciler) processSubrange(preceding Iterator, x, y Ordered) (RangeMessage, Iterator) {
	if preceding != nil && preceding.Key().Compare(x) > 0 {
		preceding = nil
	}
	info := rsr.is.GetRangeInfo(preceding, x, y, -1)
	msg := RangeMessage{
		X:           x,
		Y:           y,
		Fingerprint: info.Fingerprint,
		Count:       info.Count,
	}
	// If the range is small enough, we send its contents
	if info.Count != 0 && info.Count <= rsr.maxSendRange {
		msg.Items = rsr.getItems(info.Start, info.End)
	}
	return msg, info.End
}

func (rsr *RangeSetReconciler) processFingerprint(preceding Iterator, msg RangeMessage) ([]RangeMessage, Iterator) {
	if msg.X == nil && msg.Y == nil {
		it := rsr.is.Min()
		if it == nil {
			return nil, nil
		}
		msg.X = it.Key()
		msg.Y = msg.X
	} else if msg.X == nil || msg.Y == nil {
		// TBD: don't pass just one nil when decoding!!!
		panic("invalid range")
	}
	info := rsr.is.GetRangeInfo(preceding, msg.X, msg.Y, -1)
	// fmt.Fprintf(os.Stderr, "msg %s fp %v start %#v end %#v count %d\n", msg, info.Fingerprint, info.Start, info.End, info.Count)
	switch {
	// FIXME: use Fingerprint interface for fingerprints
	// with Equal() method
	case reflect.DeepEqual(info.Fingerprint, msg.Fingerprint):
		// fmt.Fprintf(os.Stderr, "range synced: %s\n", msg)
		// the range is synced
		return nil, info.End
	case info.Count <= rsr.maxSendRange || msg.Count == 0:
		// The other side is missing some items, and either
		// range is small enough or empty on the other side
		resp := RangeMessage{
			X:           msg.X,
			Y:           msg.Y,
			Fingerprint: info.Fingerprint,
			Count:       info.Count,
		}
		if info.Count != 0 {
			resp.Items = rsr.getItems(info.Start, info.End)
		}
		// fmt.Fprintf(os.Stderr, "small/empty incoming range: %s -> %s\n", msg, resp)
		return []RangeMessage{resp}, info.End
	default:
		// Need to split the range.
		// Note that there's no special handling for rollover ranges with x >= y
		// These need to be handled by ItemStore.GetRangeInfo()
		count := info.Count / 2
		part := rsr.is.GetRangeInfo(preceding, msg.X, msg.Y, count)
		middle := part.End.Key()
		if middle == nil {
			panic("BUG: can't split range with count > 1")
		}
		msg1, next := rsr.processSubrange(info.Start, msg.X, middle)
		msg2, _ := rsr.processSubrange(next, middle, msg.Y)
		// fmt.Fprintf(os.Stderr, "normal: split X %s - middle %s - Y %s:\n  %s ->\n    %s\n    %s\n",
		// 	msg.X, middle, msg.Y, msg, msg1, msg2)
		return []RangeMessage{msg1, msg2}, info.End
	}
}

func (rsr *RangeSetReconciler) Initiate() RangeMessage {
	it := rsr.is.Min()
	if it == nil {
		// Create a message with count 0
		return RangeMessage{}
	}
	min := it.Key()
	info := rsr.is.GetRangeInfo(nil, min, min, -1)
	return RangeMessage{
		X:           min,
		Y:           min,
		Fingerprint: info.Fingerprint,
		Count:       info.Count,
	}
}

func (rsr *RangeSetReconciler) Process(in []RangeMessage) []RangeMessage {
	rsr.addItems(in)
	var out []RangeMessage
	for _, msg := range in {
		// TODO: need to sort ranges, but also need to be careful
		msgs, _ := rsr.processFingerprint(nil, msg)
		out = append(out, msgs...)
	}
	return out
}

// TBD: limit the number of rounds
// TBD: join adjacent ranges in the input
// TBD: process ascending ranges properly
// TBD: join adjacent ranges in the output
// TBD: bounded reconcile
