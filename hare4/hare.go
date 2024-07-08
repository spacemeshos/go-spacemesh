package hare4

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	gohash "hash"
	"io"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/aead/siphash"
	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/layerpatrol"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/beacons"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/system"
)

const PROTOCOL_NAME = "hare4_compacts"

var (
	errNoLayerProposals     = errors.New("no proposals for layer")
	errCannotMatchProposals = errors.New("cannot match proposals to compacted form")
)

type Config struct {
	Enable          bool          `mapstructure:"enable"`
	EnableLayer     types.LayerID `mapstructure:"enable-layer"`
	DisableLayer    types.LayerID `mapstructure:"disable-layer"`
	Committee       uint16        `mapstructure:"committee"`
	Leaders         uint16        `mapstructure:"leaders"`
	IterationsLimit uint8         `mapstructure:"iterations-limit"`
	PreroundDelay   time.Duration `mapstructure:"preround-delay"`
	RoundDuration   time.Duration `mapstructure:"round-duration"`
	// LogStats if true will log iteration statistics with INFO level at the start of the next iteration.
	// This requires additional computation and should be used for debugging only.
	LogStats     bool   `mapstructure:"log-stats"`
	ProtocolName string `mapstructure:"protocolname"`
}

func (cfg *Config) Validate(zdist time.Duration) error {
	terminates := cfg.roundStart(IterRound{Iter: cfg.IterationsLimit, Round: hardlock})
	if terminates > zdist {
		return fmt.Errorf("hare terminates later (%v) than expected (%v)", terminates, zdist)
	}
	if cfg.Enable && cfg.DisableLayer <= cfg.EnableLayer {
		return fmt.Errorf("disabled layer (%d) must be larger than enabled (%d)",
			cfg.DisableLayer, cfg.EnableLayer)
	}
	return nil
}

func (cfg *Config) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddBool("enabled", cfg.Enable)
	encoder.AddUint32("enabled layer", cfg.EnableLayer.Uint32())
	encoder.AddUint32("disabled layer", cfg.DisableLayer.Uint32())
	encoder.AddUint16("committee", cfg.Committee)
	encoder.AddUint16("leaders", cfg.Leaders)
	encoder.AddUint8("iterations limit", cfg.IterationsLimit)
	encoder.AddDuration("preround delay", cfg.PreroundDelay)
	encoder.AddDuration("round duration", cfg.RoundDuration)
	encoder.AddBool("log stats", cfg.LogStats)
	encoder.AddString("p2p protocol", cfg.ProtocolName)
	return nil
}

// roundStart returns expected time for iter/round relative to
// layer start.
func (cfg *Config) roundStart(round IterRound) time.Duration {
	if round.Round == 0 {
		return cfg.PreroundDelay
	}
	return cfg.PreroundDelay + time.Duration(round.Absolute()-1)*cfg.RoundDuration
}

func DefaultConfig() Config {
	return Config{
		// NOTE(talm) We aim for a 2^{-40} error probability; if the population at large has a 2/3 honest majority,
		// we need a committee of size ~800 to guarantee this error rate (at least,
		// this is what the Chernoff bound gives you; the actual value is a bit lower,
		// so we can probably get away with a smaller committee). For a committee of size 400,
		// the Chernoff bound gives 2^{-20} probability of a dishonest majority when 1/3 of the population is dishonest.
		Committee:       800,
		Leaders:         5,
		IterationsLimit: 4,
		PreroundDelay:   25 * time.Second,
		RoundDuration:   12 * time.Second,
		// can be bumped to 3.1 when oracle upgrades
		ProtocolName: "/h/4.0",
		DisableLayer: math.MaxUint32,
	}
}

type ConsensusOutput struct {
	Layer     types.LayerID
	Proposals []types.ProposalID
}

type WeakCoinOutput struct {
	Layer types.LayerID
	Coin  bool
}

type Opt func(*Hare)

func WithServer(s streamRequester) Opt {
	return func(hr *Hare) {
		hr.p2p = s
	}
}

