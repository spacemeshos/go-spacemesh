package hashsync

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
)

// Interactions:
//
// A: empty set; B: empty set
// A -> B:
//
//	EmptySet
//	EndRound
//
// B -> A:
//
//	Done
//
// A: empty set; B: non-empty set
// A -> B:
//
//	EmptySet
//	EndRound
//
// B -> A:
//
//	ItemBatch
//	ItemBatch
//	...
//
// A -> B:
//
//	Done
//
// A: small set (< maxSendRange); B: non-empty set
// A -> B:
//
//	ItemBatch
//	ItemBatch
//	...
//	RangeContents [x, y)
//	EndRound
//
// B -> A:
//
//	ItemBatch
//	ItemBatch
//	...
//
// A -> B:
//
//	Done
//
// A: large set; B: non-empty set; maxDiff < 0
// A -> B:
//
//	Fingerprint [x, y)
//	EndRound
//
// B -> A:
//
//	Fingerprint [x, m)
//	Fingerprint [m, y)
//	EndRound
//
// A -> B:
//
//	ItemBatch
//	ItemBatch
//	...
//	RangeContents [x, m)
//	EndRound
//
// A -> B:
//
//	Done
//
// A: large set; B: non-empty set; maxDiff >= 0; differenceMetric <= maxDiff
// NOTE: Sample includes fingerprint
// A -> B:
//
//	Sample [x, y)
//	EndRound
//
// B -> A:
//
//	Fingerprint [x, m)
//	Fingerprint [m, y)
//	EndRound
//
// A -> B:
//
//	ItemBatch
//	ItemBatch
//	...
//	RangeContents [x, m)
//	EndRound
//
// A -> B:
//
//	Done
//
// A: large set; B: non-empty set; maxDiff >= 0; differenceMetric > maxDiff
// A -> B:
//
//	Sample [x, y)
//	EndRound
//
// B -> A:
//
//	ItemBatch
//	ItemBatch
//	...
//	RangeContents [x, y)
//	EndRound
//
// A -> B:
//
//	Done
//
// A: large set; B: non-empty set; sync priming; maxDiff >= 0; differenceMetric <= maxDiff (after priming)
// A -> B:
//
//	ItemBatch
//	ItemBatch
//	...
//	Recent
//	EndRound
//
// B -> A:
//
//	ItemBatch
//	ItemBatch
//	...
//	Sample [x, y)
//	EndRound
//
// A -> B:
//
//	Fingerprint [x, m)
//	Fingerprint [m, y)
//	EndRound
//
// B -> A:
//
//	ItemBatch
//	ItemBatch
//	...
//	RangeContents [x, m)
//	EndRound
//
// A -> B:
//
//	Done
//
// A: large set; B: non-empty set; sync priming; maxDiff < 0
// A -> B:
//
//	ItemBatch
//	ItemBatch
//	...
//	Recent
//	EndRound
//
// B -> A:
//
//	ItemBatch
//	ItemBatch
//	...
//	Fingerprint [x, y)
//	EndRound
//
// A -> B:
//
//	Fingerprint [x, m)
//	Fingerprint [m, y)
//	EndRound
//
// B -> A:
//
//	ItemBatch
//	ItemBatch
//	...
//	RangeContents [x, m)
//	EndRound
//
// A -> B:
//
//	Done

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
	MessageTypeSample
	MessageTypeRecent
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
	"sample",
	"recent",
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
	Since() time.Time
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
	// SendItems sends a chunk of items
	SendChunk(items []Ordered) error
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
	// SendSample sends a set sample. If 'it' is not nil, the corresponding items are
	// included in the sample
	SendSample(x, y Ordered, fingerprint any, count, sampleSize int, it Iterator) error
	// SendRecent sends recent items
	SendRecent(since time.Time) error
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

// WithMaxDiff sets maximum set difference metric (0..1) allowed for recursive
// reconciliation, with value of 0 meaning equal sets and 1 meaning completely disjoint
// set. If the difference metric MaxDiff value, the whole set is transmitted instead of
// applying the recursive algorithm.
func WithMaxDiff(d float64) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.maxDiff = d
	}
}

