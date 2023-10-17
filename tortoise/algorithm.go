package tortoise

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Config for protocol parameters.
type Config struct {
	Hdist uint32 `mapstructure:"tortoise-hdist"` // hare output lookback distance
	Zdist uint32 `mapstructure:"tortoise-zdist"` // hare result wait distance
	// how long we are waiting for a switch from verifying to full. relevant during rerun.
	WindowSize    uint32 `mapstructure:"tortoise-window-size"`    // size of the tortoise sliding window (in layers)
	MaxExceptions int    `mapstructure:"tortoise-max-exceptions"` // if candidate for base ballot has more than max exceptions it will be ignored
	// number of layers to delay votes for blocks with bad beacon values during self-healing. ideally a full epoch.
	BadBeaconVoteDelayLayers uint32 `mapstructure:"tortoise-delay-layers"`
	// EnableTracer will write tortoise traces to the stderr.
	EnableTracer bool `mapstructure:"tortoise-enable-tracer"`
	// MinimalActiveSetWeight denotes weight that will replace weight
	// recorded in the first ballot, if that weight is less than minimal
	// for purposes of eligibility computation.
	MinimalActiveSetWeight uint64 `mapstructure:"tortoise-activeset-weight"`

	LayerSize uint32
}

// DefaultConfig for Tortoise.
func DefaultConfig() Config {
	return Config{
		LayerSize:                50,
		Hdist:                    10,
		Zdist:                    8,
		WindowSize:               1000,
		BadBeaconVoteDelayLayers: 0,
		MaxExceptions:            50 * 100, // 100 layers of average size
	}
}

// Tortoise is a thread safe verifying tortoise wrapper, it just locks all actions.
type Tortoise struct {
	logger *zap.Logger
	ctx    context.Context
	cfg    Config

	mu     sync.Mutex
	trtl   *turtle
	tracer *tracer
}

// Opt for configuring tortoise.
type Opt func(t *Tortoise)

// WithLogger defines logger for tortoise.
func WithLogger(logger log.Log) Opt {
	return func(t *Tortoise) {
		t.logger = logger.Zap()
	}
}

// WithConfig defines protocol parameters.
func WithConfig(cfg Config) Opt {
	return func(t *Tortoise) {
		t.cfg = cfg
	}
}

// WithTracer enables tracing of every call to the tortoise.
func WithTracer(opts ...TraceOpt) Opt {
	return func(t *Tortoise) {
		t.tracer = newTracer(opts...)
	}
}

// New creates Tortoise instance.
func New(opts ...Opt) (*Tortoise, error) {
	t := &Tortoise{
		ctx:    context.Background(),
		logger: log.NewNop().Zap(),
		cfg:    DefaultConfig(),
	}
	for _, opt := range opts {
		opt(t)
	}
	if t.cfg.Hdist < t.cfg.Zdist {
		t.logger.Panic("hdist must be >= zdist",
			zap.Uint32("hdist", t.cfg.Hdist),
			zap.Uint32("zdist", t.cfg.Zdist),
		)
	}
	if t.cfg.WindowSize == 0 {
		t.logger.Panic("tortoise-window-size should not be zero")
	}
	t.trtl = newTurtle(t.logger, t.cfg)
	if t.tracer != nil {
		t.tracer.On(&ConfigTrace{
			Hdist:                    t.cfg.Hdist,
			Zdist:                    t.cfg.Zdist,
			WindowSize:               t.cfg.WindowSize,
			MaxExceptions:            uint32(t.cfg.MaxExceptions),
			BadBeaconVoteDelayLayers: t.cfg.BadBeaconVoteDelayLayers,
			LayerSize:                t.cfg.LayerSize,
			EpochSize:                types.GetLayersPerEpoch(),
			EffectiveGenesis:         types.GetEffectiveGenesis().Uint32(),
		})
	}
	return t, nil
}

func (t *Tortoise) RecoverFrom(lid types.LayerID, opinion, prev types.Hash32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logger.Debug("recover from",
		zap.Uint32("lid", lid.Uint32()),
		log.ZShortStringer("opinion", opinion),
		log.ZShortStringer("prev opinion", prev),
	)
	t.trtl.evicted = lid - 1
	t.trtl.pending = lid
	t.trtl.verified = lid
	t.trtl.processed = lid
	t.trtl.last = lid
	layer := t.trtl.layer(lid)
	layer.opinion = opinion
	layer.prevOpinion = &prev
}

// LatestComplete returns the latest verified layer.
func (t *Tortoise) LatestComplete() types.LayerID {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.trtl.verified
}