func WithWallclock(clock clockwork.Clock) Opt {
	return func(hr *Hare) {
		hr.wallclock = clock
	}
}

func WithConfig(cfg Config) Opt {
	return func(hr *Hare) {
		hr.config = cfg
		hr.oracle.config = cfg
	}
}

func WithLogger(logger *zap.Logger) Opt {
	return func(hr *Hare) {
		hr.log = logger
		hr.oracle.log = logger
	}
}

func WithTracer(tracer Tracer) Opt {
	return func(hr *Hare) {
		hr.tracer = tracer
	}
}

// WithResultsChan overrides the default result channel with a different one.
// This is only needed for the migration period between hare3 and hare4.
func WithResultsChan(c chan ConsensusOutput) Opt {
	return func(hr *Hare) {
		hr.results = c
	}
}

type nodeclock interface {
	AwaitLayer(types.LayerID) <-chan struct{}
	CurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}

func New(
	nodeclock nodeclock,
	pubsub pubsub.PublishSubsciber,
	db *sql.Database,
	atxsdata *atxsdata.Data,
	proposals *store.Store,
	verif verifier,
	oracle oracle,
	sync system.SyncStateProvider,
	patrol *layerpatrol.LayerPatrol,
	host server.Host,
	opts ...Opt,
) *Hare {
	ctx, cancel := context.WithCancel(context.Background())
	hr := &Hare{
		ctx:          ctx,
		cancel:       cancel,
		results:      make(chan ConsensusOutput, 32),
		coins:        make(chan WeakCoinOutput, 32),
		signers:      make(map[string]*signing.EdSigner),
		sessions:     make(map[types.LayerID]*protocol),
		messageCache: make(map[types.Hash32]Message),

		config:    DefaultConfig(),
		log:       zap.NewNop(),
		wallclock: clockwork.NewRealClock(),

		nodeclock: nodeclock,
		pubsub:    pubsub,
		db:        db,
		atxsdata:  atxsdata,
		proposals: proposals,
		verifier:  verif,
		oracle: &legacyOracle{
			log:    zap.NewNop(),
			oracle: oracle,
			config: DefaultConfig(),
		},
		sync:   sync,
		patrol: patrol,
		hasherFn: func(b []byte) (gohash.Hash64, error) {
			return siphash.New64(b)
		},
		tracer: noopTracer{},
	}
	for _, opt := range opts {
		opt(hr)
	}

	if hr.p2p == nil {
		hr.p2p = server.New(host, PROTOCOL_NAME, hr.handleProposalsStream)
	}
	return hr
}

type Hare struct {
	// state
	ctx          context.Context
	cancel       context.CancelFunc
	eg           errgroup.Group
	results      chan ConsensusOutput
	coins        chan WeakCoinOutput
	mu           sync.Mutex
	signers      map[string]*signing.EdSigner
	sessions     map[types.LayerID]*protocol
	messageCache map[types.Hash32]Message

	// options
	config    Config
	log       *zap.Logger
	wallclock clockwork.Clock

	// dependencies
	nodeclock nodeclock
	pubsub    pubsub.PublishSubsciber
	db        *sql.Database
	atxsdata  *atxsdata.Data
	proposals *store.Store
	verifier  verifier

	hasherFn func([]byte) (gohash.Hash64, error)
	oracle   *legacyOracle
	sync     system.SyncStateProvider
	patrol   *layerpatrol.LayerPatrol
	p2p      streamRequester
	tracer   Tracer
}

func (h *Hare) Register(sig *signing.EdSigner) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.log.Info("registered signing key", log.ZShortStringer("id", sig.NodeID()))
	h.signers[string(sig.NodeID().Bytes())] = sig
}

func (h *Hare) Results() <-chan ConsensusOutput {
	return h.results
}

func (h *Hare) Coins() <-chan WeakCoinOutput {
	return h.coins
}