// TODO: RangeSetReconciler should sit in a separate package
// and WithRangeReconcilerLogger should be named WithLogger
func WithRangeReconcilerLogger(log *zap.Logger) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.log = log
	}
}

// WithRecentTimeSpan specifies the time span for recent items
func WithRecentTimeSpan(d time.Duration) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.recentTimeSpan = d
	}
}

// Tracer tracks the reconciliation process
type Tracer interface {
	// OnDumbSync is called when the difference metric exceeds maxDiff and dumb
	// reconciliation process is used
	OnDumbSync()
	// OnRecent is invoked when Recent message is received
	OnRecent(receivedItems, sentItems int)
}

type nullTracer struct{}

func (t nullTracer) OnDumbSync()       {}
func (t nullTracer) OnRecent(int, int) {}

// WithTracer specifies a tracer for RangeSetReconciler
func WithTracer(t Tracer) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.tracer = t
	}
}

// TBD: rename
func WithRangeReconcilerClock(c clockwork.Clock) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.clock = c
	}
}

type ProbeResult struct {
	FP    any
	Count int
	Sim   float64
}

type RangeSetReconciler struct {
	is             ItemStore
	maxSendRange   int
	itemChunkSize  int
	sampleSize     int
	maxDiff        float64
	recentTimeSpan time.Duration
	tracer         Tracer
	clock          clockwork.Clock
	log            *zap.Logger
}