func (t *Tortoise) OnWeakCoin(lid types.LayerID, coin bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logger.Debug("on weakcoin",
		zap.Uint32("layer_id", lid.Uint32()),
		zap.Uint32("evicted", t.trtl.evicted.Uint32()),
		zap.Bool("coin", coin),
	)
	if lid <= t.trtl.evicted {
		return
	}
	layer := t.trtl.layer(lid)
	if coin {
		layer.coinflip = support
	} else {
		layer.coinflip = against
	}
	if t.tracer != nil {
		t.tracer.On(&WeakCoinTrace{Layer: lid, Coin: coin})
	}
}

// OnMalfeasance registers node id as malfeasent.
// - ballots from this id will have zero weight
// - atxs - will not be counted towards global/local threhsolds
// If node registers equivocating ballot/atx it should
// call OnMalfeasance before storing ballot/atx.
func (t *Tortoise) OnMalfeasance(id types.NodeID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.trtl.isMalfeasant(id) {
		return
	}
	t.logger.Debug("on malfeasence", zap.Stringer("id", id))
	t.trtl.makrMalfeasant(id)
	malfeasantNumber.Inc()
	if t.tracer != nil {
		t.tracer.On(&MalfeasanceTrace{ID: id})
	}
}

func (t *Tortoise) OnBeacon(eid types.EpochID, beacon types.Beacon) {
	t.mu.Lock()
	defer t.mu.Unlock()
	firstInWindow := t.trtl.evicted.Add(1).GetEpoch()
	t.logger.Debug("on beacon",
		zap.Uint32("epoch id", eid.Uint32()),
		zap.Uint32("first epoch", firstInWindow.Uint32()),
		zap.Stringer("beacon", beacon),
	)
	if eid < firstInWindow {
		return
	}
	epoch := t.trtl.epoch(eid)
	epoch.beacon = &beacon
	if t.tracer != nil {
		t.tracer.On(&BeaconTrace{Epoch: eid, Beacon: beacon})
	}
}

type encodeConf struct {
	current *types.LayerID
}

// EncodeVotesOpts is for configuring EncodeVotes options.
type EncodeVotesOpts func(*encodeConf)

// EncodeVotesWithCurrent changes last known layer that will be used for encoding votes.
//
// NOTE(dshulyak) why do we need this?
// tortoise computes threshold from last non-verified till the last known layer,
// since we dont download atxs before starting tortoise we won't be able to compute threshold
// based on the last clock layer (see https://github.com/spacemeshos/go-spacemesh/issues/3003)
func EncodeVotesWithCurrent(current types.LayerID) EncodeVotesOpts {
	return func(conf *encodeConf) {
		conf.current = &current
	}
}

// EncodeVotes chooses a base ballot and creates a differences list. needs the hare results for latest layers.
func (t *Tortoise) EncodeVotes(
	ctx context.Context,
	opts ...EncodeVotesOpts,
) (*types.Opinion, error) {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitEncodeVotes.Observe(float64(time.Since(start).Nanoseconds()))
	start = time.Now()
	conf := &encodeConf{}
	for _, opt := range opts {
		opt(conf)
	}
	opinion, err := t.trtl.EncodeVotes(ctx, conf)
	executeEncodeVotes.Observe(float64(time.Since(start).Nanoseconds()))
	if err != nil {
		errorsCounter.Inc()
	}
	if t.tracer != nil {
		event := &EncodeVotesTrace{
			Opinion: opinion,
		}
		if err != nil {
			event.Error = err.Error()
		}
		if conf.current != nil {
			event.Layer = *conf.current
		} else {
			event.Layer = t.trtl.last + 1
		}
		t.tracer.On(event)
	}
	return opinion, err
}

// TallyVotes up to the specified layer.
func (t *Tortoise) TallyVotes(ctx context.Context, lid types.LayerID) {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitTallyVotes.Observe(float64(time.Since(start).Nanoseconds()))
	start = time.Now()
	t.trtl.onLayer(ctx, lid)
	executeTallyVotes.Observe(float64(time.Since(start).Nanoseconds()))
	if t.tracer != nil {
		t.tracer.On(&TallyTrace{Layer: lid})
	}
}

// OnAtx is expected to be called before ballots that use this atx.
func (t *Tortoise) OnAtx(header *types.AtxTortoiseData) {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitAtxDuration.Observe(float64(time.Since(start).Nanoseconds()))
	t.trtl.onAtx(header)
	if t.tracer != nil {
		t.tracer.On(&AtxTrace{Header: header})
	}
}