func (h *Hare) Start() {
	h.pubsub.Register(h.config.ProtocolName, h.Handler, pubsub.WithValidatorInline(true))
	current := h.nodeclock.CurrentLayer() + 1
	enabled := max(current, h.config.EnableLayer, types.GetEffectiveGenesis()+1)
	disabled := types.LayerID(math.MaxUint32)
	if h.config.DisableLayer > 0 {
		disabled = h.config.DisableLayer
	}
	h.log.Info("started",
		zap.Inline(&h.config),
		zap.Uint32("enabled", enabled.Uint32()),
		zap.Uint32("disabled", disabled.Uint32()),
	)
	h.eg.Go(func() error {
		for next := enabled; next < disabled; next++ {
			select {
			case <-h.nodeclock.AwaitLayer(next):
				h.log.Debug("notified", zap.Uint32("layer id", next.Uint32()))
				h.onLayer(next)
				h.cleanMessageCache(next - 1)
			case <-h.ctx.Done():
				return nil
			}
		}
		return nil
	})
}

func (h *Hare) Running() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessions)
}

// fetchCompacts will fetch the given compact proposal IDs from the provided peer
// and will return the array of the corresponding (full) proposal IDs.
// the compact proposals ids are expected to be de-duplicated.
func (h *Hare) fetchCompacts(ctx context.Context, peer p2p.Peer, msgId types.Hash32, vrf types.VrfSignature,
	compacts []types.CompactProposalID,
) ([]types.ProposalID, error) {
	requestCompactCounter.Inc()
	req := &CompactIdRequest{MsgId: msgId, Ids: compacts}
	reqBytes := codec.MustEncode(req)
	resp := &CompactIdResponse{}
	cb := func(ctx context.Context, rw io.ReadWriter) error {
		respLen, _, err := codec.DecodeLen(rw)
		if err != nil {
			return fmt.Errorf("decode length: %w", err)
		}
		buff := make([]byte, respLen)
		_, err = io.ReadFull(rw, buff)
		if err != nil {
			return fmt.Errorf("read response buffer: %w", err)
		}
		err = codec.Decode(buff, resp)
		if err != nil {
			return fmt.Errorf("decode compact id response: %w", err)
		}
		return nil
	}

	if err := h.streamRequest(ctx, peer, reqBytes, cb); err != nil {
		requestCompactErrorCounter.Inc()
		return nil, fmt.Errorf("stream request: %w", err)
	}

	return resp.Ids, nil
}

func (h *Hare) handleProposalsStream(ctx context.Context, msg []byte, s io.ReadWriter) error {
	requestCompactHandlerCounter.Inc()
	compactProps := &CompactIdRequest{}
	if err := codec.Decode(msg, compactProps); err != nil {
		malformedError.Inc()
		return fmt.Errorf("%w: decoding error %s", pubsub.ErrValidationReject, err.Error())
	}
	h.mu.Lock()
	m, ok := h.messageCache[compactProps.MsgId]
	h.mu.Unlock()
	if !ok {
		messageCacheMiss.Inc()
		return fmt.Errorf("handle proposal stream: can't find message %s", compactProps.MsgId)
	}
	resp := &CompactIdResponse{}
	if len(m.Body.Value.Proposals) == len(compactProps.Ids) {
		// special case - when we get the full list of compact proposals (without
		// deduplication, we return the full list of proposal ids. This is triggered
		// when the signature validation happens on the other end due to a possible
		// siphash collision
		resp.Ids = m.Body.Value.Proposals
	} else {
		for i, c := range m.Body.Value.Proposals {
			if slices.Contains(compactProps.Ids, m.Body.Value.CompactProposals[i]) {
				resp.Ids = append(resp.Ids, c)
			}
		}
	}

	respBytes := codec.MustEncode(resp)
	if _, err := codec.EncodeLen(s, uint32(len(respBytes))); err != nil {
		return fmt.Errorf("encode length: %w", err)
	}

	if _, err := s.Write(respBytes); err != nil {
		return fmt.Errorf("write response: %w", err)
	}

	return nil
}