func NewRangeSetReconciler(is ItemStore, opts ...RangeSetReconcilerOption) *RangeSetReconciler {
	rsr := &RangeSetReconciler{
		is:            is,
		maxSendRange:  DefaultMaxSendRange,
		itemChunkSize: DefaultItemChunkSize,
		sampleSize:    DefaultSampleSize,
		maxDiff:       -1,
		tracer:        nullTracer{},
		clock:         clockwork.NewRealClock(),
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

func (rsr *RangeSetReconciler) processSubrange(c Conduit, x, y Ordered, info RangeInfo) error {
	// fmt.Fprintf(os.Stderr, "QQQQQ: preceding=%q\n",
	// 	qqqqRmmeK(preceding))
	// TODO: don't re-request range info for the first part of range after stop
	rsr.log.Debug("processSubrange", HexField("x", x), HexField("y", y),
		zap.Int("count", info.Count), HexField("fingerprint", info.Fingerprint))
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
	// 	if err := c.SendItems(info.Count, rsr.itemChunkSize, info.Start); err != nil {
	// 		return nil, err
	// 	}
	// 	if err := c.SendRangeContents(x, y, info.Count); err != nil {
	// 		return nil, err
	// 	}
	case info.Count == 0:
		// We have no more items in this subrange.
		// Ask peer to send any items it has in the range
		rsr.log.Debug("processSubrange: send empty range", HexField("x", x), HexField("y", y))
		if err := c.SendEmptyRange(x, y); err != nil {
			return err
		}
	default:
		// The range is non-empty and large enough.
		// Send fingerprint so that the peer can further subdivide it.
		rsr.log.Debug("processSubrange: send fingerprint", HexField("x", x), HexField("y", y),
			zap.Int("count", info.Count))
		if err := c.SendFingerprint(x, y, info.Fingerprint, info.Count); err != nil {
			return err
		}
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: info.End=%q\n", qqqqRmmeK(info.End))
	return nil
}

func (rsr *RangeSetReconciler) splitRange(
	ctx context.Context,
	c Conduit,
	preceding Iterator,
	count int,
	x, y Ordered,
) error {
	count = count / 2
	rsr.log.Debug("handleMessage: PRE split range",
		HexField("x", x), HexField("y", y),
		zap.Int("countArg", count))
	si, err := rsr.is.SplitRange(ctx, preceding, x, y, count)
	if err != nil {
		return err
	}
	rsr.log.Debug("handleMessage: split range",
		HexField("x", x), HexField("y", y),
		zap.Int("countArg", count),
		zap.Int("count0", si.Parts[0].Count),
		HexField("fp0", si.Parts[0].Fingerprint),
		IteratorField("start0", si.Parts[0].Start),
		IteratorField("end0", si.Parts[0].End),
		zap.Int("count1", si.Parts[1].Count),
		HexField("fp1", si.Parts[1].Fingerprint),
		IteratorField("start1", si.Parts[1].End),
		IteratorField("end1", si.Parts[1].End))
	if err := rsr.processSubrange(c, x, si.Middle, si.Parts[0]); err != nil {
		return err
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: next=%q\n", qqqqRmmeK(next))
	if err := rsr.processSubrange(c, si.Middle, y, si.Parts[1]); err != nil {
		return err
	}
	return nil
}

func (rsr *RangeSetReconciler) sendSmallRange(
	c Conduit,
	count int,
	it Iterator,
	x, y Ordered,
) error {
	if count == 0 {
		rsr.log.Debug("handleMessage: empty incoming range",
			HexField("x", x), HexField("y", y))
		// fmt.Fprintf(os.Stderr, "small incoming range: %s -> empty range msg\n", msg)
		return c.SendEmptyRange(x, y)
	}
	rsr.log.Debug("handleMessage: send small range",
		HexField("x", x), HexField("y", y),
		zap.Int("count", count),
		zap.Int("maxSendRange", rsr.maxSendRange))
	// fmt.Fprintf(os.Stderr, "small incoming range: %s -> SendItems\n", msg)
	if err := c.SendRangeContents(x, y, count); err != nil {
		return err
	}
	_, err := rsr.sendItems(c, count, it, nil)
	return err
}

func (rsr *RangeSetReconciler) sendItems(
	c Conduit,
	count int,
	it Iterator,
	skipKeys []Ordered,
) (int, error) {
	nSent := 0
	skipPos := 0
	for i := 0; i < count; i += rsr.itemChunkSize {
		// TBD: do not use chunks, just stream the contentkeys
		var keys []Ordered
		n := min(rsr.itemChunkSize, count-i)
	IN_CHUNK:
		for n > 0 {
			k, err := it.Key()
			if err != nil {
				return nSent, err
			}
			for skipPos < len(skipKeys) {
				cmp := k.Compare(skipKeys[skipPos])
				if cmp == 0 {
					// we can skip this item. Advance skipPos as there are no duplicates
					skipPos++
					continue IN_CHUNK
				}
				if cmp < 0 {
					// current ley is yet to reach the skipped key at skipPos
					break
				}
				// current item is greater than the skipped key at skipPos,
				// so skipPos needs to catch up with the iterator
				skipPos++
			}
			keys = append(keys, k)
			if err := it.Next(); err != nil {
				return nSent, err
			}
			n--
		}
		if err := c.SendChunk(keys); err != nil {
			return nSent, err
		}
		nSent += len(keys)
	}
	return nSent, nil
}

// handleMessage handles incoming messages. Note that the set reconciliation protocol is
// designed to be stateless.
func (rsr *RangeSetReconciler) handleMessage(
	ctx context.Context,
	c Conduit,
	preceding Iterator,
	msg SyncMessage,
	receivedKeys []Ordered,
) (done bool, err error) {
	rsr.log.Debug("handleMessage", IteratorField("preceding", preceding),
		zap.String("msg", SyncMessageToString(msg)))
	x := msg.X()
	y := msg.Y()
	done = true
	if msg.Type() == MessageTypeEmptySet ||
		msg.Type() == MessageTypeRecent ||
		(msg.Type() == MessageTypeProbe && x == nil && y == nil) {
		// The peer has no items at all so didn't
		// even send X & Y (SendEmptySet)
		it, err := rsr.is.Min(ctx)
		if err != nil {
			return false, err
		}
		if it == nil {
			// We don't have any items at all, too
			if msg.Type() == MessageTypeProbe {
				info, err := rsr.is.GetRangeInfo(ctx, preceding, nil, nil, -1)
				if err != nil {
					return false, err
				}
				rsr.log.Debug("handleMessage: send probe response",
					HexField("fingerpint", info.Fingerprint),
					zap.Int("count", info.Count),
					IteratorField("it", it))
				if err := c.SendSample(x, y, info.Fingerprint, info.Count, 0, it); err != nil {
					return false, err
				}
			}
			if msg.Type() == MessageTypeRecent {
				rsr.tracer.OnRecent(len(receivedKeys), 0)
			}
			return true, nil
		}
		x, err = it.Key()
		if err != nil {
			return false, err
		}
		y = x
	} else if x == nil || y == nil {
		return false, fmt.Errorf("bad X or Y in a message of type %s", msg.Type())
	}
	info, err := rsr.is.GetRangeInfo(ctx, preceding, x, y, -1)
	if err != nil {
		return false, err
	}
	rsr.log.Debug("handleMessage: range info",
		HexField("x", x), HexField("y", y),
		IteratorField("start", info.Start),
		IteratorField("end", info.End),
		zap.Int("count", info.Count),
		HexField("fingerprint", info.Fingerprint))

	// fmt.Fprintf(os.Stderr, "QQQQQ msg %s %#v fp %v start %#v end %#v count %d\n", msg.Type(), msg, info.Fingerprint, info.Start, info.End, info.Count)
	// TODO: do not use done variable
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
			if _, err := rsr.sendItems(c, info.Count, info.Start, receivedKeys); err != nil {
				return false, err
			}
		} else {
			rsr.log.Debug("handleMessage: local range is empty")
		}
	case msg.Type() == MessageTypeProbe:
		sampleSize := msg.Count()
		if sampleSize > maxSampleSize {
			return false, fmt.Errorf("bad minhash sample size %d (max %d)",
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
		if err := c.SendSample(x, y, info.Fingerprint, info.Count, sampleSize, it); err != nil {
			return false, err
		}
		return true, nil
	case msg.Type() == MessageTypeRecent:
		it, count, err := rsr.is.Recent(ctx, msg.Since())
		if err != nil {
			return false, fmt.Errorf("error getting recent items: %w", err)
		}
		nSent := 0
		if count != 0 {
			// Do not send back recent items that were received
			if nSent, err = rsr.sendItems(c, count, it, receivedKeys); err != nil {
				return false, err
			}
		}
		rsr.log.Debug("handled recent message",
			zap.Int("receivedCount", len(receivedKeys)),
			zap.Int("sentCount", nSent))
		rsr.tracer.OnRecent(len(receivedKeys), nSent)
		// if x == nil {
		// 	// FIXME: code duplication
		// 	it, err := rsr.is.Min(ctx)
		// 	if err != nil {
		// 		return false, err
		// 	}
		// 	if it != nil {
		// 		x, err = it.Key()
		// 		if err != nil {
		// 			return false, err
		// 		}
		// 		y = x
		// 	}
		// }
		return false, rsr.initiateBounded(ctx, c, x, y, false)
	case msg.Type() != MessageTypeFingerprint && msg.Type() != MessageTypeSample:
		return false, fmt.Errorf("unexpected message type %s", msg.Type())
	case fingerprintEqual(info.Fingerprint, msg.Fingerprint()):
		// The range is synced
	case msg.Type() == MessageTypeSample && rsr.maxDiff >= 0:
		// The peer has sent a sample of its items in the range to check if
		// recursive reconciliation approach is feasible.
		pr, err := rsr.handleSample(c, msg, info)
		if err != nil {
			return false, err
		}
		if 1-pr.Sim > rsr.maxDiff {
			rsr.tracer.OnDumbSync()
			rsr.log.Debug("handleMessage: maxDiff exceeded, sending full range",
				zap.Float64("sim", pr.Sim),
				zap.Float64("diff", 1-pr.Sim),
				zap.Float64("maxDiff", rsr.maxDiff))
			if _, err := rsr.sendItems(c, info.Count, info.Start, nil); err != nil {
				return false, err
			}
			return false, c.SendRangeContents(x, y, info.Count)
		}
		rsr.log.Debug("handleMessage: acceptable maxDiff, proceeding with sync",
			zap.Float64("sim", pr.Sim),
			zap.Float64("diff", 1-pr.Sim),
			zap.Float64("maxDiff", rsr.maxDiff))
		if info.Count > rsr.maxSendRange {
			return false, rsr.splitRange(ctx, c, preceding, info.Count, x, y)
		}
		return false, rsr.sendSmallRange(c, info.Count, info.Start, x, y)
	// case (info.Count+1)/2 <= rsr.maxSendRange:
	case info.Count <= rsr.maxSendRange:
		return false, rsr.sendSmallRange(c, info.Count, info.Start, x, y)
	default:
		return false, rsr.splitRange(ctx, c, preceding, info.Count, x, y)
	}
	return done, nil
}

func (rsr *RangeSetReconciler) Initiate(ctx context.Context, c Conduit) error {
	it, err := rsr.is.Min(ctx)
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
	return rsr.InitiateBounded(ctx, c, x, x)
}

func (rsr *RangeSetReconciler) InitiateBounded(ctx context.Context, c Conduit, x, y Ordered) error {
	haveRecent := rsr.recentTimeSpan > 0
	if err := rsr.initiateBounded(ctx, c, x, y, haveRecent); err != nil {
		return err
	}
	return c.SendEndRound()

}
func (rsr *RangeSetReconciler) initiateBounded(ctx context.Context, c Conduit, x, y Ordered, haveRecent bool) error {
	rsr.log.Debug("initiate", HexField("x", x), HexField("y", y))
	if x == nil {
		rsr.log.Debug("initiate: send empty set")
		return c.SendEmptySet()
	}
	info, err := rsr.is.GetRangeInfo(ctx, nil, x, y, -1)
	if err != nil {
		return fmt.Errorf("get range info: %w", err)
	}
	switch {
	case info.Count == 0:
		panic("empty full min-min range")
	case info.Count < rsr.maxSendRange:
		rsr.log.Debug("initiate: send whole range", zap.Int("count", info.Count))
		if _, err := rsr.sendItems(c, info.Count, info.Start, nil); err != nil {
			return err
		}
		return c.SendRangeContents(x, y, info.Count)
	case haveRecent:
		rsr.log.Debug("initiate: checking recent items")
		since := rsr.clock.Now().Add(-rsr.recentTimeSpan)
		it, count, err := rsr.is.Recent(ctx, since)
		if err != nil {
			return fmt.Errorf("error getting recent items: %w", err)
		}
		if count != 0 {
			rsr.log.Debug("initiate: sending recent items", zap.Int("count", count))
			if n, err := rsr.sendItems(c, count, it, nil); err != nil {
				return err
			} else if n != count {
				panic("BUG: wrong number of items sent")
			}
		} else {
			rsr.log.Debug("initiate: no recent items")
		}
		rsr.tracer.OnRecent(0, count)
		// Send Recent message even if there are no recent items, b/c we want to
		// receive recent items from the peer, if any.
		if err := c.SendRecent(since); err != nil {
			return err
		}
		return nil
	case rsr.maxDiff >= 0:
		// Use minhash to check if syncing this range is feasible
		rsr.log.Debug("initiate: send sample",
			zap.Int("count", info.Count),
			zap.Int("sampleSize", rsr.sampleSize))
		return c.SendSample(x, y, info.Fingerprint, info.Count, rsr.sampleSize, info.Start)
	default:
		rsr.log.Debug("initiate: send fingerprint", zap.Int("count", info.Count))
		return c.SendFingerprint(x, y, info.Fingerprint, info.Count)
	}
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

func (rsr *RangeSetReconciler) InitiateProbe(ctx context.Context, c Conduit) (RangeInfo, error) {
	return rsr.InitiateBoundedProbe(ctx, c, nil, nil)
}

func (rsr *RangeSetReconciler) InitiateBoundedProbe(
	ctx context.Context,
	c Conduit,
	x, y Ordered,
) (RangeInfo, error) {
	info, err := rsr.is.GetRangeInfo(ctx, nil, x, y, -1)
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
	// for n, k := range remoteSample {
	// 	fmt.Fprintf(os.Stderr, "QQQQQ: remoteSample[%d] = %s\n", n, k.(MinhashSampleItem).String())
	// }
	sampleSize := min(info.Count, rsr.sampleSize)
	localSample := make([]Ordered, sampleSize)
	it := info.Start
	for n := 0; n < sampleSize; n++ {
		k, err := it.Key()
		if err != nil {
			return 0, err
		}
		localSample[n] = c.ShortenKey(k)
		// fmt.Fprintf(os.Stderr, "QQQQQ: n %d sampleSize %d info.Count %d rsr.sampleSize %d -- %s -> %s\n",
		// 	n, sampleSize, info.Count, rsr.sampleSize, k.(types.Hash32).String(),
		// 	localSample[n].(MinhashSampleItem).String())
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
			// k, _ := it.Key()
			// fmt.Fprintf(os.Stderr, "QQQQQ: less: %v < %s\n", c.ShortenKey(k), remoteSample[n])
			m++
		case d == 0:
			// fmt.Fprintf(os.Stderr, "QQQQQ: eq: %v\n", remoteSample[n])
			numEq++
			m++
			n++
		default:
			// k, _ := it.Key()
			// fmt.Fprintf(os.Stderr, "QQQQQ: gt: %v > %s\n", c.ShortenKey(k), remoteSample[n])
			n++
		}
	}
	maxSampleSize := max(sampleSize, len(remoteSample))
	// fmt.Fprintf(os.Stderr, "QQQQQ: numEq %d maxSampleSize %d\n", numEq, maxSampleSize)
	return float64(numEq) / float64(maxSampleSize), nil
}

func (rsr *RangeSetReconciler) handleSample(
	c Conduit,
	msg SyncMessage,
	info RangeInfo,
) (pr ProbeResult, err error) {
	pr.FP = msg.Fingerprint()
	pr.Count = msg.Count()
	sim, err := rsr.calcSim(c, info, msg.Keys(), msg.Fingerprint())
	if err != nil {
		return ProbeResult{}, fmt.Errorf("database error: %w", err)
	}
	pr.Sim = sim
	return pr, nil
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
			case MessageTypeSample:
				if gotRange {
					return ProbeResult{}, errors.New("single range message expected")
				}
				pr, err = rsr.handleSample(c, msg, info)
				if err != nil {
					return ProbeResult{}, err
				}
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
			return false, errors.New("no extra messages expected along with 'done' message")
		}
		return done, nil
	}
	done = true
	var receivedKeys []Ordered
	for _, msg := range msgs {
		if msg.Type() == MessageTypeItemBatch {
			for _, k := range msg.Keys() {
				rsr.log.Debug("Process: add item", HexField("item", k))
				if err := rsr.is.Add(ctx, k); err != nil {
					return false, fmt.Errorf("error adding an item to the store: %w", err)
				}
				receivedKeys = append(receivedKeys, k)
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
		msgDone, err = rsr.handleMessage(ctx, c, nil, msg, receivedKeys)
		if !msgDone {
			done = false
		}
		receivedKeys = nil
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

type IterEntry[T Ordered] struct {
	V   T
	Err error
}

func IterItems[T Ordered](ctx context.Context, is ItemStore) iter.Seq2[T, error] {
	return iter.Seq2[T, error](func(yield func(T, error) bool) {
		var empty T
		ctx := context.Background()
		it, err := is.Min(ctx)
		if err != nil {
			yield(empty, err)
			return
		}
		if it == nil {
			return
		}
		k, err := it.Key()
		if err != nil {
			yield(empty, err)
			return
		}
		info, err := is.GetRangeInfo(ctx, nil, k, k, -1)
		if err != nil {
			yield(empty, err)
			return
		}
		it, err = is.Min(ctx)
		if err != nil {
			yield(empty, err)
			return
		}
		for n := 0; n < info.Count; n++ {
			k, err := it.Key()
			if err != nil {
				yield(empty, err)
				return
			}
			if k == nil {
				// fmt.Fprintf(os.Stderr, "QQQQQ: it: %#v\n", it)
				panic("BUG: iterator exausted before Count reached")
			}
			yield(k.(T), nil)
			if err := it.Next(); err != nil {
				yield(empty, err)
				return
			}
		}
	})
}

// CollectStoreItems returns the list of items in the given store
func CollectStoreItems[T Ordered](ctx context.Context, is ItemStore) (r []T, err error) {
	for v, err := range IterItems[T](ctx, is) {
		if err != nil {
			return nil, err
		}
		r = append(r, v)
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
