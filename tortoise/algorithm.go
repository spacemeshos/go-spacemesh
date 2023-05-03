package tortoise

import (
	"context"
	"fmt"
	"sync"
	"time"

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

	LayerSize uint32
}

// DefaultConfig for Tortoise.
func DefaultConfig() Config {
	return Config{
		LayerSize:                30,
		Hdist:                    10,
		Zdist:                    8,
		WindowSize:               1000,
		BadBeaconVoteDelayLayers: 0,
		MaxExceptions:            30 * 100, // 100 layers of average size
	}
}

// Tortoise is a thread safe verifying tortoise wrapper, it just locks all actions.
type Tortoise struct {
	logger log.Log
	ctx    context.Context
	cfg    Config

	mu   sync.Mutex
	trtl *turtle
}

// Opt for configuring tortoise.
type Opt func(t *Tortoise)

// WithLogger defines logger for tortoise.
func WithLogger(logger log.Log) Opt {
	return func(t *Tortoise) {
		t.logger = logger
	}
}

// WithContext defines context for tortoise.
func WithContext(ctx context.Context) Opt {
	return func(t *Tortoise) {
		t.ctx = ctx
	}
}

// WithConfig defines protocol parameters.
func WithConfig(cfg Config) Opt {
	return func(t *Tortoise) {
		t.cfg = cfg
	}
}

// New creates Tortoise instance.
func New(opts ...Opt) (*Tortoise, error) {
	t := &Tortoise{
		ctx:    context.Background(),
		logger: log.NewNop(),
		cfg:    DefaultConfig(),
	}
	for _, opt := range opts {
		opt(t)
	}
	if t.cfg.Hdist < t.cfg.Zdist {
		t.logger.With().Panic("hdist must be >= zdist",
			log.Uint32("hdist", t.cfg.Hdist),
			log.Uint32("zdist", t.cfg.Zdist),
		)
	}
	t.trtl = newTurtle(t.logger, t.cfg)
	return t, nil
}

// LatestComplete returns the latest verified layer.
func (t *Tortoise) LatestComplete() types.LayerID {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.trtl.verified
}

func (t *Tortoise) Updates() map[types.LayerID]map[types.BlockID]bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	res := t.trtl.updated
	t.trtl.updated = nil
	return res
}

func (t *Tortoise) OnWeakCoin(lid types.LayerID, coin bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logger.With().Debug("on weakcoin",
		log.Uint32("layer_id", lid.Uint32()),
		log.Uint32("evicted", t.trtl.evicted.Uint32()),
		log.Bool("coin", coin),
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
}

func (t *Tortoise) OnBeacon(eid types.EpochID, beacon types.Beacon) {
	t.mu.Lock()
	defer t.mu.Unlock()
	evicted := t.trtl.evicted.GetEpoch()
	t.logger.With().Debug("on beacon",
		log.Uint32("epoch_id", eid.Uint32()),
		log.Uint32("evicted", evicted.Uint32()),
		log.Stringer("beacon", beacon),
	)
	if eid <= evicted {
		return
	}
	epoch := t.trtl.epoch(eid)
	epoch.beacon = &beacon
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
func (t *Tortoise) EncodeVotes(ctx context.Context, opts ...EncodeVotesOpts) (*types.Opinion, error) {
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
}

// OnAtx is expected to be called before ballots that use this atx.
func (t *Tortoise) OnAtx(atx *types.ActivationTxHeader) {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitAtxDuration.Observe(float64(time.Since(start).Nanoseconds()))
	t.trtl.onAtx(atx)
}

// OnBlock should be called every time new block is received.
func (t *Tortoise) OnBlock(block *types.Block) {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitBlockDuration.Observe(float64(time.Since(start).Nanoseconds()))
	t.trtl.onBlock(result.Block{Header: block.ToVote(), Data: true})
}

func (t *Tortoise) OnHistoricalResult(result result.Block) {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitBlockDuration.Observe(float64(time.Since(start).Nanoseconds()))
	t.trtl.onBlock(result)
}

// OnBallot should be called every time new ballot is received.
// BaseBallot and RefBallot must be always processed first. And ATX must be stored in the database.
func (t *Tortoise) OnBallot(ballot *types.Ballot) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.trtl.onBallot(ballot); err != nil {
		errorsCounter.Inc()
		t.logger.With().Error("failed to save state from ballot", ballot.ID(), log.Err(err))
	}
}

// DecodedBallot created after unwrapping exceptions list and computing internal opinion.
type DecodedBallot struct {
	*types.Ballot
	info *ballotInfo
	// after validation is finished we need to add new vote targets
	// for tortoise from the decoded votes. minHint identifies the boundary
	// until which we have to scan.
	minHint types.LayerID
}

// DecodeBallot decodes ballot if it wasn't processed earlier.
func (t *Tortoise) DecodeBallot(ballot *types.Ballot) (*DecodedBallot, error) {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitBallotDuration.Observe(float64(time.Since(start).Nanoseconds()))
	info, min, err := t.trtl.decodeBallot(ballot)
	if err != nil {
		errorsCounter.Inc()
		return nil, err
	}
	if info == nil {
		errorsCounter.Inc()
		return nil, fmt.Errorf("can't decode ballot %s", ballot.ID())
	}
	if info.opinion() != ballot.OpinionHash {
		errorsCounter.Inc()
		return nil, fmt.Errorf(
			"computed opinion hash %s doesn't match signed %s for ballot %s",
			info.opinion().ShortString(), ballot.OpinionHash.ShortString(), ballot.ID(),
		)
	}
	return &DecodedBallot{Ballot: ballot, info: info, minHint: min}, nil
}

// StoreBallot stores previously decoded ballot.
func (t *Tortoise) StoreBallot(decoded *DecodedBallot) error {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitBallotDuration.Observe(float64(time.Since(start).Nanoseconds()))
	if decoded.IsMalicious() {
		decoded.info.malicious = true
	}
	t.trtl.storeBallot(decoded.info, decoded.minHint)
	return nil
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

// Results returns layers that crossed threshold in range [from, to].
func (t *Tortoise) Results(from, to types.LayerID) ([]result.Layer, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if from <= t.trtl.evicted {
		return nil, fmt.Errorf("requested layer %d is before evicted %d", from, t.trtl.evicted)
	}
	rst := make([]result.Layer, 0, to-from)
	for lid := from; lid <= to; lid++ {
		layer := t.trtl.layer(lid)
		blocks := make([]result.Block, 0, len(layer.blocks))
		for _, block := range layer.blocks {
			blocks = append(blocks, result.Block{
				Header: block.header(),
				Data:   block.data,
				Valid:  block.validity == support,
			})
		}
		rst = append(rst, result.Layer{
			Layer:  lid,
			Blocks: blocks,
		})
	}
	return rst, nil
}
