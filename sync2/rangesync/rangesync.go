package rangesync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

const (
	DefaultMaxSendRange  = 16
	DefaultItemChunkSize = 1024
	DefaultSampleSize    = 200
	maxSampleSize        = 1000
)

// RangeSetReconcilerOption is a configuration option for RangeSetReconciler.
type RangeSetReconcilerOption func(r *RangeSetReconciler)

// WithMaxSendRange sets the maximum range size to send instead of further subdividing the
// input range.
func WithMaxSendRange(n int) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.maxSendRange = n
	}
}

// WithItemChunkSize sets the size of the item chunk to use when sending the set items.
func WithItemChunkSize(n int) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.itemChunkSize = n
	}
}

// WithSampleSize sets the size of the MinHash sample to be sent to the peer.
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

// WithLogger specifies the logger for RangeSetReconciler.
func WithLogger(log *zap.Logger) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.log = log
	}
}

// WithRecentTimeSpan specifies the time span for recent items.
func WithRecentTimeSpan(d time.Duration) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.recentTimeSpan = d
	}
}

// Tracer tracks the reconciliation process.
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

// WithTracer specifies a tracer for RangeSetReconciler.
func WithTracer(t Tracer) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.tracer = t
	}
}

// WithClock specifies the clock for RangeSetReconciler.
func WithClock(c clockwork.Clock) RangeSetReconcilerOption {
	return func(r *RangeSetReconciler) {
		r.clock = c
	}
}

// ProbeResult contains the result of a probe.
type ProbeResult struct {
	// Fingerprint of the range.
	FP any
	// Number of items in the range.
	Count int
	// An estimate of Jaccard similarity coefficient between the sets.
	// The range is 0..1, 0 being mostly disjoint sets and 1 being mostly equal sets.
	Sim float64
}

// RangeSetReconciler reconciles two sets of items using the recursive set reconciliation
// protocol.
type RangeSetReconciler struct {
	os             OrderedSet
	maxSendRange   int
	itemChunkSize  int
	sampleSize     int
	maxDiff        float64
	recentTimeSpan time.Duration
	tracer         Tracer
	clock          clockwork.Clock
	log            *zap.Logger
}