// OnBlock updates tortoise with information that data is available locally.
func (t *Tortoise) OnBlock(header types.BlockHeader) {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitBlockDuration.Observe(float64(time.Since(start).Nanoseconds()))
	t.trtl.onBlock(header, true, false)
	if t.tracer != nil {
		t.tracer.On(&BlockTrace{Header: header})
	}
}

// OnValidBlock inserts block, updates that data is stored locally
// and that block was previously considered valid by tortoise.
func (t *Tortoise) OnValidBlock(header types.BlockHeader) {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitBlockDuration.Observe(float64(time.Since(start).Nanoseconds()))
	t.trtl.onBlock(header, true, true)
	if t.tracer != nil {
		t.tracer.On(&BlockTrace{Header: header, Valid: true})
	}
}

// OnBallot should be called every time new ballot is received.
// Dependencies (base ballot, ref ballot, active set and its own atx) must
// be processed before ballot.
func (t *Tortoise) OnBallot(ballot *types.BallotTortoiseData) {
	t.mu.Lock()
	defer t.mu.Unlock()
	err := t.trtl.onBallot(ballot)
	if err != nil {
		errorsCounter.Inc()
		t.logger.Error("failed to save state from ballot",
			zap.Stringer("ballot", ballot.ID),
			zap.Error(err))
	}
	if t.tracer != nil {
		t.tracer.On(&BallotTrace{Ballot: ballot})
	}
}

// OnRecoveredBallot is called for ballots recovered from database.
//
// For recovered ballots base ballot is not required to be in state therefore
// opinion is not recomputed, but instead recovered from database state.
func (t *Tortoise) OnRecoveredBallot(ballot *types.BallotTortoiseData) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.trtl.onRecoveredBallot(ballot); err != nil {
		errorsCounter.Inc()
		t.logger.Error("failed to save state from recovered ballot",
			zap.Stringer("ballot", ballot.ID),
			zap.Error(err))
	}
	if t.tracer != nil {
		t.tracer.On(&BallotTrace{Ballot: ballot})
	}
}

// DecodedBallot created after unwrapping exceptions list and computing internal opinion.
type DecodedBallot struct {
	*types.BallotTortoiseData
	info *ballotInfo
	// after validation is finished we need to add new vote targets
	// for tortoise from the decoded votes. minHint identifies the boundary
	// until which we have to scan.
	minHint types.LayerID
}

type BallotData struct {
	ID           types.BallotID
	Layer        types.LayerID
	ATXID        types.ATXID
	Smesher      types.NodeID
	Beacon       types.Beacon
	Eligiblities uint32
}

func (t *Tortoise) GetBallot(id types.BallotID) *BallotData {
	t.mu.Lock()
	defer t.mu.Unlock()
	info := t.trtl.ballotRefs[id]
	if info == nil {
		return nil
	}
	return &BallotData{
		ID:           id,
		Layer:        info.layer,
		ATXID:        info.reference.atxid,
		Smesher:      info.reference.smesher,
		Beacon:       info.reference.beacon,
		Eligiblities: info.reference.expectedBallots,
	}
}

// DecodeBallot decodes ballot if it wasn't processed earlier.
func (t *Tortoise) DecodeBallot(ballot *types.BallotTortoiseData) (*DecodedBallot, error) {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitBallotDuration.Observe(float64(time.Since(start).Nanoseconds()))
	decoded, err := t.decodeBallot(ballot)
	if t.tracer != nil {
		ev := &DecodeBallotTrace{Ballot: ballot}
		if err != nil {
			ev.Error = err.Error()
		}
		t.tracer.On(ev)
	}
	return decoded, err
}

func (t *Tortoise) decodeBallot(ballot *types.BallotTortoiseData) (*DecodedBallot, error) {
	info, min, err := t.trtl.decodeBallot(ballot)
	if err != nil {
		errorsCounter.Inc()
		return nil, err
	}
	if info == nil {
		errorsCounter.Inc()
		return nil, fmt.Errorf("can't decode ballot %s", ballot.ID)
	}
	if info.opinion() != ballot.Opinion.Hash {
		errorsCounter.Inc()
		return nil, fmt.Errorf(
			"computed opinion hash %s doesn't match signed %s for ballot %d / %s",
			info.opinion().
				ShortString(),
			ballot.Opinion.Hash.ShortString(),
			ballot.Layer,
			ballot.ID,
		)
	}
	return &DecodedBallot{BallotTortoiseData: ballot, info: info, minHint: min}, nil
}

