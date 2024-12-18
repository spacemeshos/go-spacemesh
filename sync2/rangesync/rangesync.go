package rangesync

import (
	"errors"
	"fmt"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	DefaultMaxSendRange  = 1
	DefaultItemChunkSize = 1024
	DefaultSampleSize    = 200
	maxSampleSize        = 1000
)

type RangeSetReconcilerConfig struct {
	// Maximum range size to send instead of further subdividing the input range.
	MaxSendRange uint `mapstructure:"max-send-range"`
	// Size of the item chunk to use when sending the set items.
	ItemChunkSize int `mapstructure:"item-chunk-size"`
	// Size of the MinHash sample to be sent to the peer.
	SampleSize uint `mapstructure:"sample-size"`
	// Maximum set difference metric (0..1) allowed for recursive reconciliation, with
	// value of 0 meaning equal sets and 1 meaning completely disjoint set. If the
	// difference metric MaxReconcDiff value, the whole set is transmitted instead of
	// applying the recursive algorithm.
	MaxReconcDiff float64 `mapstructure:"max-reconc-diff"`
	// Time span for recent sync.
	RecentTimeSpan time.Duration `mapstructure:"recent-time-span"`
	// Traffic limit in bytes.
	TrafficLimit int `mapstructure:"traffic-limit"`
	// Message count limit.
	MessageLimit int `mapstructure:"message-limit"`
}

func (cfg *RangeSetReconcilerConfig) Validate(logger *zap.Logger) bool {
	r := true
	if cfg.MaxSendRange == 0 {
		logger.Error("max-send-range must be positive")
		r = false
	}
	if cfg.ItemChunkSize == 0 {
		logger.Error("item-chunk-size must be positive")
		r = false
	}
	if cfg.SampleSize > maxSampleSize {
		logger.Error("bad sample-size", zap.Uint("sample-size", cfg.SampleSize), zap.Uint("max", maxSampleSize))
		r = false
	}
	if cfg.MaxReconcDiff < 0 || cfg.MaxReconcDiff > 1 {
		logger.Error("bad max-reconc-diff, should be within [0, 1] interval",
			zap.Float64("max-reconc-diff", cfg.MaxReconcDiff))
		r = false
	}
	return r
}

