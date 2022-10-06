package tortoise

import (
	"context"
	"math/big"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

// Config for protocol parameters.
type Config struct {
	Hdist uint32 `mapstructure:"tortoise-hdist"` // hare output lookback distance
	Zdist uint32 `mapstructure:"tortoise-zdist"` // hare result wait distance
	// how long we are waiting for a switch from verifying to full. relevant during rerun.
	WindowSize      uint32   `mapstructure:"tortoise-window-size"`      // size of the tortoise sliding window (in layers)
	GlobalThreshold *big.Rat `mapstructure:"tortoise-global-threshold"` // threshold for finalizing blocks and layers
	LocalThreshold  *big.Rat `mapstructure:"tortoise-local-threshold"`  // threshold for choosing when to use weak coin
	MaxExceptions   int      `mapstructure:"tortoise-max-exceptions"`   // if candidate for base block has more than max exceptions it will be ignored

	LayerSize                uint32
	BadBeaconVoteDelayLayers uint32 // number of layers to delay votes for blocks with bad beacon values during self-healing
	MeshProcessed            types.LayerID
}

// DefaultConfig for Tortoise.
func DefaultConfig() Config {
	return Config{
		LayerSize:                30,
		Hdist:                    10,
		Zdist:                    8,
		WindowSize:               1000,
		GlobalThreshold:          big.NewRat(60, 100),
		LocalThreshold:           big.NewRat(20, 100),
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
		t.logger.With().Info("loading state from disk. make sure to wait until tortoise is ready",
			log.Stringer("last layer", t.cfg.MeshProcessed),
		)
		t.eg.Go(func() error {
			for lid := types.GetEffectiveGenesis().Add(1); lid.After(t.cfg.MeshProcessed); lid = lid.Add(1) {
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
func (t *Tortoise) EncodeVotes(ctx context.Context, opts ...EncodeVotesOpts) (*types.Votes, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	conf := &encodeConf{}
	for _, opt := range opts {
		opt(conf)
	}
	return t.trtl.EncodeVotes(ctx, conf)
}

// TallyVotes up to the specified layer.
func (t *Tortoise) TallyVotes(ctx context.Context, lid types.LayerID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.trtl.onLayer(ctx, lid); err != nil {
		t.logger.Error("failed on layer", lid, log.Err(err))
	}
}

// OnAtx is expected to be called before ballots that use this atx.
func (t *Tortoise) OnAtx(atx *types.ActivationTxHeader) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.trtl.onAtx(atx)
}

// OnBlock should be called every time new block is received.
func (t *Tortoise) OnBlock(block *types.Block) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.trtl.onBlock(block.LayerIndex, block); err != nil {
		t.logger.With().Error("failed to add block to the state", block.ID(), log.Err(err))
	}
}

// OnBallot should be called every time new ballot is received.
// BaseBallot and RefBallot must be always processed first. And ATX must be stored in the database.
func (t *Tortoise) OnBallot(ballot *types.Ballot) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.trtl.onBallot(ballot); err != nil {
		t.logger.With().Error("failed to save state from ballot", ballot.ID(), log.Err(err))
	}
}

// OnHareOutput should be called when hare terminated or certificate for a block
// was synced from a peer.
// This method is expected to be called any number of times, with layers in any order.
//
// This method should be called with EmptyBlockID if node received proof of malicious behavior,
// such as signing same block id by members of the same committee.
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

// EnableBackgroundTallyVotes starts a goroutine that tallies votes in the middle
// of the layer if node is synced.
func (t *Tortoise) EnableBackgroundTallyVotes(layers timesync.LayerTimer, duration time.Duration, syncer system.SyncStateProvider) {
	t.eg.Go(func() error {
		var timer *time.Timer
		for {
			select {
			case <-t.ctx.Done():
				if timer != nil {
					timer.Stop()
				}
				return t.ctx.Err()
			case lid := <-layers:
				if syncer.IsSynced(t.ctx) {
					timer = time.AfterFunc(duration/2, func() {
						t.TallyVotes(t.ctx, lid)
					})
				}
			}
		}
	})
}

// Stop background workers.
func (t *Tortoise) Stop() {
	t.cancel()
	_ = t.eg.Wait()
}
