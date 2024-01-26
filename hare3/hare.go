package hare3

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/layerpatrol"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/beacons"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	"github.com/spacemeshos/go-spacemesh/system"
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
		ProtocolName: "/h/3.0",
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

type nodeclock interface {
	AwaitLayer(types.LayerID) <-chan struct{}
	CurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}

func New(
	nodeclock nodeclock,
	pubsub pubsub.PublishSubsciber,
	db *datastore.CachedDB,
	verifier *signing.EdVerifier,
	oracle oracle,
	sync system.SyncStateProvider,
	patrol *layerpatrol.LayerPatrol,
	opts ...Opt,
) *Hare {
	ctx, cancel := context.WithCancel(context.Background())
	hr := &Hare{
		ctx:      ctx,
		cancel:   cancel,
		results:  make(chan ConsensusOutput, 32),
		coins:    make(chan WeakCoinOutput, 32),
		signers:  map[string]*signing.EdSigner{},
		sessions: map[types.LayerID]*protocol{},

		config:    DefaultConfig(),
		log:       zap.NewNop(),
		wallclock: clockwork.NewRealClock(),

		nodeclock: nodeclock,
		pubsub:    pubsub,
		db:        db,
		verifier:  verifier,
		oracle: &legacyOracle{
			log:    zap.NewNop(),
			oracle: oracle,
			config: DefaultConfig(),
		},
		sync:   sync,
		patrol: patrol,
		tracer: noopTracer{},
	}
	for _, opt := range opts {
		opt(hr)
	}
	return hr
}

type Hare struct {
	// state
	ctx      context.Context
	cancel   context.CancelFunc
	eg       errgroup.Group
	results  chan ConsensusOutput
	coins    chan WeakCoinOutput
	mu       sync.Mutex
	signers  map[string]*signing.EdSigner
	sessions map[types.LayerID]*protocol

	// options
	config    Config
	log       *zap.Logger
	wallclock clockwork.Clock

	// dependencies
	nodeclock nodeclock
	pubsub    pubsub.PublishSubsciber
	db        *datastore.CachedDB
	verifier  *signing.EdVerifier
	oracle    *legacyOracle
	sync      system.SyncStateProvider
	patrol    *layerpatrol.LayerPatrol
	tracer    Tracer
}

func (h *Hare) Register(signer *signing.EdSigner) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.log.Info("register signing key", zap.Stringer("node", signer.NodeID()))
	h.signers[string(signer.NodeID().Bytes())] = signer
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
				h.log.Debug("notified", zap.Uint32("lid", next.Uint32()))
				h.onLayer(next)
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
	if !h.verifier.Verify(signing.HARE, msg.Sender, msg.ToMetadata().ToBytes(), msg.Signature) {
		signatureError.Inc()
		return fmt.Errorf("%w: invalid signature", pubsub.ErrValidationReject)
	}
	malicious, err := h.db.IsMalicious(msg.Sender)
	if err != nil {
		maliciousError.Inc()
		return fmt.Errorf("database error %s", err.Error())
	}
	start := time.Now()
	g := h.oracle.validate(msg)
	oracleLatency.Observe(time.Since(start).Seconds())
	if g == grade0 {
		oracleError.Inc()
		return fmt.Errorf("zero grade")
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
		h.db.CacheMalfeasanceProof(equivocation.Messages[0].SmesherID, proof)
	}
	if !gossip {
		droppedMessages.Inc()
		return fmt.Errorf("dropped by graded gossip")
	}
	expected := h.nodeclock.LayerToTime(msg.Layer).Add(h.config.roundStart(msg.IterRound))
	metrics.ReportMessageLatency(h.config.ProtocolName, msg.Round.String(), time.Since(expected))
	return nil
}

