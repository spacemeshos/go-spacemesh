package hashsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"

	"go.uber.org/zap"
)

const (
	DefaultMaxSendRange  = 16
	DefaultItemChunkSize = 16
	DefaultSampleSize    = 200
	maxSampleSize        = 1000
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
	MessageTypeProbe
	MessageTypeProbeResponse
)

var messageTypes = []string{
	"done",
	"endRound",
	"emptySet",
	"emptyRange",
	"fingerprint",
	"rangeContents",
	"itemBatch",
	"probe",
	"probeResponse",
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
}

func formatID(v any) string {
	switch v := v.(type) {
	case fmt.Stringer:
		s := v.String()
		if len(s) > 10 {
			return s[:10]
		}
		return s
	case string:
		return v
	default:
		return "<unknown>"
	}
}

func SyncMessageToString(m SyncMessage) string {
	var sb strings.Builder
	sb.WriteString("<" + m.Type().String())
	if x := m.X(); x != nil {
		sb.WriteString(" X=" + formatID(x))
	}
	if y := m.Y(); y != nil {
		sb.WriteString(" Y=" + formatID(y))
	}
	if count := m.Count(); count != 0 {
		fmt.Fprintf(&sb, " Count=%d", count)
	}
	if fp := m.Fingerprint(); fp != nil {
		sb.WriteString(" FP=" + formatID(fp))
	}
	for _, k := range m.Keys() {
		fmt.Fprintf(&sb, " item=%s", formatID(k))
	}
	sb.WriteString(">")
	return sb.String()
}

// Conduit handles receiving and sending peer messages
// TODO: replace multiple Send* methods with a single one
// (after de-generalizing messages)
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
	// SendRangeContents notifies the peer that the corresponding range items will
	// be included in this sync round. The items themselves are sent via
	// SendItems
	SendRangeContents(x, y Ordered, count int) error
	// SendItems sends just items without any message
	SendItems(count, chunkSize int, it Iterator) error
	// SendEndRound sends a message that signifies the end of sync round
	SendEndRound() error
	// SendDone sends a message that notifies the peer that sync is finished
	SendDone() error
	// SendProbe sends a message requesting fingerprint and count of the
	// whole range or part of the range. If fingerprint is provided and
	// it doesn't match the fingerprint on the probe handler side,
	// the handler must send a sample subset of its items for MinHash
	// calculation.
	SendProbe(x, y Ordered, fingerprint any, sampleSize int) error
	// SendProbeResponse sends probe response. If 'it' is not nil,
	// the corresponding items are included in the sample
	SendProbeResponse(x, y Ordered, fingerprint any, count, sampleSize int, it Iterator) error
	// ShortenKey shortens the key for minhash calculation
	ShortenKey(k Ordered) Ordered
}

type RangeSetReconcilerOption func(r *RangeSetReconciler)

func WithMaxSendRange(n int) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.maxSendRange = n
	}
}

func WithItemChunkSize(n int) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.itemChunkSize = n
	}
}

func WithSampleSize(s int) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.sampleSize = s
	}
}

// TODO: RangeSetReconciler should sit in a separate package
// and WithRangeSyncLogger should be named WithLogger
func WithRangeSyncLogger(log *zap.Logger) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.log = log
	}
}

type ProbeResult struct {
	FP    any
	Count int
	Sim   float64
}

type RangeSetReconciler struct {
	is            ItemStore
	maxSendRange  int
	itemChunkSize int
	sampleSize    int
	log           *zap.Logger
}

