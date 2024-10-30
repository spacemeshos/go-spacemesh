package tortoise

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Config for protocol parameters.
type Config struct {
	// how long we are waiting for a switch from verifying to full. relevant during rerun.
	Hdist                uint32 `mapstructure:"tortoise-hdist"`       // hare output look back distance
	Zdist                uint32 `mapstructure:"tortoise-zdist"`       // hare result wait distance
	WindowSize           uint32 `mapstructure:"tortoise-window-size"` // size of the tortoise sliding window (in layers)
	HistoricalWindowSize []WindowSizeInterval
	// ignored if candidate for base ballot has more than max exceptions
	MaxExceptions int `mapstructure:"tortoise-max-exceptions"`
	// number of layers to delay votes for blocks with bad beacon values during self-healing. ideally a full epoch.
	BadBeaconVoteDelayLayers uint32 `mapstructure:"tortoise-delay-layers"`
	// EnableTracer will write tortoise traces to the stderr.
	EnableTracer bool `mapstructure:"tortoise-enable-tracer"`
	// MinimalActiveSetWeight is a weight that will replace weight
	// recorded in the first ballot, if that weight is less than minimal
	// for purposes of eligibility computation.
	MinimalActiveSetWeight []types.EpochMinimalActiveWeight
	// CollectDetails sets numbers of layers to collect details.
	// Must be less than WindowSize.
	CollectDetails uint32 `mapstructure:"tortoise-collect-details"`
	LayerSize      uint32
}