func (h *Hare) streamRequest(ctx context.Context, peer p2p.Peer, req []byte, cb server.StreamRequestCallback) error {
	err := h.p2p.StreamRequest(ctx, peer, req, cb)
	if err != nil {
		return fmt.Errorf("stream request: %w", err)
	}
	return nil
}

func (h *Hare) reconstructProposals(ctx context.Context, peer p2p.Peer, msgId types.Hash32, msg *Message) error {
	proposals := h.proposals.GetForLayer(msg.Layer)
	if len(proposals) == 0 {
		return errNoLayerProposals // we might want to just force a full exchange on this edge case
	}
	hasher, err := h.hasherFn(msg.Eligibility.Proof[:16])
	if err != nil {
		return fmt.Errorf("create hasher: %w", err)
	}
	compacted := compactProposals(hasher, proposals)
	proposalIds := make([]proposalTuple, len(proposals))
	for i := range proposals {
		proposalIds[i] = proposalTuple{id: proposals[i].ID(), compact: compacted[i]}
	}
	slices.SortFunc(proposalIds, sortProposalsTuple)

	taken := make([]bool, len(proposals))
	findProp := func(id types.CompactProposalID) (bool, types.ProposalID) {
		for i := 0; i < len(proposalIds); i++ {
			if id != proposalIds[i].compact {
				continue
			}
			if taken[i] {
				continue
			}
			// item is both not taken and equals to looked up ID
			taken[i] = true
			return true, proposalIds[i].id
		}
		return false, types.EmptyProposalID
	}

	msg.Value.Proposals = make([]types.ProposalID, len(msg.Value.CompactProposals))
	var missing []types.CompactProposalID
	ctr := 0
	ll := len(msg.Value.CompactProposals)
	marked := make([]bool, ll)

	for i, compact := range msg.Value.CompactProposals {
		// try to see if we can match it to the proposals we have
		// if we do, add the proposal ID to the list of hashes in the proposals on the message
		if found, id := findProp(compact); found {
			msg.Value.Proposals[i] = id
			marked[i] = true
			ctr++
		} else {
			if !slices.Contains(missing, compact) {
				missing = append(missing, compact)
			}
		}
	}
	if ctr != len(msg.Value.CompactProposals) {
		// go into another round with a full message here
		messageCompactFetchCounter.Add(float64(len(missing)))
		missingCompacts, err := h.fetchCompacts(ctx, peer, msgId, msg.Eligibility.Proof, missing)
		if err != nil {
			return fmt.Errorf("fetch compacts: %w", err)
		}

		for i, v := range msg.Value.CompactProposals {
			if slices.Contains(missing, v) {
				if marked[i] {
					ctr--
				}
				marked[i] = false
			}
		}
		hasher, err := h.hasherFn(msg.Eligibility.Proof[:16])
		if err != nil {
			panic(err)
		}
		comp2 := compactProposalIds(hasher, missingCompacts)
	OUTER:
		for i, item := range comp2 {
			for j := 0; j < len(msg.Value.CompactProposals); j++ {
				if msg.Value.CompactProposals[j] == item && !marked[j] {
					msg.Value.Proposals[j] = missingCompacts[i]
					marked[j] = true
					ctr++
					continue OUTER
				}
			}
		}
	}

	if ctr != len(msg.Value.CompactProposals) {
		return errCannotMatchProposals
	}
	// sort the found proposals and unset the compact proposals
	// field before trying to check the signature
	// since it would add unnecessary data to the hasher
	slices.SortFunc(msg.Value.Proposals, sortProposalIds)
	msg.Value.CompactProposals = []types.CompactProposalID{}
	return nil
}

