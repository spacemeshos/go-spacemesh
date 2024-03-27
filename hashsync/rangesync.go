package hashsync

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

const (
	defaultMaxSendRange  = 16
	defaultItemChunkSize = 16
)

type MessageType byte

const (
	MessageTypeDone MessageType = iota
	MessageTypeEndRound
	MessageTypeEmptySet
	MessageTypeEmptyRange
	MessageTypeFingerprint
	MessageTypeRangeContents
	MessageTypeItemBatch
	MessageTypeQuery
)

var messageTypes = []string{
	"done",
	"endRound",
	"emptySet",
	"emptyRange",
	"fingerprint",
	"rangeContents",
	"itemBatch",
}

func (mtype MessageType) String() string {
	if int(mtype) < len(messageTypes) {
		return messageTypes[mtype]
	}
	return fmt.Sprintf("<unknown %02x>", int(mtype))
}

type SyncMessage interface {
	Type() MessageType
	X() Ordered
	Y() Ordered
	Fingerprint() any
	Count() int
	Keys() []Ordered
	Values() []any
}

func SyncMessageToString(m SyncMessage) string {
	var sb strings.Builder
	sb.WriteString("<" + m.Type().String())

	if x := m.X(); x != nil {
		sb.WriteString(" X=" + x.(fmt.Stringer).String())
	}
	if y := m.Y(); y != nil {
		sb.WriteString(" Y=" + y.(fmt.Stringer).String())
	}
	if count := m.Count(); count != 0 {
		fmt.Fprintf(&sb, " Count=%d", count)
	}
	if fp := m.Fingerprint(); fp != nil {
		sb.WriteString(" FP=" + fp.(fmt.Stringer).String())
	}
	vals := m.Values()
	for n, k := range m.Keys() {
		fmt.Fprintf(&sb, " item=[%s:%#v]", k.(fmt.Stringer).String(), vals[n])
	}
	sb.WriteString(">")
	return sb.String()
}

type NewValueFunc func() any

// Conduit handles receiving and sending peer messages
type Conduit interface {
	// NextMessage returns the next SyncMessage, or nil if there are no more
	// SyncMessages for this session. NextMessage is only called after a NextItem call
	// indicates that there are no more items. NextMessage should not be called after
	// any of Send...() methods is invoked
	NextMessage() (SyncMessage, error)
	// SendFingerprint sends range fingerprint to the peer.
	// Count must be > 0
	SendFingerprint(x, y Ordered, fingerprint any, count int) error
	// SendEmptySet notifies the peer that it we don't have any items.
	// The corresponding SyncMessage has Count() == 0, X() == nil and Y() == nil
	SendEmptySet() error
	// SendEmptyRange notifies the peer that the specified range
	// is empty on our side. The corresponding SyncMessage has Count() == 0
	SendEmptyRange(x, y Ordered) error
	// SendItems notifies the peer that the corresponding range items will
	// be included in this sync round. The items themselves are sent via
	// SendItemsOnly
	SendRangeContents(x, y Ordered, count int) error
	// SendItems sends just items without any message
	SendItems(count, chunkSize int, it Iterator) error
	// SendEndRound sends a message that signifies the end of sync round
	SendEndRound() error
	// SendDone sends a message that notifies the peer that sync is finished
	SendDone() error
	// SendQuery sends a message requesting fingerprint and count of the
	// whole range or part of the range. The response will never contain any
	// actual data items
	SendQuery(x, y Ordered) error
}

type Option func(r *RangeSetReconciler)

func WithMaxSendRange(n int) Option {
	return func(r *RangeSetReconciler) {
		r.maxSendRange = n
	}
}

func WithItemChunkSize(n int) Option {
	return func(r *RangeSetReconciler) {
		r.itemChunkSize = n
	}
}

// Iterator points to in item in ItemStore
type Iterator interface {
	// Equal returns true if this iterator is equal to another Iterator
	Equal(other Iterator) bool
	// Key returns the key corresponding to iterator position. It returns
	// nil if the ItemStore is empty
	Key() Ordered
	// Value returns the value corresponding to the iterator. It returns nil
	// if the ItemStore is empty
	Value() any
	// Next advances the iterator
	Next()
}

