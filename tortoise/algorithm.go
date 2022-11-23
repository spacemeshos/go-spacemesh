package tortoise

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/system"
)

// Config for protocol parameters.
type Config struct {
	Hdist uint32 `mapstructure:"tortoise-hdist"` // hare output lookback distance
	Zdist uint32 `mapstructure:"tortoise-zdist"` // hare result wait distance
	// how long we are waiting for a switch from verifying to full. relevant during rerun.
	WindowSize    uint32 `mapstructure:"tortoise-window-size"`    // size of the tortoise sliding window (in layers)
	MaxExceptions int    `mapstructure:"tortoise-max-exceptions"` // if candidate for base block has more than max exceptions it will be ignored

	LayerSize                uint32
	BadBeaconVoteDelayLayers uint32 // number of layers to delay votes for blocks with bad beacon values during self-healing
}

// DefaultConfig for Tortoise.
func DefaultConfig() Config {
	return Config{
		LayerSize:                30,
		Hdist:                    10,
		Zdist:                    8,
		WindowSize:               1000,
		BadBeaconVoteDelayLayers: 6,
		MaxExceptions:            30 * 100, // 100 layers of average size
	}
}

// Tortoise is a thread safe verifying tortoise wrapper, it just locks all actions.
type Tortoise struct {
	logger log.Log
	ctx    context.Context
	cfg    Config

	eg     errgroup.Group
	cancel context.CancelFunc

	ready    chan error
	readyErr struct {
		sync.Mutex
		err error
	}

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
func New(cdb *datastore.CachedDB, beacons system.BeaconGetter, updater blockValidityUpdater, opts ...Opt) *Tortoise {
	t := &Tortoise{
		ctx:    context.Background(),
		logger: log.NewNop(),
		cfg:    DefaultConfig(),
		ready:  make(chan error, 1),
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

	ctx, cancel := context.WithCancel(t.ctx)
	t.cancel = cancel

	latest, err := ballots.LatestLayer(cdb)
	if err != nil {
		t.logger.With().Panic("failed to load latest layer",
			log.Err(err),
		)
	}
	needsRecovery := latest.After(types.GetEffectiveGenesis())

	t.trtl = newTurtle(
		t.logger,
		cdb,
		beacons,
		updater,
		t.cfg,
	)
	if needsRecovery {
		t.logger.With().Info("loading state from disk. make sure to wait until tortoise is ready",
			log.Stringer("last layer", latest),
		)
		t.eg.Go(func() error {
			for lid := types.GetEffectiveGenesis().Add(1); !lid.After(latest); lid = lid.Add(1) {
				err := t.trtl.onLayer(ctx, lid)
				if err != nil {
					t.ready <- err
					return err
				}
			}
			close(t.ready)
			return nil
		})
	} else {
		t.logger.Info("no state on disk. initialized with genesis")
		close(t.ready)
	}
	return t
}

// LatestComplete returns the latest verified layer.
func (t *Tortoise) LatestComplete() types.LayerID {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.trtl.verified
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
	if err := t.trtl.onLayer(ctx, lid); err != nil {
		errorsCounter.Inc()
		t.logger.With().Error("failed on layer", lid, log.Err(err))
	}
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
	if err := t.trtl.onBlock(block.LayerIndex, block); err != nil {
		errorsCounter.Inc()
		t.logger.With().Error("failed to add block to the state", block.ID(), log.Err(err))
	}
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
}

// DecodeBallot decodes ballot if it wasn't processed earlier.
func (t *Tortoise) DecodeBallot(ballot *types.Ballot) (*DecodedBallot, error) {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitBallotDuration.Observe(float64(time.Since(start).Nanoseconds()))
	info, err := t.trtl.decodeBallot(ballot)
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
	return &DecodedBallot{Ballot: ballot, info: info}, nil
}

// StoreBallot stores previously decoded ballot.
func (t *Tortoise) StoreBallot(decoded *DecodedBallot) error {
	start := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	waitBallotDuration.Observe(float64(time.Since(start).Nanoseconds()))
	if decoded.IsMalicious() {
		decoded.info.weight = weight{}
	}
	if err := t.trtl.storeBallot(decoded.info); err != nil {
		errorsCounter.Inc()
		return err
	}
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

// WaitReady waits until state will be reloaded from disk.
func (t *Tortoise) WaitReady(ctx context.Context) error {
	select {
	case err := <-t.ready:
		t.readyErr.Lock()
		defer t.readyErr.Unlock()
		if err != nil {
			t.readyErr.err = err
		}
		return t.readyErr.err
	case <-ctx.Done():
		return ctx.Err() //nolint
	}
}

// Stop background workers.
func (t *Tortoise) Stop() {
	t.cancel()
	_ = t.eg.Wait()
}
