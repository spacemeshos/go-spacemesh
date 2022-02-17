package tortoise

import (
	"context"
	"math/big"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/tortoise/organizer"
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

	mu  sync.Mutex
	org *organizer.Organizer

	// update will be set to non-nil after rerun completes, and must be set to nil once
	// used to replace trtl.
	update *rerunResult
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
func New(mdb blockDataProvider, atxdb atxDataProvider, beacons system.BeaconGetter, opts ...Opt) *Tortoise {
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
		mdb,
		atxdb,
		beacons,
		t.cfg,
	)
	t.trtl.init(t.ctx, types.GenesisLayer())
	if needsRecovery {
		t.trtl.processed = t.cfg.MeshProcessed
		// TODO(dshulyak) last should be set according to the clock.
		t.trtl.last = t.cfg.MeshProcessed
		t.trtl.verified = t.cfg.MeshVerified
		t.trtl.historicallyVerified = t.cfg.MeshVerified

		t.logger.Info("loading state from disk. make sure to wait until tortoise is ready",
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

	t.org = organizer.New(
		organizer.WithLogger(t.logger),
		organizer.WithLastLayer(t.trtl.processed),
	)

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

// BaseBallot chooses a base ballot and creates a differences list. needs the hare results for latest layers.
func (t *Tortoise) BaseBallot(ctx context.Context) (*types.Votes, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.trtl.BaseBallot(ctx)
}

// HandleIncomingLayer processes all layer block votes
// returns the old verified layer and new verified layer after taking into account the blocks votes.
func (t *Tortoise) HandleIncomingLayer(ctx context.Context, layerID types.LayerID) (types.LayerID, types.LayerID, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var (
		old    = t.trtl.verified
		logger = t.logger.WithContext(ctx).With()
	)

	reverted, observed := t.updateFromRerun(ctx)
	if reverted {
		// make sure state is reapplied from far enough back if there was a state reversion.
		// this is the first changed layer. subtract one to indicate that the layer _prior_ was the old
		// pBase, since we never reapply the state of oldPbase.
		old = observed.Sub(1)
	}
	t.org.Iterate(ctx, layerID, func(lid types.LayerID) {
		if err := t.trtl.HandleIncomingLayer(ctx, lid); err != nil {
			logger.Error("tortoise errored handling incoming layer", lid, log.Err(err))
		}
	})

	return old, t.trtl.verified, reverted
}

// OnBlock should be called every time new block is received.
func (t *Tortoise) OnBlock(block *types.Block) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.trtl.onBlock(block.LayerIndex, block.ID())
}

// OnBallot should be called every time new ballot is received.
// BaseBallot and RefBallot must be always processed first. And ATX must be stored in the database.
func (t *Tortoise) OnBallot(ballot *types.Ballot) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.trtl.onBallot(ballot); err != nil {
		t.logger.Warning("failed to save state from ballot", ballot.ID(), log.Err(err))
	}
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
	t.eg.Wait()
}