type RangeInfo struct {
	Fingerprint any
	Count       int
	Start, End  Iterator
}

type ItemStore interface {
	// Add adds a key-value pair to the store
	Add(k Ordered, v any)
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
	// New returns an empty payload value
	New() any
}

type RangeSetReconciler struct {
	is            ItemStore
	maxSendRange  int
	itemChunkSize int
}

func NewRangeSetReconciler(is ItemStore, opts ...Option) *RangeSetReconciler {
	rsr := &RangeSetReconciler{
		is:            is,
		maxSendRange:  defaultMaxSendRange,
		itemChunkSize: defaultItemChunkSize,
	}
	for _, opt := range opts {
		opt(rsr)
	}
	if rsr.maxSendRange <= 0 {
		panic("bad maxSendRange")
	}
	return rsr
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

func (rsr *RangeSetReconciler) processSubrange(c Conduit, preceding, start, end Iterator, x, y Ordered) (Iterator, error) {
	if preceding != nil && preceding.Key().Compare(x) > 0 {
		preceding = nil
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: preceding=%q\n",
	// 	qqqqRmmeK(preceding))
	// TODO: don't re-request range info for the first part of range after stop
	info := rsr.is.GetRangeInfo(preceding, x, y, -1)
	// fmt.Fprintf(os.Stderr, "QQQQQ: start=%q end=%q info.Start=%q info.End=%q info.FP=%q x=%q y=%q\n",
	// 	qqqqRmmeK(start), qqqqRmmeK(end), qqqqRmmeK(info.Start), qqqqRmmeK(info.End), info.Fingerprint, x, y)
	switch {
	// TODO: make sending items from small chunks resulting from subdivision right away an option
	// case info.Count != 0 && info.Count <= rsr.maxSendRange:
	// 	// If the range is small enough, we send its contents.
	// 	// The peer may have more items of its own in that range,
	// 	// so we can't use SendItemsOnly(), instead we use SendItems,
	// 	// which includes our items and asks the peer to send any
	// 	// items it has in the range.
	// 	if err := c.SendRangeContents(x, y, info.Count); err != nil {
	// 		return nil, err
	// 	}
	// 	if err := c.SendItems(info.Count, rsr.itemChunkSize, info.Start); err != nil {
	// 		return nil, err
	// 	}
	case info.Count == 0:
		// We have no more items in this subrange.
		// Ask peer to send any items it has in the range
		if err := c.SendEmptyRange(x, y); err != nil {
			return nil, err
		}
	default:
		// The range is non-empty and large enough.
		// Send fingerprint so that the peer can further subdivide it.
		if err := c.SendFingerprint(x, y, info.Fingerprint, info.Count); err != nil {
			return nil, err
		}
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: info.End=%q\n", qqqqRmmeK(info.End))
	return info.End, nil
}

func (rsr *RangeSetReconciler) handleMessage(c Conduit, preceding Iterator, msg SyncMessage) (it Iterator, done bool, err error) {
	x := msg.X()
	y := msg.Y()
	done = true
	if msg.Type() == MessageTypeEmptySet || (msg.Type() == MessageTypeQuery && x == nil && y == nil) {
		// The peer has no items at all so didn't
		// even send X & Y (SendEmptySet)
		it := rsr.is.Min()
		if it == nil {
			// We don't have any items at all, too
			return nil, true, nil
		}
		x = it.Key()
		y = x
	} else if x == nil || y == nil {
		return nil, false, errors.New("bad X or Y")
	}
	info := rsr.is.GetRangeInfo(preceding, x, y, -1)
	// fmt.Fprintf(os.Stderr, "msg %s fp %v start %#v end %#v count %d\n", msg, info.Fingerprint, info.Start, info.End, info.Count)
	switch {
	case msg.Type() == MessageTypeEmptyRange ||
		msg.Type() == MessageTypeRangeContents ||
		msg.Type() == MessageTypeEmptySet:
		// The peer has no more items to send in this range after this
		// message, as it is either empty or it has sent all of its
		// items in the range to us, but there may be some items on our
		// side. In the latter case, send only the items themselves b/c
		// the range doesn't need any further handling by the peer.
		if info.Count != 0 {
			done = false
			if err := c.SendItems(info.Count, rsr.itemChunkSize, info.Start); err != nil {
				return nil, false, err
			}
		}
	case msg.Type() == MessageTypeQuery:
		if err := c.SendFingerprint(x, y, info.Fingerprint, info.Count); err != nil {
			return nil, false, err
		}
		return nil, true, nil
	case msg.Type() != MessageTypeFingerprint:
		return nil, false, fmt.Errorf("unexpected message type %s", msg.Type())
	case fingerprintEqual(info.Fingerprint, msg.Fingerprint()):
		// The range is synced
	// case (info.Count+1)/2 <= rsr.maxSendRange:
	case info.Count <= rsr.maxSendRange:
		// The range differs from the peer's version of it, but the it
		// is small enough (or would be small enough after split) or
		// empty on our side
		done = false
		if info.Count != 0 {
			// fmt.Fprintf(os.Stderr, "small incoming range: %s -> SendItems\n", msg)
			if err := c.SendRangeContents(x, y, info.Count); err != nil {
				return nil, false, err
			}
			if err := c.SendItems(info.Count, rsr.itemChunkSize, info.Start); err != nil {
				return nil, false, err
			}
		} else {
			// fmt.Fprintf(os.Stderr, "small incoming range: %s -> empty range msg\n", msg)
			if err := c.SendEmptyRange(x, y); err != nil {
				return nil, false, err
			}
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
		next, err := rsr.processSubrange(c, info.Start, part.Start, part.End, x, middle)
		if err != nil {
			return nil, false, err
		}
		// fmt.Fprintf(os.Stderr, "QQQQQ: next=%q\n", qqqqRmmeK(next))
		_, err = rsr.processSubrange(c, next, part.End, info.End, middle, y)
		if err != nil {
			return nil, false, err
		}
		// fmt.Fprintf(os.Stderr, "normal: split X %s - middle %s - Y %s:\n  %s",
		// 	msg.X(), middle, msg.Y(), msg)
		done = false
	}
	return info.End, done, nil
}

func (rsr *RangeSetReconciler) Initiate(c Conduit) error {
	it := rsr.is.Min()
	var x Ordered
	if it != nil {
		x = it.Key()
	}
	return rsr.InitiateBounded(c, x, x)
}

func (rsr *RangeSetReconciler) InitiateBounded(c Conduit, x, y Ordered) error {
	if x == nil {
		if err := c.SendEmptySet(); err != nil {
			return err
		}
	} else {
		info := rsr.is.GetRangeInfo(nil, x, y, -1)
		switch {
		case info.Count == 0:
			panic("empty full min-min range")
		case info.Count < rsr.maxSendRange:
			if err := c.SendRangeContents(x, y, info.Count); err != nil {
				return err
			}
			if err := c.SendItems(info.Count, rsr.itemChunkSize, info.Start); err != nil {
				return err
			}
		default:
			if err := c.SendFingerprint(x, y, info.Fingerprint, info.Count); err != nil {
				return err
			}
		}
	}
	if err := c.SendEndRound(); err != nil {
		return err
	}
	return nil
}

func (rsr *RangeSetReconciler) getMessages(c Conduit) (msgs []SyncMessage, done bool, err error) {
	for {
		msg, err := c.NextMessage()
		switch {
		case err != nil:
			return msgs, false, err
		case msg == nil:
			return msgs, false, errors.New("no end round marker")
		default:
			switch msg.Type() {
			case MessageTypeEndRound:
				return msgs, false, nil
			case MessageTypeDone:
				return msgs, true, nil
			default:
				msgs = append(msgs, msg)
			}
		}
	}
}

func (rsr *RangeSetReconciler) InitiateProbe(c Conduit) error {
	return rsr.InitiateBoundedProbe(c, nil, nil)
}

func (rsr *RangeSetReconciler) InitiateBoundedProbe(c Conduit, x, y Ordered) error {
	if err := c.SendQuery(x, y); err != nil {
		return err
	}
	if err := c.SendEndRound(); err != nil {
		return err
	}
	return nil
}

func (rsr *RangeSetReconciler) HandleProbeResponse(c Conduit) (fp any, count int, err error) {
	gotRange := false
	for {
		msg, err := c.NextMessage()
		switch {
		case err != nil:
			return nil, 0, err
		case msg == nil:
			return nil, 0, errors.New("no end round marker")
		default:
			switch mt := msg.Type(); mt {
			case MessageTypeEndRound:
				return nil, 0, errors.New("non-final round in response to a probe")
			case MessageTypeDone:
				// the peer is not expecting any new messages
				return fp, count, nil
			case MessageTypeFingerprint:
				fp = msg.Fingerprint()
				count = msg.Count()
				fallthrough
			case MessageTypeEmptySet, MessageTypeEmptyRange:
				if gotRange {
					return nil, 0, errors.New("single range message expected")
				}
				gotRange = true
			default:
				return nil, 0, fmt.Errorf("unexpected message type: %v", msg.Type())
			}
		}
	}
}

func (rsr *RangeSetReconciler) Process(c Conduit) (done bool, err error) {
	var msgs []SyncMessage
	// All of the messages need to be received before processing
	// them, as processing the messages involves sending more
	// messages back to the peer
	msgs, done, err = rsr.getMessages(c)
	if done {
		// items already added
		if len(msgs) != 0 {
			return false, errors.New("non-item messages with 'done' marker")
		}
		return done, nil
	}
	done = true
	for _, msg := range msgs {
		if msg.Type() == MessageTypeItemBatch {
			vals := msg.Values()
			for n, k := range msg.Keys() {
				rsr.is.Add(k, vals[n])
			}
			continue
		}

		// If there was an error, just add any items received,
		// but ignore other messages
		if err != nil {
			continue
		}

		// TODO: pass preceding range. Somehow, currently the code
		// breaks if we capture the iterator from handleMessage and
		// pass it to the next handleMessage call (it shouldn't)
		var msgDone bool
		_, msgDone, err = rsr.handleMessage(c, nil, msg)
		if !msgDone {
			done = false
		}
	}

	if err != nil {
		return false, err
	}

	if done {
		err = c.SendDone()
	} else {
		err = c.SendEndRound()
	}

	if err != nil {
		return false, err
	}
	return done, nil
}

func fingerprintEqual(a, b any) bool {
	// FIXME: use Fingerprint interface with Equal() method for fingerprints
	// but still allow nil fingerprints
	return reflect.DeepEqual(a, b)
}

// TBD: test: add items to the store even in case of NextMessage() failure
// TBD: !!! use wire types instead of multiple Send* methods in the Conduit interface !!!
// TBD: !!! queue outbound messages right in RangeSetReconciler while processing msgs, and no need for done in handleMessage this way ++ no need for complicated logic on the conduit part !!!
// TBD: !!! check that done message present !!!
// Note: can't just use send/recv channels instead of Conduit b/c Receive must be an explicit
// operation done via the underlying Interactor
// TBD: SyncTree
//      * rename to SyncTree
//      * rm Monoid stuff, use Hash32 for values and Hash12 for fingerprints
//      * pass single chars as Hash32 for testing
//      * track hashing and XORing during tests to recover the fingerprint substring in tests
//        (but not during XOR test!)
// TBD: successive messages with payloads can be combined!
// TBD: limit the number of rounds (outside RangeSetReconciler)
// TBD: process ascending ranges properly
// TBD: bounded reconcile
// TBD: limit max N of received unconfirmed items
// TBD: streaming sync with sequence numbers or timestamps
// TBD: never pass just one of X and Y as nil when decoding the messages!!!