func NewRangeSetReconciler(is ItemStore, opts ...RangeSetReconcilerOption) *RangeSetReconciler {
	rsr := &RangeSetReconciler{
		is:            is,
		maxSendRange:  DefaultMaxSendRange,
		itemChunkSize: DefaultItemChunkSize,
		sampleSize:    DefaultSampleSize,
		log:           zap.NewNop(),
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

func (rsr *RangeSetReconciler) processSubrange(c Conduit, preceding Iterator, x, y Ordered) (Iterator, error) {
	if preceding != nil {
		k, err := preceding.Key()
		if err != nil {
			return nil, err
		}
		if k.Compare(x) > 0 {
			preceding = nil
		}
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: preceding=%q\n",
	// 	qqqqRmmeK(preceding))
	// TODO: don't re-request range info for the first part of range after stop
	rsr.log.Debug("processSubrange", IteratorField("preceding", preceding), HexField("x", x), HexField("y", y))
	info, err := rsr.is.GetRangeInfo(preceding, x, y, -1)
	if err != nil {
		return nil, err
	}
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
		rsr.log.Debug("processSubrange: send empty range", HexField("x", x), HexField("y", y))
		if err := c.SendEmptyRange(x, y); err != nil {
			return nil, err
		}
	default:
		// The range is non-empty and large enough.
		// Send fingerprint so that the peer can further subdivide it.
		rsr.log.Debug("processSubrange: send fingerprint", HexField("x", x), HexField("y", y),
			zap.Int("count", info.Count))
		if err := c.SendFingerprint(x, y, info.Fingerprint, info.Count); err != nil {
			return nil, err
		}
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: info.End=%q\n", qqqqRmmeK(info.End))
	return info.End, nil
}

func (rsr *RangeSetReconciler) handleMessage(c Conduit, preceding Iterator, msg SyncMessage) (it Iterator, done bool, err error) {
	rsr.log.Debug("handleMessage", IteratorField("preceding", preceding),
		zap.String("msg", SyncMessageToString(msg)))
	x := msg.X()
	y := msg.Y()
	done = true
	if msg.Type() == MessageTypeEmptySet || (msg.Type() == MessageTypeProbe && x == nil && y == nil) {
		// The peer has no items at all so didn't
		// even send X & Y (SendEmptySet)
		it, err := rsr.is.Min()
		if err != nil {
			return nil, false, err
		}
		if it == nil {
			// We don't have any items at all, too
			if msg.Type() == MessageTypeProbe {
				info, err := rsr.is.GetRangeInfo(preceding, nil, nil, -1)
				if err != nil {
					return nil, false, err
				}
				rsr.log.Debug("handleMessage: send probe response",
					HexField("fingerpint", info.Fingerprint),
					zap.Int("count", info.Count),
					IteratorField("it", it))
				if err := c.SendProbeResponse(x, y, info.Fingerprint, info.Count, 0, it); err != nil {
					return nil, false, err
				}
			}
			return nil, true, nil
		}
		x, err = it.Key()
		if err != nil {
			return nil, false, err
		}
		y = x
	} else if x == nil || y == nil {
		return nil, false, errors.New("bad X or Y")
	}
	info, err := rsr.is.GetRangeInfo(preceding, x, y, -1)
	if err != nil {
		return nil, false, err
	}
	rsr.log.Debug("handleMessage: range info",
		HexField("x", x), HexField("y", y),
		IteratorField("start", info.Start),
		IteratorField("end", info.End),
		zap.Int("count", info.Count),
		HexField("fingerprint", info.Fingerprint))

	// fmt.Fprintf(os.Stderr, "QQQQQ msg %s %#v fp %v start %#v end %#v count %d\n", msg.Type(), msg, info.Fingerprint, info.Start, info.End, info.Count)
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
			rsr.log.Debug("handleMessage: send items", zap.Int("count", info.Count),
				IteratorField("start", info.Start))
			if err := c.SendItems(info.Count, rsr.itemChunkSize, info.Start); err != nil {
				return nil, false, err
			}
		} else {
			rsr.log.Debug("handleMessage: local range is empty")
		}
	case msg.Type() == MessageTypeProbe:
		sampleSize := msg.Count()
		if sampleSize > maxSampleSize {
			return nil, false, fmt.Errorf("bad minhash sample size %d (max %d)",
				msg.Count(), maxSampleSize)
		} else if sampleSize > info.Count {
			sampleSize = info.Count
		}
		it := info.Start
		if fingerprintEqual(msg.Fingerprint(), info.Fingerprint) {
			// no need to send MinHash items if fingerprints match
			it = nil
			sampleSize = 0
			// fmt.Fprintf(os.Stderr, "QQQQQ: fingerprint eq %#v %#v\n",
			// 	msg.Fingerprint(), info.Fingerprint)
		}
		if err := c.SendProbeResponse(x, y, info.Fingerprint, info.Count, sampleSize, it); err != nil {
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
			rsr.log.Debug("handleMessage: send small range",
				HexField("x", x), HexField("y", y), zap.Int("count", info.Count))
			// fmt.Fprintf(os.Stderr, "small incoming range: %s -> SendItems\n", msg)
			if err := c.SendRangeContents(x, y, info.Count); err != nil {
				return nil, false, err
			}
			if err := c.SendItems(info.Count, rsr.itemChunkSize, info.Start); err != nil {
				return nil, false, err
			}
		} else {
			rsr.log.Debug("handleMessage: empty incoming range",
				HexField("x", x), HexField("y", y))
			// fmt.Fprintf(os.Stderr, "small incoming range: %s -> empty range msg\n", msg)
			if err := c.SendEmptyRange(x, y); err != nil {
				return nil, false, err
			}
		}
	default:
		// Need to split the range.
		// Note that there's no special handling for rollover ranges with x >= y
		// These need to be handled by ItemStore.GetRangeInfo()
		// TODO: instead of count-based split, use split between X and Y with
		// lower bits set to zero to avoid SQL queries on the edges
		count := (info.Count + 1) / 2
		rsr.log.Debug("handleMessage: PRE split range",
			HexField("x", x), HexField("y", y),
			zap.Int("countArg", count))
		part, err := rsr.is.GetRangeInfo(preceding, x, y, count)
		if err != nil {
			return nil, false, err
		}
		if part.End == nil {
			panic("BUG: can't split range with count > 1")
		}
		rsr.log.Debug("handleMessage: split range",
			HexField("x", x), HexField("y", y),
			zap.Int("countArg", count),
			zap.Int("count", part.Count),
			HexField("fingerprint", part.Fingerprint),
			IteratorField("start", part.Start),
			IteratorField("middle", part.End))
		middle, err := part.End.Key()
		if err != nil {
			return nil, false, err
		}
		next, err := rsr.processSubrange(c, info.Start, x, middle)
		if err != nil {
			return nil, false, err
		}
		// fmt.Fprintf(os.Stderr, "QQQQQ: next=%q\n", qqqqRmmeK(next))
		_, err = rsr.processSubrange(c, next, middle, y)
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
	it, err := rsr.is.Min()
	if err != nil {
		return err
	}
	var x Ordered
	if it != nil {
		x, err = it.Key()
		if err != nil {
			return err
		}
	}
	return rsr.InitiateBounded(c, x, x)
}

func (rsr *RangeSetReconciler) InitiateBounded(c Conduit, x, y Ordered) error {
	rsr.log.Debug("inititate", HexField("x", x), HexField("y", y))
	if x == nil {
		rsr.log.Debug("initiate: send empty set")
		if err := c.SendEmptySet(); err != nil {
			return err
		}
	} else {
		info, err := rsr.is.GetRangeInfo(nil, x, y, -1)
		if err != nil {
			return err
		}
		switch {
		case info.Count == 0:
			panic("empty full min-min range")
		case info.Count < rsr.maxSendRange:
			rsr.log.Debug("initiate: send whole range", zap.Int("count", info.Count))
			if err := c.SendRangeContents(x, y, info.Count); err != nil {
				return err
			}
			if err := c.SendItems(info.Count, rsr.itemChunkSize, info.Start); err != nil {
				return err
			}
		default:
			rsr.log.Debug("initiate: send fingerprint", zap.Int("count", info.Count))
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

func (rsr *RangeSetReconciler) InitiateProbe(c Conduit) (RangeInfo, error) {
	return rsr.InitiateBoundedProbe(c, nil, nil)
}

func (rsr *RangeSetReconciler) InitiateBoundedProbe(c Conduit, x, y Ordered) (RangeInfo, error) {
	info, err := rsr.is.GetRangeInfo(nil, x, y, -1)
	if err != nil {
		return RangeInfo{}, err
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: x %#v y %#v count %d\n", x, y, info.Count)
	if err := c.SendProbe(x, y, info.Fingerprint, rsr.sampleSize); err != nil {
		return RangeInfo{}, err
	}
	if err := c.SendEndRound(); err != nil {
		return RangeInfo{}, err
	}
	return info, nil
}

func (rsr *RangeSetReconciler) calcSim(c Conduit, info RangeInfo, remoteSample []Ordered, fp any) (float64, error) {
	if fingerprintEqual(info.Fingerprint, fp) {
		return 1, nil
	}
	if info.Start == nil {
		return 0, nil
	}
	sampleSize := min(info.Count, rsr.sampleSize)
	localSample := make([]Ordered, sampleSize)
	it := info.Start
	for n := 0; n < sampleSize; n++ {
		// fmt.Fprintf(os.Stderr, "QQQQQ: n %d sampleSize %d info.Count %d rsr.sampleSize %d %#v\n",
		// 	n, sampleSize, info.Count, rsr.sampleSize, it.Key())
		k, err := it.Key()
		if err != nil {
			return 0, err
		}
		localSample[n] = c.ShortenKey(k)
		if err := it.Next(); err != nil {
			return 0, err
		}
	}
	slices.SortFunc(remoteSample, func(a, b Ordered) int { return a.Compare(b) })
	slices.SortFunc(localSample, func(a, b Ordered) int { return a.Compare(b) })

	numEq := 0
	for m, n := 0, 0; m < len(localSample) && n < len(remoteSample); {
		d := localSample[m].Compare(remoteSample[n])
		switch {
		case d < 0:
			// fmt.Fprintf(os.Stderr, "QQQQQ: less: %v < %s\n", c.ShortenKey(it.Key()), remoteSample[n])
			m++
		case d == 0:
			// fmt.Fprintf(os.Stderr, "QQQQQ: eq: %v\n", remoteSample[n])
			numEq++
			m++
			n++
		default:
			// fmt.Fprintf(os.Stderr, "QQQQQ: gt: %v > %s\n", c.ShortenKey(it.Key()), remoteSample[n])
			n++
		}
	}
	maxSampleSize := max(sampleSize, len(remoteSample))
	// fmt.Fprintf(os.Stderr, "QQQQQ: numEq %d maxSampleSize %d\n", numEq, maxSampleSize)
	return float64(numEq) / float64(maxSampleSize), nil
}

func (rsr *RangeSetReconciler) HandleProbeResponse(c Conduit, info RangeInfo) (pr ProbeResult, err error) {
	// fmt.Fprintf(os.Stderr, "QQQQQ: HandleProbeResponse\n")
	// defer fmt.Fprintf(os.Stderr, "QQQQQ: HandleProbeResponse done\n")
	gotRange := false
	for {
		msg, err := c.NextMessage()
		switch {
		case err != nil:
			return ProbeResult{}, err
		case msg == nil:
			// fmt.Fprintf(os.Stderr, "QQQQQ: HandleProbeResponse: %s %#v\n", msg.Type(), msg)
			return ProbeResult{}, errors.New("no end round marker")
		default:
			// fmt.Fprintf(os.Stderr, "QQQQQ: HandleProbeResponse: %s %#v\n", msg.Type(), msg)
			switch mt := msg.Type(); mt {
			case MessageTypeEndRound:
				return ProbeResult{}, errors.New("non-final round in response to a probe")
			case MessageTypeDone:
				// the peer is not expecting any new messages
				if !gotRange {
					return ProbeResult{}, errors.New("no range info received during probe")
				}
				return pr, nil
			case MessageTypeProbeResponse:
				if gotRange {
					return ProbeResult{}, errors.New("single range message expected")
				}
				pr.FP = msg.Fingerprint()
				pr.Count = msg.Count()
				sim, err := rsr.calcSim(c, info, msg.Keys(), msg.Fingerprint())
				if err != nil {
					return ProbeResult{}, fmt.Errorf("database error: %w", err)
				}
				pr.Sim = sim
				gotRange = true
			case MessageTypeEmptySet, MessageTypeEmptyRange:
				if gotRange {
					return ProbeResult{}, errors.New("single range message expected")
				}
				if info.Count == 0 {
					pr.Sim = 1
				}
				gotRange = true
			default:
				return ProbeResult{}, fmt.Errorf(
					"probe response: unexpected message type: %v", msg.Type())
			}
		}
	}
}

func (rsr *RangeSetReconciler) Process(ctx context.Context, c Conduit) (done bool, err error) {
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
			for _, k := range msg.Keys() {
				rsr.log.Debug("Process: add item", HexField("item", k))
				if err := rsr.is.Add(ctx, k); err != nil {
					return false, fmt.Errorf("error adding an item to the store: %w", err)
				}
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

// CollectStoreItems returns the list of items in the given store
func CollectStoreItems[K Ordered](is ItemStore) ([]K, error) {
	var r []K
	it, err := is.Min()
	if err != nil {
		return nil, err
	}
	if it == nil {
		return nil, nil
	}
	k, err := it.Key()
	if err != nil {
		return nil, err
	}
	info, err := is.GetRangeInfo(nil, k, k, -1)
	if err != nil {
		return nil, err
	}
	it, err = is.Min()
	if err != nil {
		return nil, err
	}
	for n := 0; n < info.Count; n++ {
		k, err := it.Key()
		if err != nil {
			return nil, err
		}
		if k == nil {
			fmt.Fprintf(os.Stderr, "QQQQQ: it: %#v\n", it)
			panic("BUG: iterator exausted before Count reached")
		}
		r = append(r, k.(K))
		it.Next()
	}
	return r, nil
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