func (h *Hare) Handler(ctx context.Context, peer p2p.Peer, buf []byte) error {
	msg := &Message{}
	if err := codec.Decode(buf, msg); err != nil {
		malformedError.Inc()
		return fmt.Errorf("%w: decoding error %s", pubsub.ErrValidationReject, err.Error())
	}
	if err := msg.Validate(); err != nil {
		malformedError.Inc()
		return fmt.Errorf("%w: validation %s", pubsub.ErrValidationReject, err.Error())
	}
	h.tracer.OnMessageReceived(msg)
	h.mu.Lock()
	session, registered := h.sessions[msg.Layer]
	h.mu.Unlock()
	if !registered {
		notRegisteredError.Inc()
		return fmt.Errorf("layer %d is not registered", msg.Layer)
	}

	msgId := msg.ToHash()
	var compacts []types.CompactProposalID
	if msg.IterRound.Round == preround {
		// this will mutate the message to conform to the (hopefully)
		// original sent message for signature validation to occur
		compacts = msg.Value.CompactProposals
		messageCompactsCounter.Add(float64(len(compacts)))
		if err := h.reconstructProposals(ctx, peer, msgId, msg); err != nil {
			return fmt.Errorf("reconstruct proposals: %w", err)
		}
	}
	if !h.verifier.Verify(signing.HARE, msg.Sender, msg.ToMetadata().ToBytes(), msg.Signature) {
		if msg.IterRound.Round == preround {
			preroundSigFailCounter.Inc()
			// we might have a bad signature because of a local hash collision
			// of a proposal that has the same short hash that the node sent us.
			// in this case we try to ask for a full exchange of all full proposal
			// ids and try to validate again
			var err error
			msg.Body.Value.Proposals, err = h.fetchCompacts(ctx, peer, msgId, msg.Eligibility.Proof, compacts)
			if err != nil {
				return fmt.Errorf("signature verify: fetch compacts: %w", err)
			}
			if len(msg.Body.Value.Proposals) != len(compacts) {
				return fmt.Errorf("signature verify: proposals mismatch: %w", err)
			}
			if !h.verifier.Verify(signing.HARE, msg.Sender, msg.ToMetadata().ToBytes(), msg.Signature) {
				signatureError.Inc()
				return fmt.Errorf("%w: signature verify: invalid signature", pubsub.ErrValidationReject)
			}
		} else {
			signatureError.Inc()
			return fmt.Errorf("%w: invalid signature", pubsub.ErrValidationReject)
		}
	}

	if msg.IterRound.Round == preround {
		h.mu.Lock()
		if _, ok := h.messageCache[msgId]; !ok {
			newMsg := *msg
			newMsg.Body.Value.CompactProposals = compacts
			h.messageCache[msgId] = newMsg
		}
		h.mu.Unlock()
	}

	malicious := h.atxsdata.IsMalicious(msg.Sender)

	start := time.Now()
	g := h.oracle.validate(msg)
	oracleLatency.Observe(time.Since(start).Seconds())
	if g == grade0 {
		oracleError.Inc()
		return errors.New("zero grade")
	}
	start = time.Now()
	input := &input{
		Message:   msg,
		msgHash:   msg.ToHash(),
		malicious: malicious,
		atxgrade:  g,
	}
	h.log.Debug("on message", zap.Inline(input))
	gossip, equivocation := session.OnInput(input)
	h.log.Debug("after on message", log.ZShortStringer("hash", input.msgHash), zap.Bool("gossip", gossip))
	submitLatency.Observe(time.Since(start).Seconds())
	if equivocation != nil && !malicious {
		h.log.Debug("registered equivocation",
			zap.Uint32("lid", msg.Layer.Uint32()),
			zap.Stringer("sender", equivocation.Messages[0].SmesherID))
		proof := equivocation.ToMalfeasanceProof()
		if err := identities.SetMalicious(
			h.db, equivocation.Messages[0].SmesherID, codec.MustEncode(proof), time.Now()); err != nil {
			h.log.Error("failed to save malicious identity", zap.Error(err))
		}
		h.atxsdata.SetMalicious(equivocation.Messages[0].SmesherID)
	}
	if !gossip {
		droppedMessages.Inc()
		return errors.New("dropped by graded gossip")
	}
	expected := h.nodeclock.LayerToTime(msg.Layer).Add(h.config.roundStart(msg.IterRound))
	metrics.ReportMessageLatency(h.config.ProtocolName, msg.Round.String(), time.Since(expected))
	return nil
}