// DefaultConfig returns the default configuration for the RangeSetReconciler.
func DefaultConfig() RangeSetReconcilerConfig {
	return RangeSetReconcilerConfig{
		MaxSendRange:  DefaultMaxSendRange,
		ItemChunkSize: DefaultItemChunkSize,
		SampleSize:    DefaultSampleSize,
		MaxReconcDiff: 0.01,
		TrafficLimit:  300_000_000,
		MessageLimit:  20_000_000,
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

// ProbeResult contains the result of a probe.
type ProbeResult struct {
	// True if the peer's range (or full set) is fully in sync with the local range
	// (or full set).
	// Note that Sim==1 does not guarantee that the peer is in sync b/c simhash
	// algorithm is not precise.
	InSync bool
	// Number of items in the range.
	Count int
	// An estimate of Jaccard similarity coefficient between the sets.
	// The range is 0..1, 0 being mostly disjoint sets and 1 being mostly equal sets.
	Sim float64
}

// RangeSetReconciler reconciles two sets of items using the recursive set reconciliation
// protocol.
type RangeSetReconciler struct {
	os     OrderedSet
	cfg    RangeSetReconcilerConfig
	tracer Tracer
	clock  clockwork.Clock
	logger *zap.Logger
}

// NewRangeSetReconcilerInternal creates a new RangeSetReconciler.
// It is only directly called by the tests.
// It accepts extra tracer and clock parameters.
func NewRangeSetReconcilerInternal(
	logger *zap.Logger,
	cfg RangeSetReconcilerConfig,
	os OrderedSet,
	tracer Tracer,
	clock clockwork.Clock,
) *RangeSetReconciler {
	rsr := &RangeSetReconciler{
		os:     os,
		cfg:    cfg,
		tracer: tracer,
		clock:  clock,
		logger: logger,
	}
	if rsr.cfg.MaxSendRange <= 0 {
		panic("bad maxSendRange")
	}
	return rsr
}

// NewRangeSetReconciler creates a new RangeSetReconciler.
func NewRangeSetReconciler(logger *zap.Logger, cfg RangeSetReconcilerConfig, os OrderedSet) *RangeSetReconciler {
	return NewRangeSetReconcilerInternal(logger, cfg, os, nullTracer{}, clockwork.NewRealClock())
}

func (rsr *RangeSetReconciler) defaultRange() (x, y KeyBytes, err error) {
	if empty, err := rsr.os.Empty(); err != nil {
		return nil, nil, fmt.Errorf("checking for empty set: %w", err)
	} else if empty {
		return nil, nil, nil
	}

	x, err = rsr.os.Items().First()
	if err != nil {
		return nil, nil, fmt.Errorf("get items: %w", err)
	}

	return x, x, nil
}

func (rsr *RangeSetReconciler) processSubrange(s sender, x, y KeyBytes, info RangeInfo) error {
	rsr.logger.Debug("processSubrange", log.ZShortStringer("x", x), log.ZShortStringer("y", y),
		zap.Int("count", info.Count), log.ZShortStringer("fingerprint", info.Fingerprint))

	if info.Count == 0 {
		// We have no more items in this subrange.
		// Ask peer to send any items it has in the range
		rsr.logger.Debug("processSubrange: send empty range", log.ZShortStringer("x", x), log.ZShortStringer("y", y))
		if err := s.SendEmptyRange(x, y); err != nil {
			return fmt.Errorf("send empty range: %w", err)
		}
	}

	// The range is non-empty and large enough.
	// Send fingerprint so that the peer can further subdivide it.
	rsr.logger.Debug("processSubrange: send fingerprint", log.ZShortStringer("x", x), log.ZShortStringer("y", y),
		zap.Int("count", info.Count))
	if err := s.SendFingerprint(x, y, info.Fingerprint, info.Count); err != nil {
		return fmt.Errorf("send fingerprint: %w", err)
	}

	return nil
}

func (rsr *RangeSetReconciler) splitRange(s sender, count int, x, y KeyBytes) error {
	count = count / 2
	rsr.logger.Debug("handleMessage: PRE split range",
		log.ZShortStringer("x", x), log.ZShortStringer("y", y),
		zap.Int("countArg", count))
	si, err := rsr.os.SplitRange(x, y, count)
	if err != nil {
		return fmt.Errorf("split range: %w", err)
	}
	rsr.logger.Debug("handleMessage: split range",
		log.ZShortStringer("x", x), log.ZShortStringer("y", y),
		zap.Int("countArg", count),
		zap.Int("count0", si.Parts[0].Count),
		log.ZShortStringer("fp0", si.Parts[0].Fingerprint),
		zap.Array("start0", si.Parts[0].Items),
		zap.Int("count1", si.Parts[1].Count),
		log.ZShortStringer("fp1", si.Parts[1].Fingerprint),
		zap.Array("start1", si.Parts[1].Items))
	if err := rsr.processSubrange(s, x, si.Middle, si.Parts[0]); err != nil {
		return fmt.Errorf("process subrange after split: %w", err)
	}
	if err := rsr.processSubrange(s, si.Middle, y, si.Parts[1]); err != nil {
		return fmt.Errorf("process subrange after split: %w", err)
	}
	return nil
}

func (rsr *RangeSetReconciler) sendSmallRange(
	s sender,
	count int,
	sr SeqResult,
	x, y KeyBytes,
) error {
	if count == 0 {
		rsr.logger.Debug("handleMessage: empty incoming range",
			log.ZShortStringer("x", x), log.ZShortStringer("y", y))
		return s.SendEmptyRange(x, y)
	}
	rsr.logger.Debug("handleMessage: send small range",
		log.ZShortStringer("x", x), log.ZShortStringer("y", y),
		zap.Int("count", count),
		zap.Uint("maxSendRange", rsr.cfg.MaxSendRange))
	if _, err := rsr.sendItems(s, count, sr, nil); err != nil {
		return fmt.Errorf("send items: %w", err)
	}
	return s.SendRangeContents(x, y, count)
}

func (rsr *RangeSetReconciler) sendItems(
	s sender,
	count int,
	sr SeqResult,
	skipKeys map[string]struct{},
) (int, error) {
	if count == 0 {
		return 0, nil
	}
	nSent := 0
	if rsr.cfg.ItemChunkSize == 0 {
		panic("BUG: zero item chunk size")
	}
	var keys []KeyBytes
	n := count
	for k := range sr.Seq {
		if _, found := skipKeys[string(k)]; !found {
			if len(keys) == rsr.cfg.ItemChunkSize {
				if err := s.SendChunk(keys); err != nil {
					return nSent, err
				}
				nSent += len(keys)
				keys = keys[:0]
			}
			keys = append(keys, k)
		}
		n--
		if n == 0 {
			break
		}
	}
	if err := sr.Error(); err != nil {
		return nSent, err
	}

	if len(keys) != 0 {
		if err := s.SendChunk(keys); err != nil {
			return nSent, err
		}
		nSent += len(keys)
	}
	return nSent, nil
}

func (rsr *RangeSetReconciler) handleFingerprint(
	s sender,
	msg SyncMessage,
	x, y KeyBytes,
	info RangeInfo,
) (done bool, err error) {
	switch {
	case info.Fingerprint == msg.Fingerprint():
		// The range is synced
		return true, nil

	case msg.Type() == MessageTypeSample && rsr.cfg.MaxReconcDiff >= 0:
		// The peer has sent a sample of its items in the range to check if
		// recursive reconciliation approach is feasible.
		pr, err := rsr.handleSample(msg, info)
		if err != nil {
			return false, err
		}
		if 1-pr.Sim > rsr.cfg.MaxReconcDiff {
			rsr.tracer.OnDumbSync()
			rsr.logger.Debug("handleMessage: maxDiff exceeded, sending full range",
				zap.Float64("sim", pr.Sim),
				zap.Float64("diff", 1-pr.Sim),
				zap.Float64("maxDiff", rsr.cfg.MaxReconcDiff))
			if _, err := rsr.sendItems(s, info.Count, info.Items, nil); err != nil {
				return false, fmt.Errorf("send items: %w", err)
			}
			return false, s.SendRangeContents(x, y, info.Count)
		}
		rsr.logger.Debug("handleMessage: acceptable maxDiff, proceeding with sync",
			zap.Float64("sim", pr.Sim),
			zap.Float64("diff", 1-pr.Sim),
			zap.Float64("maxDiff", rsr.cfg.MaxReconcDiff))
		if uint(info.Count) > rsr.cfg.MaxSendRange {
			return false, rsr.splitRange(s, info.Count, x, y)
		}
		return false, rsr.sendSmallRange(s, info.Count, info.Items, x, y)

	case uint(info.Count) <= rsr.cfg.MaxSendRange:
		return false, rsr.sendSmallRange(s, info.Count, info.Items, x, y)

	default:
		return false, rsr.splitRange(s, info.Count, x, y)
	}
}

func (rsr *RangeSetReconciler) messageRange(
	msg SyncMessage,
) (x, y KeyBytes, err error) {
	x, y = msg.X(), msg.Y()
	if (x == nil || y == nil) && (x != nil && y != nil) {
		return nil, nil, fmt.Errorf("bad X or Y in a message of type %s", msg.Type())
	}
	switch msg.Type() {
	case MessageTypeEmptySet:
		if x != nil {
			return nil, nil, errors.New("EmptySet message should not contain a range")
		}
		return rsr.defaultRange()
	case MessageTypeProbe, MessageTypeRecent:
		if x == nil {
			return rsr.defaultRange()
		}
	default:
		if x == nil {
			return nil, nil, fmt.Errorf("no range for message of type %s", msg.Type())
		}
	}
	return x, y, nil
}

func (rsr *RangeSetReconciler) handleRecent(
	s sender,
	msg SyncMessage,
	x, y KeyBytes,
	receivedKeys map[string]struct{},
) error {
	since := msg.Since()
	if since.IsZero() {
		// This is a response to a Recent message with timestamp.
		// It is only needed so that we add the received items to the set
		// immediately, which is already done above.
		return nil
	}

	sr, count := rsr.os.Recent(msg.Since())
	nSent := 0
	if count != 0 {
		// Do not send back recent items that were received
		var err error
		if nSent, err = rsr.sendItems(s, count, sr, receivedKeys); err != nil {
			return fmt.Errorf("send items: %w", err)
		}
	}
	// Following the items, we send Recent message with zero time.
	// The peer will see this as the indicator that it needs to add the
	// received items to its set immediately, before proceeding with further
	// reconciliation steps.
	if err := s.SendRecent(time.Time{}); err != nil {
		return fmt.Errorf("sending recent: %w", err)
	}
	rsr.logger.Debug("handled recent message",
		zap.Int("receivedCount", len(receivedKeys)),
		zap.Int("sentCount", nSent))
	rsr.tracer.OnRecent(len(receivedKeys), nSent)
	return rsr.initiate(s, x, y, false)
}

// handleMessage handles incoming messages. Note that the set reconciliation protocol is
// designed to be stateless.
func (rsr *RangeSetReconciler) handleMessage(
	s sender,
	msg SyncMessage,
	receivedKeys map[string]struct{},
) (done bool, err error) {
	rsr.logger.Debug("handleMessage", zap.String("msg", SyncMessageToString(msg)))

	x, y, err := rsr.messageRange(msg)
	if err != nil {
		return false, err
	}

	if msg.Type() == MessageTypeRecent {
		for k := range receivedKeys {
			// Add received items to the set. Receive() was already
			// called on these items, but we also need them to
			// be present in the set before we proceed with further
			// reconciliation.
			if err := rsr.os.Add(KeyBytes(k)); err != nil {
				return false, fmt.Errorf("adding an item to the set: %w", err)
			}
		}
	}

	if x == nil {
		switch msg.Type() {
		case MessageTypeProbe:
			rsr.logger.Debug("handleMessage: send empty probe response")
			if err := s.SendSample(
				x, y, EmptyFingerprint(), 0, 0, EmptySeqResult(),
			); err != nil {
				return false, err
			}
		case MessageTypeRecent:
			return false, rsr.handleRecent(s, msg, x, y, receivedKeys)
		}
		return true, nil
	}

	info, err := rsr.os.GetRangeInfo(x, y)
	if err != nil {
		return false, err
	}
	rsr.logger.Debug("handleMessage: range info",
		log.ZShortStringer("x", x), log.ZShortStringer("y", y),
		zap.Array("items", info.Items),
		zap.Int("count", info.Count),
		log.ZShortStringer("fingerprint", info.Fingerprint))

	switch msg.Type() {
	case MessageTypeEmptyRange, MessageTypeRangeContents, MessageTypeEmptySet:
		// The peer has no more items to send in this range after this
		// message, as it is either empty or it has sent all of its
		// items in the range to us, but there may be some items on our
		// side. In the latter case, send only the items themselves b/c
		// the range doesn't need any further handling by the peer.
		if info.Count != 0 {
			rsr.logger.Debug("handleMessage: send items", zap.Int("count", info.Count),
				zap.Array("items", info.Items),
				zap.Int("receivedCount", len(receivedKeys)))
			nSent, err := rsr.sendItems(s, info.Count, info.Items, receivedKeys)
			if err != nil {
				return false, fmt.Errorf("send items: %w", err)
			}
			rsr.logger.Debug("handleMessage: sent items", zap.Int("count", nSent))
			return false, nil
		}
		rsr.logger.Debug("handleMessage: local range is empty")
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
			items = EmptySeqResult()
			sampleSize = 0
		}
		if err := s.SendSample(x, y, info.Fingerprint, info.Count, sampleSize, items); err != nil {
			return false, err
		}
		return true, nil

	case MessageTypeRecent:
		return false, rsr.handleRecent(s, msg, x, y, receivedKeys)

	case MessageTypeFingerprint, MessageTypeSample:
		return rsr.handleFingerprint(s, msg, x, y, info)

	default:
		return false, fmt.Errorf("unexpected message type %s", msg.Type())
	}
}

// Initiate initiates the reconciliation process with the peer.
// If x and y are non-nil, [x, y) range is reconciled.  If x and y are nil, the whole
// range is reconciled.
func (rsr *RangeSetReconciler) Initiate(c Conduit, x, y KeyBytes) error {
	s := sender{c}
	if x == nil && y == nil {
		var err error
		x, y, err = rsr.defaultRange()
		if err != nil {
			return err
		}
	} else if x == nil || y == nil {
		panic("BUG: bad range")
	}
	haveRecent := rsr.cfg.RecentTimeSpan > 0
	if err := rsr.initiate(s, x, y, haveRecent); err != nil {
		return err
	}
	return s.SendEndRound()
}

func (rsr *RangeSetReconciler) initiate(s sender, x, y KeyBytes, haveRecent bool) error {
	rsr.logger.Debug("initiate", log.ZShortStringer("x", x), log.ZShortStringer("y", y))
	if x == nil {
		rsr.logger.Debug("initiate: send empty set")
		return s.SendEmptySet()
	}
	info, err := rsr.os.GetRangeInfo(x, y)
	if err != nil {
		return fmt.Errorf("get range info: %w", err)
	}
	switch {
	case info.Count == 0:
		rsr.logger.Debug("initiate: send empty set")
		return s.SendEmptyRange(x, y)
	case uint(info.Count) < rsr.cfg.MaxSendRange:
		rsr.logger.Debug("initiate: send whole range", zap.Int("count", info.Count))
		if _, err := rsr.sendItems(s, info.Count, info.Items, nil); err != nil {
			return fmt.Errorf("send items: %w", err)
		}
		return s.SendRangeContents(x, y, info.Count)
	case haveRecent:
		rsr.logger.Debug("initiate: checking recent items")
		since := rsr.clock.Now().Add(-rsr.cfg.RecentTimeSpan)
		items, count := rsr.os.Recent(since)
		if count != 0 {
			rsr.logger.Debug("initiate: sending recent items", zap.Int("count", count))
			if n, err := rsr.sendItems(s, count, items, nil); err != nil {
				return fmt.Errorf("send recent items: %w", err)
			} else if n != count {
				panic("BUG: wrong number of items sent")
			}
		} else {
			rsr.logger.Debug("initiate: no recent items")
		}
		rsr.tracer.OnRecent(0, count)
		// Send Recent message even if there are no recent items, b/c we want to
		// receive recent items from the peer, if any.
		if err := s.SendRecent(since); err != nil {
			return fmt.Errorf("send recent message: %w", err)
		}
		return nil
	case rsr.cfg.MaxReconcDiff >= 0:
		// Use minhash to check if syncing this range is feasible
		rsr.logger.Debug("initiate: send sample",
			zap.Int("count", info.Count),
			zap.Uint("sampleSize", rsr.cfg.SampleSize))
		return s.SendSample(x, y, info.Fingerprint, info.Count, int(rsr.cfg.SampleSize), info.Items)
	default:
		rsr.logger.Debug("initiate: send fingerprint", zap.Int("count", info.Count))
		return s.SendFingerprint(x, y, info.Fingerprint, info.Count)
	}
}

// InitiateProbe initiates a probe to retrieve the item count and Jaccard similarity
// coefficient from the peer.
func (rsr *RangeSetReconciler) InitiateProbe(
	c Conduit,
	x, y KeyBytes,
) (RangeInfo, error) {
	s := sender{c}
	info, err := rsr.os.GetRangeInfo(x, y)
	if err != nil {
		return RangeInfo{}, err
	}
	if err := s.SendProbe(x, y, info.Fingerprint, int(rsr.cfg.SampleSize)); err != nil {
		return RangeInfo{}, err
	}
	if err := s.SendEndRound(); err != nil {
		return RangeInfo{}, err
	}
	return info, nil
}

func (rsr *RangeSetReconciler) handleSample(
	msg SyncMessage,
	info RangeInfo,
) (pr ProbeResult, err error) {
	pr.InSync = msg.Fingerprint() == info.Fingerprint
	pr.Count = msg.Count()
	if pr.InSync && pr.Count != info.Count {
		return ProbeResult{}, errors.New("mismatched count with matching fingerprint, possible collision")
	}
	if info.Fingerprint == msg.Fingerprint() {
		pr.Sim = 1
		return pr, nil
	}
	localSample, err := Sample(info.Items, info.Count, int(rsr.cfg.SampleSize))
	if err != nil {
		return ProbeResult{}, fmt.Errorf("sampling local items: %w", err)
	}
	pr.Sim = CalcSim(localSample, msg.Sample())
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

func (rsr *RangeSetReconciler) doRound(s sender) (done bool, err error) {
	done = true
	receivedKeys := make(map[string]struct{})
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
				if err := rsr.os.Receive(k); err != nil {
					return false, fmt.Errorf("adding an item to the set: %w", err)
				}
				receivedKeys[string(k)] = struct{}{}
			}
			continue
		}

		msgDone, err := rsr.handleMessage(s, msg, receivedKeys)
		if err != nil {
			return false, err
		}
		nHandled++
		if !msgDone {
			done = false
		}
		clear(receivedKeys)
	}

	switch {
	case done:
		err = s.SendDone()
	case nHandled == 0:
		err = errEmptyRound
	default:
		err = s.SendEndRound()
	}

	if err != nil {
		return false, err
	}
	return done, nil
}

// Run performs sync reconciliation run using specified Conduit to send and receive
// messages.
func (rsr *RangeSetReconciler) Run(c Conduit) error {
	rsr.logger.Debug("begin set reconciliation")
	defer rsr.logger.Debug("end set reconciliation")
	s := sender{c}
	for {
		// Process() will receive all items and messages from the peer
		syncDone, err := rsr.doRound(s)
		if err != nil {
			return err
		} else if syncDone {
			return nil
		}
	}
}
