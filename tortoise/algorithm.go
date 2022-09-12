package tortoise

import (
	"context"
	"math/big"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/system"
)

// Config for protocol parameters.
type Config struct {
	Hdist uint32 `mapstructure:"tortoise-hdist"` // hare output lookback distance
	Zdist uint32 `mapstructure:"tortoise-zdist"` // hare result wait distance
	// how long we are waiting for a switch from verifying to full. relevant during rerun.
	WindowSize                      uint32        `mapstructure:"tortoise-window-size"`      // size of the tortoise sliding window (in layers)
	GlobalThreshold                 *big.Rat      `mapstructure:"tortoise-global-threshold"` // threshold for finalizing blocks and layers
	LocalThreshold                  *big.Rat      `mapstructure:"tortoise-local-threshold"`  // threshold for choosing when to use weak coin
	RerunInterval                   time.Duration `mapstructure:"tortoise-rerun-interval"`
	MaxExceptions                   int           `mapstructure:"tortoise-max-exceptions"` // if candidate for base block has more than max exceptions it will be ignored
	VerifyingModeVerificationWindow uint32        `mapstructure:"verifying-mode-verification-window"`
	FullModeVerificationWindow      uint32        `mapstructure:"full-mode-verification-window"`

	LayerSize                uint32
	BadBeaconVoteDelayLayers uint32 // number of layers to delay votes for blocks with bad beacon values during self-healing
	MeshProcessed            types.LayerID
	MeshVerified             types.LayerID
}

// DefaultConfig for Tortoise.
func DefaultConfig() Config {
	return Config{
		LayerSize:                       30,
		Hdist:                           10,
		Zdist:                           8,
		WindowSize:                      100,
		GlobalThreshold:                 big.NewRat(60, 100),
		LocalThreshold:                  big.NewRat(20, 100),
		RerunInterval:                   time.Hour,
		BadBeaconVoteDelayLayers:        6,
		MaxExceptions:                   30 * 100, // 100 layers of average size
		VerifyingModeVerificationWindow: 1000,
		FullModeVerificationWindow:      20,
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

	mu sync.Mutex

	// update will be set to non-nil after rerun completes, and must be set to nil once
	// used to replace trtl.
	update *turtle
	trtl   *turtle
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

	needsRecovery := t.cfg.MeshProcessed.After(types.GetEffectiveGenesis())

	t.trtl = newTurtle(
		t.logger,
		cdb,
		beacons,
		updater,
		t.cfg,
	)
	t.trtl.init(t.ctx, types.GenesisLayer())
	if needsRecovery {
		t.trtl.processed = t.cfg.MeshProcessed
		// TODO(dshulyak) last should be set according to the clock.
		t.trtl.last = t.cfg.MeshProcessed
		t.trtl.verified = t.cfg.MeshVerified
		t.trtl.historicallyVerified = t.cfg.MeshVerified

		t.logger.With().Info("loading state from disk. make sure to wait until tortoise is ready",
			log.Stringer("last_layer", t.cfg.MeshProcessed),
			log.Stringer("historically_verified", t.cfg.MeshVerified),
		)
		t.eg.Go(func() error {
			t.ready <- t.rerun(ctx)
			close(t.ready)
			return nil
		})
	} else {
		t.logger.Info("no state on disk. initialized with genesis")
		close(t.ready)
	}

	// TODO(dshulyak) with low rerun interval it is possible to start a rerun
	// when initial sync is in progress, or right after sync
	if t.cfg.RerunInterval != 0 {
		t.logger.With().Info("launching rerun loop", log.Duration("interval", t.cfg.RerunInterval))
		t.eg.Go(func() error {
			t.rerunLoop(ctx, t.cfg.RerunInterval)
			return nil
		})
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
func (t *Tortoise) EncodeVotes(ctx context.Context, opts ...EncodeVotesOpts) (*types.Votes, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	conf := &encodeConf{}
	for _, opt := range opts {
		opt(conf)
	}
	return t.trtl.EncodeVotes(ctx, conf)
}

// HandleIncomingLayer processes all layer block votes
// returns the old verified layer and new verified layer after taking into account the blocks votes.
func (t *Tortoise) HandleIncomingLayer(ctx context.Context, lid types.LayerID) types.LayerID {
	t.mu.Lock()
	defer t.mu.Unlock()

	var (
		old    = t.trtl.verified
		logger = t.logger.WithContext(ctx).With()
	)

	t.updateFromRerun(ctx)
	if err := t.trtl.onLayerTerminated(ctx, lid); err != nil {
		logger.Error("tortoise errored handling incoming layer", lid, log.Err(err))
		return t.trtl.verified
	}

	for lid := old.Add(1); !lid.After(t.trtl.verified); lid = lid.Add(1) {
		events.ReportLayerUpdate(events.LayerUpdate{
			LayerID: lid,
			Status:  events.LayerStatusTypeConfirmed,
		})
	}

	return t.trtl.verified
}

// OnBlock should be called every time new block is received.
func (t *Tortoise) OnBlock(block *types.Block) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.trtl.onBlock(block.LayerIndex, block)
}

// OnBallot should be called every time new ballot is received.
// BaseBallot and RefBallot must be always processed first. And ATX must be stored in the database.
func (t *Tortoise) OnBallot(ballot *types.Ballot) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.trtl.onBallot(ballot); err != nil {
		t.logger.With().Warning("failed to save state from ballot", ballot.ID(), log.Err(err))
	}
}

// OnHareOutput should be called when hare terminated or certificate for a block
// was synced from a peer.
func (t *Tortoise) OnHareOutput(lid types.LayerID, bid types.BlockID) {
	t.mu.Lock()
	defer t.mu.Unlock()
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