func (h *Hare) onLayer(layer types.LayerID) {
	h.proposals.OnLayer(layer)
	if !h.sync.IsSynced(h.ctx) {
		h.log.Debug("not synced", zap.Uint32("lid", layer.Uint32()))
		return
	}
	beacon, err := beacons.Get(h.db, layer.GetEpoch())
	if err != nil || beacon == types.EmptyBeacon {
		h.log.Debug("no beacon",
			zap.Uint32("epoch", layer.GetEpoch().Uint32()),
			zap.Uint32("lid", layer.Uint32()),
			zap.Error(err),
		)
		return
	}
	h.patrol.SetHareInCharge(layer)

	h.mu.Lock()
	// signer can't join mid session
	s := &session{
		lid:     layer,
		beacon:  beacon,
		signers: maps.Values(h.signers),
		vrfs:    make([]*types.HareEligibility, len(h.signers)),
		proto:   newProtocol(h.config.Committee/2 + 1),
	}
	h.sessions[layer] = s.proto
	h.mu.Unlock()

	sessionStart.Inc()
	h.tracer.OnStart(layer)
	h.log.Debug("registered layer", zap.Uint32("lid", layer.Uint32()))
	h.eg.Go(func() error {
		if err := h.run(s); err != nil {
			h.log.Warn("failed",
				zap.Uint32("lid", layer.Uint32()),
				zap.Error(err),
			)
			exitErrors.Inc()
			// if terminated successfully it will notify block generator
			// and it will have to CompleteHare
			h.patrol.CompleteHare(layer)
		} else {
			h.log.Debug("terminated",
				zap.Uint32("lid", layer.Uint32()),
			)
		}
		h.mu.Lock()
		delete(h.sessions, layer)
		h.mu.Unlock()
		sessionTerminated.Inc()
		h.tracer.OnStop(layer)
		return nil
	})
}

func (h *Hare) run(session *session) error {
	// oracle may load non-negligible amount of data from disk
	// we do it before preround starts, so that load can have some slack time
	// before it needs to be used in validation
	var (
		current = IterRound{Round: preround}
		start   = time.Now()
		active  bool
	)
	for i := range session.signers {
		session.vrfs[i] = h.oracle.active(session.signers[i], session.beacon, session.lid, current)
		active = active || session.vrfs[i] != nil
	}
	h.tracer.OnActive(session.vrfs)
	activeLatency.Observe(time.Since(start).Seconds())

	walltime := h.nodeclock.LayerToTime(session.lid).Add(h.config.PreroundDelay)
	if active {
		h.log.Debug("active in preround. waiting for preround delay", zap.Uint32("lid", session.lid.Uint32()))
		// initial set is not needed if node is not active in preround
		select {
		case <-h.wallclock.After(walltime.Sub(h.wallclock.Now())):
		case <-h.ctx.Done():
			return h.ctx.Err()
		}
		start := time.Now()
		session.proto.OnInitial(h.selectProposals(session))
		proposalsLatency.Observe(time.Since(start).Seconds())
	}
	if err := h.onOutput(session, current, session.proto.Next()); err != nil {
		return err
	}
	result := false
	for {
		walltime = walltime.Add(h.config.RoundDuration)
		current = session.proto.IterRound
		start = time.Now()

		for i := range session.signers {
			if current.IsMessageRound() {
				session.vrfs[i] = h.oracle.active(session.signers[i], session.beacon, session.lid, current)
			} else {
				session.vrfs[i] = nil
			}
		}
		h.tracer.OnActive(session.vrfs)
		activeLatency.Observe(time.Since(start).Seconds())

		select {
		case <-h.wallclock.After(walltime.Sub(h.wallclock.Now())):
			h.log.Debug("execute round",
				zap.Uint32("lid", session.lid.Uint32()),
				zap.Uint8("iter", session.proto.Iter), zap.Stringer("round", session.proto.Round),
				zap.Bool("active", active),
			)
			out := session.proto.Next()
			if out.result != nil {
				result = true
			}
			if err := h.onOutput(session, current, out); err != nil {
				return err
			}
			// we are logginng stats 1 network delay after new iteration start
			// so that we can receive notify messages from previous iteration
			if session.proto.Round == softlock && h.config.LogStats {
				h.log.Info("stats", zap.Uint32("lid", session.lid.Uint32()), zap.Inline(session.proto.Stats()))
			}
			if out.terminated {
				if !result {
					return errors.New("terminated without result")
				}
				return nil
			}
			if current.Iter == h.config.IterationsLimit {
				return fmt.Errorf("hare failed to reach consensus in %d iterations",
					h.config.IterationsLimit)
			}
		case <-h.ctx.Done():
			return nil
		}
	}
}