// StoreBallot stores previously decoded ballot.
func (t *Tortoise) StoreBallot(decoded *DecodedBallot) error {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitBallotDuration.Observe(float64(time.Since(start).Nanoseconds()))
	if decoded.Malicious {
		decoded.info.malicious = true
	}
	err := t.trtl.storeBallot(decoded.info, decoded.minHint)
	if t.tracer != nil {
		ev := &StoreBallotTrace{ID: decoded.ID, Malicious: decoded.Malicious}
		if err != nil {
			ev.Error = err.Error()
		}
		t.tracer.On(ev)
	}
	return err
}

// OnHareOutput should be called when hare terminated or certificate for a block
// was synced from a peer.
// This method is expected to be called any number of times, with layers in any order.
//
// This method should be called with EmptyBlockID if node received proof of malicious behavior,
// such as signing same block id by members of the same committee.
func (t *Tortoise) OnHareOutput(lid types.LayerID, bid types.BlockID) {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitHareOutputDuration.Observe(float64(time.Since(start).Nanoseconds()))
	t.trtl.onHareOutput(lid, bid)
	if t.tracer != nil {
		t.tracer.On(&HareTrace{Layer: lid, Vote: bid})
	}
}

// GetMissingActiveSet returns unknown atxs from the original list. It is done for a specific epoch
// as active set atxs never cross epoch boundary.
func (t *Tortoise) GetMissingActiveSet(epoch types.EpochID, atxs []types.ATXID) []types.ATXID {
	t.mu.Lock()
	defer t.mu.Unlock()
	edata, exists := t.trtl.epochs[epoch]
	if !exists {
		return atxs
	}
	var missing []types.ATXID
	for _, atx := range atxs {
		_, exists := edata.atxs[atx]
		if !exists {
			missing = append(missing, atx)
		}
	}
	return missing
}

// OnApplied compares stored opinion with computed opinion and sets
// pending layer to the layer above equal layer.
// this method is meant to be used only in recovery from disk codepath.
func (t *Tortoise) OnApplied(lid types.LayerID, opinion types.Hash32) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	layer := t.trtl.layer(lid)
	t.logger.Debug("on applied",
		zap.Uint32("lid", lid.Uint32()),
		log.ZShortStringer("computed", layer.opinion),
		log.ZShortStringer("stored", opinion),
	)
	rst := false
	if layer.opinion == opinion {
		t.trtl.pending = min(lid+1, t.trtl.processed)
		rst = true
	}
	if t.tracer != nil {
		t.tracer.On(&AppliedTrace{Layer: lid, Opinion: opinion, Result: rst})
	}
	return rst
}

// Updates returns list of layers where opinion was changed since previous call.
func (t *Tortoise) Updates() []result.Layer {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.trtl.pending == 0 {
		return nil
	}
	rst, err := t.results(t.trtl.pending, t.trtl.processed)
	if err != nil {
		t.logger.Panic("unexpected error",
			zap.Uint32("pending", t.trtl.pending.Uint32()),
			zap.Uint32("processed", t.trtl.pending.Uint32()),
			zap.Error(err),
		)
	}
	if t.tracer != nil {
		t.tracer.On(&UpdatesTrace{
			From: t.trtl.pending, To: t.trtl.processed,
			Results: rst,
		})
	}
	return rst
}

func (t *Tortoise) results(from, to types.LayerID) ([]result.Layer, error) {
	if from <= t.trtl.evicted {
		return nil, fmt.Errorf("requested layer %d is before evicted %d", from, t.trtl.evicted)
	}
	if from > to {
		return nil, fmt.Errorf("requested range (%d - %d) is invalid", from, to)
	}
	rst := make([]result.Layer, 0, to-from+1)
	for lid := from; lid <= to; lid++ {
		layer := t.trtl.layer(lid)
		blocks := make([]result.Block, 0, len(layer.blocks))
		for _, block := range layer.blocks {
			blocks = append(blocks, result.Block{
				Header:  block.header(),
				Data:    block.data,
				Hare:    block.hare == support,
				Valid:   block.validity == support,
				Invalid: block.validity == against,
				Local:   crossesThreshold(block.margin, t.trtl.localThreshold) == support,
			})
		}
		rst = append(rst, result.Layer{
			Layer:    lid,
			Blocks:   blocks,
			Verified: t.trtl.verified >= lid,
			Opinion:  layer.opinion,
		})
	}
	return rst, nil
}

type Mode int

func (m Mode) String() string {
	if m == 0 {
		return "verifying"
	}
	return "full"
}

const (
	Verifying = 0
	Full      = 1
)

// Mode returns 0 for verifying.
func (t *Tortoise) Mode() Mode {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.trtl.isFull {
		return Full
	}
	return Verifying
}