// NewRangeSetReconciler creates a new RangeSetReconciler.
func NewRangeSetReconciler(os OrderedSet, opts ...RangeSetReconcilerOption) *RangeSetReconciler {
	rsr := &RangeSetReconciler{
		os:            os,
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

func (rsr *RangeSetReconciler) defaultRange(ctx context.Context) (x, y types.KeyBytes, err error) {
	if empty, err := rsr.os.Empty(ctx); err != nil {
		return nil, nil, fmt.Errorf("checking for empty set: %w", err)
	} else if empty {
		return nil, nil, nil
	}

	seq, err := rsr.os.Items(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("get items: %w", err)
	}
	x, err = seq.First()
	if err != nil {
		return nil, nil, fmt.Errorf("get first item: %w", err)
	}

	return x, x, nil
}

func (rsr *RangeSetReconciler) processSubrange(s sender, x, y types.KeyBytes, info RangeInfo) error {
	rsr.log.Debug("processSubrange", HexField("x", x), HexField("y", y),
		zap.Int("count", info.Count), HexField("fingerprint", info.Fingerprint))

	if info.Count == 0 {
		// We have no more items in this subrange.
		// Ask peer to send any items it has in the range
		rsr.log.Debug("processSubrange: send empty range", HexField("x", x), HexField("y", y))
		if err := s.sendEmptyRange(x, y); err != nil {
			return err
		}
	}

	// The range is non-empty and large enough.
	// Send fingerprint so that the peer can further subdivide it.
	rsr.log.Debug("processSubrange: send fingerprint", HexField("x", x), HexField("y", y),
		zap.Int("count", info.Count))
	if err := s.sendFingerprint(x, y, info.Fingerprint, info.Count); err != nil {
		return err
	}

	return nil
}

func (rsr *RangeSetReconciler) splitRange(
	ctx context.Context,
	s sender,
	count int,
	x, y types.KeyBytes,
) error {
	count = count / 2
	rsr.log.Debug("handleMessage: PRE split range",
		HexField("x", x), HexField("y", y),
		zap.Int("countArg", count))
	si, err := rsr.os.SplitRange(ctx, x, y, count)
	if err != nil {
		return err
	}
	rsr.log.Debug("handleMessage: split range",
		HexField("x", x), HexField("y", y),
		zap.Int("countArg", count),
		zap.Int("count0", si.Parts[0].Count),
		HexField("fp0", si.Parts[0].Fingerprint),
		SeqField("start0", si.Parts[0].Items),
		zap.Int("count1", si.Parts[1].Count),
		HexField("fp1", si.Parts[1].Fingerprint),
		SeqField("start1", si.Parts[1].Items))
	if err := rsr.processSubrange(s, x, si.Middle, si.Parts[0]); err != nil {
		return err
	}
	if err := rsr.processSubrange(s, si.Middle, y, si.Parts[1]); err != nil {
		return err
	}
	return nil
}

func (rsr *RangeSetReconciler) sendSmallRange(
	s sender,
	count int,
	seq types.Seq,
	x, y types.KeyBytes,
) error {
	if count == 0 {
		rsr.log.Debug("handleMessage: empty incoming range",
			HexField("x", x), HexField("y", y))
		return s.sendEmptyRange(x, y)
	}
	rsr.log.Debug("handleMessage: send small range",
		HexField("x", x), HexField("y", y),
		zap.Int("count", count),
		zap.Int("maxSendRange", rsr.maxSendRange))
	if _, err := rsr.sendItems(s, count, seq, nil); err != nil {
		return err
	}
	return s.sendRangeContents(x, y, count)
}

func (rsr *RangeSetReconciler) sendItems(
	s sender,
	count int,
	seq types.Seq,
	skipKeys []types.KeyBytes,
) (int, error) {
	if count == 0 {
		return 0, nil
	}
	nSent := 0
	skipPos := 0
	if rsr.itemChunkSize == 0 {
		panic("BUG: zero item chunk size")
	}
	var keys []types.KeyBytes
	n := count
	for k, err := range seq {
		if err != nil {
			return nSent, err
		}
		for skipPos < len(skipKeys) {
			cmp := k.Compare(skipKeys[skipPos])
			if cmp == 0 {
				// we can skip this item. Advance skipPos as there are no duplicates
				skipPos++
				continue
			}
			if cmp < 0 {
				// current ley is yet to reach the skipped key at skipPos
				break
			}
			// current item is greater than the skipped key at skipPos,
			// so skipPos needs to catch up with the iterator
			skipPos++
		}
		if len(keys) == rsr.itemChunkSize {
			if err := s.sendChunk(keys); err != nil {
				return nSent, err
			}
			nSent += len(keys)
			keys = keys[:0]
		}
		keys = append(keys, k)
		n--
		if n == 0 {
			break
		}
	}
	if len(keys) != 0 {
		if err := s.sendChunk(keys); err != nil {
			return nSent, err
		}
		nSent += len(keys)
	}
	return nSent, nil
}

func (rsr *RangeSetReconciler) handleFingerprint(
	ctx context.Context,
	s sender,
	msg SyncMessage,
	x, y types.KeyBytes,
	info RangeInfo,
) (done bool, err error) {
	switch {
	case info.Fingerprint == msg.Fingerprint():
		// The range is synced
		return true, nil

	case msg.Type() == MessageTypeSample && rsr.maxDiff >= 0:
		// The peer has sent a sample of its items in the range to check if
		// recursive reconciliation approach is feasible.
		pr, err := rsr.handleSample(msg, info)
		if err != nil {
			return false, err
		}
		if 1-pr.Sim > rsr.maxDiff {
			rsr.tracer.OnDumbSync()
			rsr.log.Debug("handleMessage: maxDiff exceeded, sending full range",
				zap.Float64("sim", pr.Sim),
				zap.Float64("diff", 1-pr.Sim),
				zap.Float64("maxDiff", rsr.maxDiff))
			if _, err := rsr.sendItems(s, info.Count, info.Items, nil); err != nil {
				return false, err
			}
			return false, s.sendRangeContents(x, y, info.Count)
		}
		rsr.log.Debug("handleMessage: acceptable maxDiff, proceeding with sync",
			zap.Float64("sim", pr.Sim),
			zap.Float64("diff", 1-pr.Sim),
			zap.Float64("maxDiff", rsr.maxDiff))
		if info.Count > rsr.maxSendRange {
			return false, rsr.splitRange(ctx, s, info.Count, x, y)
		}
		return false, rsr.sendSmallRange(s, info.Count, info.Items, x, y)

	case info.Count <= rsr.maxSendRange:
		return false, rsr.sendSmallRange(s, info.Count, info.Items, x, y)

	default:
		return false, rsr.splitRange(ctx, s, info.Count, x, y)
	}
}

func (rsr *RangeSetReconciler) messageRange(
	ctx context.Context,
	msg SyncMessage,
) (x, y types.KeyBytes, err error) {
	x, y = msg.X(), msg.Y()
	if (x == nil || y == nil) && (x != nil && y != nil) {
		return nil, nil, fmt.Errorf("bad X or Y in a message of type %s", msg.Type())
	}
	switch msg.Type() {
	case MessageTypeEmptySet:
		if x != nil {
			return nil, nil, errors.New("EmptySet message should not contain a range")
		}
		return rsr.defaultRange(ctx)
	case MessageTypeProbe, MessageTypeRecent:
		if x == nil {
			return rsr.defaultRange(ctx)
		}
	default:
		if x == nil {
			return nil, nil, fmt.Errorf("no range for message of type %s", msg.Type())
		}
	}
	return x, y, nil
}

// handleMessage handles incoming messages. Note that the set reconciliation protocol is
// designed to be stateless.
func (rsr *RangeSetReconciler) handleMessage(
	ctx context.Context,
	s sender,
	msg SyncMessage,
	receivedKeys []types.KeyBytes,
) (done bool, err error) {
	rsr.log.Debug("handleMessage", zap.String("msg", SyncMessageToString(msg)))

	x, y, err := rsr.messageRange(ctx, msg)
	if err != nil {
		return false, err
	}

	if x == nil {
		switch msg.Type() {
		case MessageTypeProbe:
			rsr.log.Debug("handleMessage: send empty probe response")
			if err := s.sendSample(
				x, y, types.EmptyFingerprint(), 0, 0, types.EmptySeq(),
			); err != nil {
				return false, err
			}
		case MessageTypeRecent:
			rsr.tracer.OnRecent(len(receivedKeys), 0)
		}
		return true, nil
	}

	info, err := rsr.os.GetRangeInfo(ctx, x, y, -1)
	if err != nil {
		return false, err
	}
	rsr.log.Debug("handleMessage: range info",
		HexField("x", x), HexField("y", y),
		SeqField("items", info.Items),
		zap.Int("count", info.Count),
		HexField("fingerprint", info.Fingerprint))

	switch msg.Type() {
	case MessageTypeEmptyRange, MessageTypeRangeContents, MessageTypeEmptySet:
		// The peer has no more items to send in this range after this
		// message, as it is either empty or it has sent all of its
		// items in the range to us, but there may be some items on our
		// side. In the latter case, send only the items themselves b/c
		// the range doesn't need any further handling by the peer.
		if info.Count != 0 {
			rsr.log.Debug("handleMessage: send items", zap.Int("count", info.Count),
				SeqField("items", info.Items))
			if _, err := rsr.sendItems(s, info.Count, info.Items, receivedKeys); err != nil {
				return false, err
			}
			return false, nil
		}
		rsr.log.Debug("handleMessage: local range is empty")
		return true, nil

	case MessageTypeProbe:
		sampleSize := msg.Count()
		if sampleSize > maxSampleSize {
			return false, fmt.Errorf("bad minhash sample size %d (max %d)",
				msg.Count(), maxSampleSize)
		} else if sampleSize > info.Count {
			sampleSize = info.Count
		}
		items := info.Items
		if msg.Fingerprint() == info.Fingerprint {
			// no need to send MinHash items if fingerprints match
			items = types.EmptySeq()
			sampleSize = 0
		}
		if err := s.sendSample(x, y, info.Fingerprint, info.Count, sampleSize, items); err != nil {
			return false, err
		}
		return true, nil

	case MessageTypeRecent:
		it, count, err := rsr.os.Recent(ctx, msg.Since())
		if err != nil {
			return false, fmt.Errorf("error getting recent items: %w", err)
		}
		nSent := 0
		if count != 0 {
			// Do not send back recent items that were received
			if nSent, err = rsr.sendItems(s, count, it, receivedKeys); err != nil {
				return false, err
			}
		}
		rsr.log.Debug("handled recent message",
			zap.Int("receivedCount", len(receivedKeys)),
			zap.Int("sentCount", nSent))
		rsr.tracer.OnRecent(len(receivedKeys), nSent)
		return false, rsr.initiate(ctx, s, x, y, false)

	case MessageTypeFingerprint, MessageTypeSample:
		return rsr.handleFingerprint(ctx, s, msg, x, y, info)

	default:
		return false, fmt.Errorf("unexpected message type %s", msg.Type())
	}
}

// Initiate initiates the reconciliation process with the peer.
// If x and y are non-nil, [x, y) range is reconciled.  If x and y are nil, the whole
// range is reconciled.
func (rsr *RangeSetReconciler) Initiate(ctx context.Context, c Conduit, x, y types.KeyBytes) error {
	s := sender{c}
	if x == nil && y == nil {
		var err error
		x, y, err = rsr.defaultRange(ctx)
		if err != nil {
			return err
		}
	} else if x == nil || y == nil {
		panic("BUG: bad range")
	}
	haveRecent := rsr.recentTimeSpan > 0
	if err := rsr.initiate(ctx, s, x, y, haveRecent); err != nil {
		return err
	}
	return s.sendEndRound()
}

func (rsr *RangeSetReconciler) initiate(ctx context.Context, s sender, x, y types.KeyBytes, haveRecent bool) error {
	rsr.log.Debug("initiate", HexField("x", x), HexField("y", y))
	if x == nil {
		rsr.log.Debug("initiate: send empty set")
		return s.sendEmptySet()
	}
	info, err := rsr.os.GetRangeInfo(ctx, x, y, -1)
	if err != nil {
		return fmt.Errorf("get range info: %w", err)
	}
	switch {
	case info.Count == 0:
		rsr.log.Debug("initiate: send empty set")
		return s.sendEmptyRange(x, y)
	case info.Count < rsr.maxSendRange:
		rsr.log.Debug("initiate: send whole range", zap.Int("count", info.Count))
		if _, err := rsr.sendItems(s, info.Count, info.Items, nil); err != nil {
			return err
		}
		return s.sendRangeContents(x, y, info.Count)
	case haveRecent:
		rsr.log.Debug("initiate: checking recent items")
		since := rsr.clock.Now().Add(-rsr.recentTimeSpan)
		it, count, err := rsr.os.Recent(ctx, since)
		if err != nil {
			return fmt.Errorf("error getting recent items: %w", err)
		}
		if count != 0 {
			rsr.log.Debug("initiate: sending recent items", zap.Int("count", count))
			if n, err := rsr.sendItems(s, count, it, nil); err != nil {
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
		if err := s.sendRecent(since); err != nil {
			return err
		}
		return nil
	case rsr.maxDiff >= 0:
		// Use minhash to check if syncing this range is feasible
		rsr.log.Debug("initiate: send sample",
			zap.Int("count", info.Count),
			zap.Int("sampleSize", rsr.sampleSize))
		return s.sendSample(x, y, info.Fingerprint, info.Count, rsr.sampleSize, info.Items)
	default:
		rsr.log.Debug("initiate: send fingerprint", zap.Int("count", info.Count))
		return s.sendFingerprint(x, y, info.Fingerprint, info.Count)
	}
}

// InitiateProbe initiates a probe to retrieve the item count and Jaccard similarity
// coefficient from the peer.
func (rsr *RangeSetReconciler) InitiateProbe(
	ctx context.Context,
	c Conduit,
	x, y types.KeyBytes,
) (RangeInfo, error) {
	s := sender{c}
	info, err := rsr.os.GetRangeInfo(ctx, x, y, -1)
	if err != nil {
		return RangeInfo{}, err
	}
	if err := s.sendProbe(x, y, info.Fingerprint, rsr.sampleSize); err != nil {
		return RangeInfo{}, err
	}
	if err := s.sendEndRound(); err != nil {
		return RangeInfo{}, err
	}
	return info, nil
}

func (rsr *RangeSetReconciler) handleSample(
	msg SyncMessage,
	info RangeInfo,
) (pr ProbeResult, err error) {
	pr.FP = msg.Fingerprint()
	pr.Count = msg.Count()
	if info.Fingerprint == msg.Fingerprint() {
		pr.Sim = 1
	} else {
		localSample, err := Sample(info.Items, info.Count, rsr.sampleSize)
		if err != nil {
			return ProbeResult{}, fmt.Errorf("sampling local items: %w", err)
		}
		pr.Sim = CalcSim(localSample, msg.Sample())
	}
	return pr, nil
}

// HandleProbeResponse processes the probe response message and returns the probe result.
// info is the range info returned by InitiateProbe.
func (rsr *RangeSetReconciler) HandleProbeResponse(c Conduit, info RangeInfo) (pr ProbeResult, err error) {
	gotRange := false
	for {
		msg, err := c.NextMessage()
		switch {
		case err != nil:
			return ProbeResult{}, err
		case msg == nil:
			return ProbeResult{}, errors.New("no end round marker")
		default:
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
				pr, err = rsr.handleSample(msg, info)
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

var errNoEndMarker = errors.New("no end round marker")

var errEmptyRound = errors.New("empty round")

func (rsr *RangeSetReconciler) doRound(ctx context.Context, s sender) (done bool, err error) {
	done = true
	var receivedKeys []types.KeyBytes
	nHandled := 0
RECV_LOOP:
	for {
		msg, err := s.NextMessage()
		switch {
		case err != nil:
			return false, err
		case msg == nil:
			return false, errNoEndMarker
		}
		switch msg.Type() {
		case MessageTypeEndRound:
			break RECV_LOOP
		case MessageTypeDone:
			return true, nil
		case MessageTypeItemBatch:
			nHandled++
			for _, k := range msg.Keys() {
				// rsr.log.Debug("Process: add item", HexField("item", k))
				if err := rsr.os.Add(ctx, k); err != nil {
					return false, fmt.Errorf("error adding an item to the set: %w", err)
				}
				receivedKeys = append(receivedKeys, k)
			}
			continue
		}

		msgDone, err := rsr.handleMessage(ctx, s, msg, receivedKeys)
		if err != nil {
			return false, err
		}
		nHandled++
		if !msgDone {
			done = false
		}
		receivedKeys = receivedKeys[:0]
	}

	switch {
	case done:
		err = s.sendDone()
	case nHandled == 0:
		err = errEmptyRound
	default:
		err = s.sendEndRound()
	}

	if err != nil {
		return false, err
	}
	return done, nil
}

// Run performs sync reconciliation run using specified Conduit to send and receive
// messages.
func (rsr *RangeSetReconciler) Run(ctx context.Context, c Conduit) error {
	rsr.log.Debug("begin set reconciliation")
	defer rsr.log.Debug("end set reconciliation")
	s := sender{c}
	for {
		// Process() will receive all items and messages from the peer
		syncDone, err := rsr.doRound(ctx, s)
		if err != nil {
			return err
		} else if syncDone {
			return nil
		}
	}
}