func (h *Hare) onLayer(layer types.LayerID) {
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
		session.proto.OnInitial(h.proposals(session))
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
					return fmt.Errorf("terminated without result")
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

func (h *Hare) proposals(session *session) []types.ProposalID {
	h.log.Debug("requested proposals",
		zap.Uint32("lid", session.lid.Uint32()),
		zap.Stringer("beacon", session.beacon),
	)
	props, err := proposals.GetByLayer(h.db, session.lid)
	if err != nil {
		if errors.Is(err, sql.ErrNotFound) {
			h.log.Warn("no proposals found for hare, using empty set",
				zap.Uint32("lid", session.lid.Uint32()), zap.Error(err))
		} else {
			h.log.Error("failed to get proposals for hare",
				zap.Uint32("lid", session.lid.Uint32()), zap.Error(err))
		}
		return []types.ProposalID{}
	}
	var (
		beacon   types.Beacon
		result   []types.ProposalID
		min, own *types.ActivationTxHeader
	)
	for _, signer := range session.signers {
		own, err = h.db.GetEpochAtx(session.lid.GetEpoch()-1, signer.NodeID())
		if err != nil && !errors.Is(err, sql.ErrNotFound) {
			return []types.ProposalID{}
		}
		if min == nil || (min != nil && own != nil && own.TickHeight() < min.TickHeight()) {
			min = own
		}
	}
	if min == nil {
		h.log.Debug("no atxs in the requested epoch", zap.Uint32("epoch", session.lid.GetEpoch().Uint32()-1))
		return []types.ProposalID{}
	}
	atxs := map[types.ATXID]int{}
	for _, p := range props {
		atxs[p.AtxID]++
	}
	for _, p := range props {
		if p.IsMalicious() {
			h.log.Warn("not voting on proposal from malicious identity",
				zap.Stringer("id", p.ID()),
			)
			continue
		}
		if n := atxs[p.AtxID]; n > 1 {
			h.log.Warn("proposal with same atx added several times in the recorded set",
				zap.Int("n", n),
				zap.Stringer("id", p.ID()),
				zap.Stringer("atxid", p.AtxID),
			)
			continue
		}
		hdr, err := h.db.GetAtxHeader(p.AtxID)
		if err != nil {
			h.log.Error("atx is not loaded", zap.Error(err), zap.Stringer("atxid", p.AtxID))
			return []types.ProposalID{}
		}
		if hdr.BaseTickHeight >= min.TickHeight() {
			// does not vote for future proposal
			h.log.Warn("proposal base tick height too high. skipping",
				zap.Uint32("lid", session.lid.Uint32()),
				zap.Uint64("proposal_height", hdr.BaseTickHeight),
				zap.Uint64("own_height", own.TickHeight()),
			)
			continue
		}

		if p.EpochData != nil {
			beacon = p.EpochData.Beacon
		} else if p.RefBallot == types.EmptyBallotID {
			h.log.Error("empty refballot", zap.Stringer("id", p.Ballot.ID()))
			return []types.ProposalID{}
		} else if refBallot, err := ballots.Get(h.db, p.RefBallot); err != nil {
			h.log.Error("refballot not loaded", zap.Stringer("id", p.RefBallot), zap.Error(err))
			return []types.ProposalID{}
		} else if refBallot.EpochData == nil {
			h.log.Error("refballot with empty epoch data", zap.Stringer("id", refBallot.ID()))
			return []types.ProposalID{}
		} else {
			beacon = refBallot.EpochData.Beacon
		}
		if beacon == session.beacon {
			result = append(result, p.ID())
		} else {
			h.log.Warn("proposal has different beacon value",
				zap.Uint32("lid", session.lid.Uint32()),
				zap.Stringer("id", p.ID()),
				zap.String("proposal_beacon", beacon.ShortString()),
				zap.String("epoch_beacon", session.beacon.ShortString()),
			)
		}
	}
	return result
}

func (h *Hare) Stop() {
	h.cancel()
	h.eg.Wait()
	close(h.results)
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