type WindowSizeInterval struct {
	Start  types.LayerID
	End    types.LayerID
	Window uint32
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

func (c *Config) WindowSizeLayers(applied types.LayerID) types.LayerID {
	for _, interval := range c.HistoricalWindowSize {
		if applied >= interval.Start && applied <= interval.End {
			return types.LayerID(interval.Window)
		}
	}
	return types.LayerID(c.WindowSize)
}

func (c *Config) WindowSizeEpochs(applied types.LayerID) types.EpochID {
	layers := c.WindowSizeLayers(applied)
	quo := layers / types.LayerID(types.GetLayersPerEpoch())
	if layers%types.LayerID(types.GetLayersPerEpoch()) != 0 {
		quo++
	}
	return types.EpochID(quo)
}

// Tortoise is a thread safe verifying tortoise wrapper, it just locks all actions.
type Tortoise struct {
	logger *zap.Logger
	cfg    Config

	mu     sync.Mutex
	trtl   *turtle
	tracer *tracer
}

// Opt for configuring tortoise.
type Opt func(t *Tortoise)

// WithLogger defines logger for tortoise.
func WithLogger(logger *zap.Logger) Opt {
	return func(t *Tortoise) {
		t.logger = logger
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
func New(atxdata *atxsdata.Data, opts ...Opt) (*Tortoise, error) {
	t := &Tortoise{
		logger: zap.NewNop(),
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
	if t.cfg.CollectDetails > t.cfg.WindowSize {
		t.logger.Panic("tortoise-collect-details must be lower then tortoise-window-size",
			zap.Uint32("tortoise-collect-details", t.cfg.CollectDetails),
			zap.Uint32("tortoise-window-size", t.cfg.WindowSize),
		)
	}
	t.trtl = newTurtle(t.logger, t.cfg, atxdata)
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
	if t.cfg.CollectDetails > 0 {
		t.logger.Info("tortoise will collect details",
			zap.Uint32("tortoise-collect-details", t.cfg.CollectDetails),
		)
		enableCollector(t)
	}
	return t, nil
}

func (t *Tortoise) RecoverFrom(lid types.LayerID, opinion, prev types.Hash32) {
	if lid <= types.GetEffectiveGenesis() {
		t.logger.Panic("recover should be after effective genesis",
			zap.Uint32("lid", lid.Uint32()),
			zap.Uint32("effective genesis", types.GetEffectiveGenesis().Uint32()),
		)
	}
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
	t.trtl.processed = lid - 1 // -1 so that iteration in tallyVotes starts from the target layer
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

// OnMalfeasance registers node id as malfeasant.
// - ballots from this id will have zero weight
// - atxs - will not be counted towards global/local thresholds
// If node registers equivocating ballot/atx it should
// call OnMalfeasance before storing ballot/atx.
func (t *Tortoise) OnMalfeasance(id types.NodeID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.trtl.isMalfeasant(id) {
		return
	}
	t.logger.Debug("on malfeasance", zap.Stringer("id", id))
	t.trtl.markMalfeasant(id)
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
// Tortoise computes threshold from last non-verified till the last known layer,
// since we don't download atxs before starting tortoise we won't be able to compute threshold
// based on the last clock layer (see https://github.com/spacemeshos/go-spacemesh/issues/3003)
func EncodeVotesWithCurrent(current types.LayerID) EncodeVotesOpts {
	return func(conf *encodeConf) {
		conf.current = &current
	}
}

// EncodeVotes chooses a base ballot and creates a differences list. Needs the hare results for latest layers.
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
func (t *Tortoise) TallyVotes(lid types.LayerID) {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitTallyVotes.Observe(float64(time.Since(start).Nanoseconds()))
	start = time.Now()
	t.trtl.tallyVotes(lid)
	executeTallyVotes.Observe(float64(time.Since(start).Nanoseconds()))
	if t.tracer != nil {
		t.tracer.On(&TallyTrace{Layer: lid})
	}
}

// OnAtx is expected to be called before ballots that use this atx.
func (t *Tortoise) OnAtx(target types.EpochID, id types.ATXID, atx *atxsdata.ATX) {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitAtxDuration.Observe(float64(time.Since(start).Nanoseconds()))
	t.trtl.onAtx(target, id, atx)
	if t.tracer != nil {
		t.tracer.On(&AtxTrace{ID: id, TargetEpoch: target, Atx: atx})
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

// OnRecoveredBlocks uploads blocks to the state with all metadata.
//
// Implementation assumes that they will be uploaded in order.
func (t *Tortoise) OnRecoveredBlocks(lid types.LayerID, validity map[types.BlockHeader]bool, hare *types.BlockID) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for block, valid := range validity {
		if exists := t.trtl.getBlock(block); exists != nil {
			continue
		}
		info := newBlockInfo(block)
		info.data = true
		if valid {
			info.validity = support
		}
		t.trtl.addBlock(info)
	}
	if hare != nil {
		t.trtl.onHareOutput(lid, *hare)
	} else if !withinDistance(t.cfg.Hdist, lid, t.trtl.last) {
		layer := t.trtl.state.layer(lid)
		layer.hareTerminated = true
		for _, info := range layer.blocks {
			if info.validity == abstain {
				info.validity = against
			}
		}
	}

	t.logger.Debug("loaded recovered blocks",
		zap.Uint32("lid", lid.Uint32()),
		zap.Uint32("last", t.trtl.last.Uint32()),
		zapBlocks(t.trtl.state.layer(lid).blocks),
	)
	if t.tracer != nil {
		t.tracer.On(newRecoveredBlocksTrace(lid, validity, hare))
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
	ID            types.BallotID
	Layer         types.LayerID
	ATXID         types.ATXID
	Smesher       types.NodeID
	Beacon        types.Beacon
	Eligibilities uint32
}

func (t *Tortoise) GetBallot(id types.BallotID) *BallotData {
	t.mu.Lock()
	defer t.mu.Unlock()
	info := t.trtl.ballotRefs[id]
	if info == nil {
		return nil
	}
	return &BallotData{
		ID:            id,
		Layer:         info.layer,
		ATXID:         info.reference.atxid,
		Smesher:       info.reference.smesher,
		Beacon:        info.reference.beacon,
		Eligibilities: info.reference.expectedBallots,
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
func (t *Tortoise) GetMissingActiveSet(target types.EpochID, atxs []types.ATXID) []types.ATXID {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.trtl.atxsdata.MissingInEpoch(target, atxs)
}

// OnApplied compares stored opinion with computed opinion and sets
// pending layer to the layer above equal layer.
// This method is meant to be used only in recovery from disk codepath.
func (t *Tortoise) OnApplied(lid types.LayerID, opinion types.Hash32) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	layer := t.trtl.layer(lid)
	t.logger.Debug("on applied",
		zap.Uint32("lid", lid.Uint32()),
		zap.Uint32("pending", t.trtl.pending.Uint32()),
		zap.Uint32("processed", t.trtl.processed.Uint32()),
		log.ZShortStringer("computed", layer.opinion),
		log.ZShortStringer("stored", opinion),
	)
	rst := false
	if layer.opinion == opinion {
		oldPending := t.trtl.pending
		t.trtl.pending = min(lid+1, t.trtl.processed)
		rst = true
		if oldPending != t.trtl.pending {
			t.trtl.evict()
		}
	}
	if t.tracer != nil {
		t.tracer.On(&AppliedTrace{Layer: lid, Opinion: opinion, Result: rst})
	}
	return rst
}

// latestsResults returns at most N latest results from process layer.
//
// Private as it meant to be used for metering.
func (t *Tortoise) latestsResults(n uint32) []result.Layer {
	t.mu.Lock()
	defer t.mu.Unlock()
	if n > t.trtl.processed.Uint32() {
		n = t.trtl.processed.Uint32()
	}
	rst, err := t.results(t.trtl.processed-types.LayerID(n), t.trtl.processed)
	if err != nil {
		t.logger.Error("unexpected error", zap.Error(err))
		return nil
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

// UpdateLastLayer updates last layer which is used for determining weight thresholds.
func (t *Tortoise) UpdateLastLayer(last types.LayerID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.trtl.updateLast(last)
}

// UpdateVerified layers based on the previously known verified layer.
func (t *Tortoise) UpdateVerified(verified types.LayerID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.trtl.verified = verified
}

func (t *Tortoise) WithinHdist(lid types.LayerID) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return withinDistance(t.cfg.Hdist, lid, t.trtl.last)
}