func (h *Hare) onOutput(session *session, ir IterRound, out output) error {
	for i, vrf := range session.vrfs {
		if vrf == nil || out.message == nil {
			continue
		}
		msg := *out.message // shallow copy
		msg.Layer = session.lid
		msg.Eligibility = *vrf
		msg.Sender = session.signers[i].NodeID()
		msg.Signature = session.signers[i].Sign(signing.HARE, msg.ToMetadata().ToBytes())
		if ir.Round == preround {
			hasher, err := h.hasherFn(msg.Eligibility.Proof[:16])
			if err != nil {
				panic(err)
			}

			msg.Body.Value.CompactProposals = compactProposalIds(hasher, out.message.Body.Value.Proposals)
			fullProposals := msg.Body.Value.Proposals
			msg.Body.Value.Proposals = []types.ProposalID{}
			id := msg.ToHash()
			msg.Body.Value.Proposals = fullProposals
			h.mu.Lock()
			h.messageCache[id] = msg
			h.mu.Unlock()
			msg.Body.Value.Proposals = []types.ProposalID{}
		}
		if err := h.pubsub.Publish(h.ctx, h.config.ProtocolName, msg.ToBytes()); err != nil {
			h.log.Error("failed to publish", zap.Inline(&msg), zap.Error(err))
		}
	}
	h.tracer.OnMessageSent(out.message)
	h.log.Debug("round output",
		zap.Uint32("lid", session.lid.Uint32()),
		zap.Uint8("iter", ir.Iter), zap.Stringer("round", ir.Round),
		zap.Inline(&out),
	)
	if out.coin != nil {
		select {
		case <-h.ctx.Done():
			return h.ctx.Err()
		case h.coins <- WeakCoinOutput{Layer: session.lid, Coin: *out.coin}:
		}
		sessionCoin.Inc()
	}
	if out.result != nil {
		select {
		case <-h.ctx.Done():
			return h.ctx.Err()
		case h.results <- ConsensusOutput{Layer: session.lid, Proposals: out.result}:
		}
		sessionResult.Inc()
	}
	return nil
}

func (h *Hare) selectProposals(session *session) []types.ProposalID {
	h.log.Debug("requested proposals",
		zap.Uint32("lid", session.lid.Uint32()),
		zap.Stringer("beacon", session.beacon),
	)

	var (
		result []types.ProposalID
		min    *atxsdata.ATX
	)
	target := session.lid.GetEpoch()
	publish := target - 1
	for _, signer := range session.signers {
		atxid, err := atxs.GetIDByEpochAndNodeID(h.db, publish, signer.NodeID())
		switch {
		case errors.Is(err, sql.ErrNotFound):
			// if atx is not registered for identity we will get sql.ErrNotFound
		case err != nil:
			h.log.Error("failed to get atx id by epoch and node id", zap.Error(err))
			return []types.ProposalID{}
		default:
			own := h.atxsdata.Get(target, atxid)
			if min == nil || (min != nil && own != nil && own.Height < min.Height) {
				min = own
			}
		}
	}
	if min == nil {
		h.log.Debug("no atxs in the requested epoch", zap.Uint32("epoch", session.lid.GetEpoch().Uint32()-1))
		return []types.ProposalID{}
	}

	candidates := h.proposals.GetForLayer(session.lid)
	atxs := map[types.ATXID]int{}
	for _, p := range candidates {
		atxs[p.AtxID]++
	}
	for _, p := range candidates {
		if h.atxsdata.IsMalicious(p.SmesherID) || p.IsMalicious() {
			h.log.Warn("not voting on proposal from malicious identity",
				zap.Stringer("id", p.ID()),
			)
			continue
		}
		// double check that a single smesher is not included twice
		// theoretically it should never happen as it is covered
		// by the malicious check above.
		if n := atxs[p.AtxID]; n > 1 {
			h.log.Error("proposal with same atx added several times in the recorded set",
				zap.Int("n", n),
				zap.Stringer("id", p.ID()),
				zap.Stringer("atxid", p.AtxID),
			)
			continue
		}
		header := h.atxsdata.Get(target, p.AtxID)
		if header == nil {
			h.log.Error("atx is not loaded", zap.Stringer("atxid", p.AtxID))
			return []types.ProposalID{}
		}
		if header.BaseHeight >= min.Height {
			// does not vote for future proposal
			h.log.Warn("proposal base tick height too high. skipping",
				zap.Uint32("lid", session.lid.Uint32()),
				zap.Uint64("proposal_height", header.BaseHeight),
				zap.Uint64("min_height", min.Height),
			)
			continue
		}

		if p.Beacon() == session.beacon {
			result = append(result, p.ID())
		} else {
			h.log.Warn("proposal has different beacon value",
				zap.Uint32("lid", session.lid.Uint32()),
				zap.Stringer("id", p.ID()),
				zap.String("proposal_beacon", p.Beacon().ShortString()),
				zap.String("epoch_beacon", session.beacon.ShortString()),
			)
		}
	}
	return result
}

func (h *Hare) IsKnown(layer types.LayerID, proposal types.ProposalID) bool {
	return h.proposals.Get(layer, proposal) != nil
}

// OnProposal is a hook which gets called when we get a proposal.
func (h *Hare) OnProposal(p *types.Proposal) error {
	return h.proposals.Add(p)
}

func (h *Hare) cleanMessageCache(l types.LayerID) {
	var keys []types.Hash32
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, item := range h.messageCache {
		if item.Layer < l {
			// mark key for deletion
			keys = append(keys, k)
		}
	}
	for _, v := range keys {
		delete(h.messageCache, v)
	}
}

func (h *Hare) Stop() {
	h.cancel()
	h.eg.Wait()
	close(h.coins)
	h.log.Info("stopped")
}

type session struct {
	proto   *protocol
	lid     types.LayerID
	beacon  types.Beacon
	signers []*signing.EdSigner
	vrfs    []*types.HareEligibility
}

func compactProposal(h gohash.Hash64, workSlice []byte, p types.ProposalID) (c types.CompactProposalID) {
	h.Write(p[:])
	binary.LittleEndian.PutUint64(workSlice, h.Sum64())
	copy(c[:], workSlice[:4])
	return c
}

func compactProposals(h gohash.Hash64, proposals []*types.Proposal) []types.CompactProposalID {
	workSlice := make([]byte, 8)
	compactProposals := make([]types.CompactProposalID, len(proposals))
	for i, prop := range proposals {
		compactProposals[i] = compactProposal(h, workSlice, prop.ID())
		h.Reset()
	}
	return compactProposals
}

func compactProposalIds(h gohash.Hash64, proposals []types.ProposalID) []types.CompactProposalID {
	workSlice := make([]byte, 8)
	compactProposals := make([]types.CompactProposalID, len(proposals))
	for i, prop := range proposals {
		compactProposals[i] = compactProposal(h, workSlice, prop)
		h.Reset()
	}
	return compactProposals
}

type proposalTuple struct {
	id      types.ProposalID
	compact types.CompactProposalID
}

func sortProposalsTuple(i, j proposalTuple) int {
	return sortProposalIds(i.id, j.id)
}

func sortProposalIds(i, j types.ProposalID) int {
	return bytes.Compare(i[:], j[:])
}
