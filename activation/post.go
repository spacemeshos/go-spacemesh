package activation

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/metrics/public"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

// PostSetupProvider represent a compute provider for Post setup data creation.
type PostSetupProvider initialization.Provider

// PostConfig is the configuration of the Post protocol, used for data creation, proofs generation and validation.
type PostConfig struct {
	MinNumUnits   uint32 `mapstructure:"post-min-numunits"`
	MaxNumUnits   uint32 `mapstructure:"post-max-numunits"`
	LabelsPerUnit uint64 `mapstructure:"post-labels-per-unit"`
	K1            uint   `mapstructure:"post-k1"`
	K2            uint   `mapstructure:"post-k2"`
	// size of the subset of labels to verify in POST proofs
	// lower values will result in faster ATX verification but increase the risk
	// as the node must depend on malfeasance proofs to detect invalid ATXs
	K3            uint          `mapstructure:"post-k3"`
	PowDifficulty PowDifficulty `mapstructure:"post-pow-difficulty"`
}

func (c PostConfig) ToConfig() config.Config {
	return config.Config{
		MinNumUnits:   c.MinNumUnits,
		MaxNumUnits:   c.MaxNumUnits,
		LabelsPerUnit: c.LabelsPerUnit,
		K1:            c.K1,
		K2:            c.K2,
		PowDifficulty: [32]byte(c.PowDifficulty),
	}
}

// PostSetupOpts are the options used to initiate a Post setup data creation session,
// either via the public smesher API, or on node launch (via cmd args).
type PostSetupOpts struct {
	DataDir          string              `mapstructure:"smeshing-opts-datadir"`
	NumUnits         uint32              `mapstructure:"smeshing-opts-numunits"`
	MaxFileSize      uint64              `mapstructure:"smeshing-opts-maxfilesize"`
	ProviderID       PostProviderID      `mapstructure:"smeshing-opts-provider"`
	Throttle         bool                `mapstructure:"smeshing-opts-throttle"`
	Scrypt           config.ScryptParams `mapstructure:"smeshing-opts-scrypt"`
	ComputeBatchSize uint64              `mapstructure:"smeshing-opts-compute-batch-size"`
}

// PostProvingOpts are the options controlling POST proving process.
type PostProvingOpts struct {
	// Number of threads used in POST proving process.
	Threads uint `mapstructure:"smeshing-opts-proving-threads"`

	// Number of nonces tried in parallel in POST proving process.
	Nonces uint `mapstructure:"smeshing-opts-proving-nonces"`

	// RandomXMode is the mode used for RandomX computations.
	RandomXMode PostRandomXMode `mapstructure:"smeshing-opts-proving-randomx-mode"`
}

func DefaultPostProvingOpts() PostProvingOpts {
	return PostProvingOpts{
		Threads:     1,
		Nonces:      16,
		RandomXMode: PostRandomXModeFast,
	}
}

// PostProofVerifyingOpts are the options controlling POST proving process.
type PostProofVerifyingOpts struct {
	// Disable verifying POST proofs. Experimental.
	// Use with caution, only on private nodes with a trusted public peer that
	// validates the proofs.
	Disabled bool `mapstructure:"smeshing-opts-verifying-disable"`

	// Number of workers spawned to verify proofs.
	Workers int `mapstructure:"smeshing-opts-verifying-workers"`
	// The minimum number of verifying workers to keep
	// while POST is being generated in parallel.
	//
	// Caps at the value of `Workers` (then scaling is disabled).
	MinWorkers int `mapstructure:"smeshing-opts-verifying-min-workers"`
	// Flags used for the PoW verification.
	Flags PostPowFlags `mapstructure:"smeshing-opts-verifying-powflags"`
}

func DefaultPostVerifyingOpts() PostProofVerifyingOpts {
	workers := runtime.NumCPU() * 1 / 2
	if workers < 1 {
		workers = 1
	}
	return PostProofVerifyingOpts{
		MinWorkers: 1,
		Workers:    workers,
		Flags:      PostPowFlags(config.DefaultVerifyingPowFlags()),
	}
}

func DefaultTestPostVerifyingOpts() PostProofVerifyingOpts {
	return PostProofVerifyingOpts{
		MinWorkers: 1,
		Workers:    1,
		Flags:      PostPowFlags(config.DefaultVerifyingPowFlags()),
	}
}

// PostSetupStatus represents a status snapshot of the Post setup.
type PostSetupStatus struct {
	State            PostSetupState
	NumLabelsWritten uint64
	LastOpts         *PostSetupOpts
}

type PostSetupState int32

const (
	PostSetupStateNotStarted PostSetupState = 1 + iota
	PostSetupStatePrepared
	PostSetupStateInProgress
	PostSetupStateStopped
	PostSetupStateComplete
	PostSetupStateError
)

// DefaultPostConfig defines the default configuration for Post.
func DefaultPostConfig() PostConfig {
	cfg := config.DefaultConfig()
	return PostConfig{
		MinNumUnits:   cfg.MinNumUnits,
		MaxNumUnits:   cfg.MaxNumUnits,
		LabelsPerUnit: cfg.LabelsPerUnit,
		K1:            cfg.K1,
		K2:            cfg.K2,
		K3:            cfg.K2, // The default is to verify all K2 indices.
		PowDifficulty: PowDifficulty(cfg.PowDifficulty),
	}
}

// DefaultPostSetupOpts defines the default options for Post setup.
func DefaultPostSetupOpts() PostSetupOpts {
	opts := config.DefaultInitOpts()
	return PostSetupOpts{
		DataDir:          opts.DataDir,
		NumUnits:         opts.NumUnits,
		MaxFileSize:      opts.MaxFileSize,
		Throttle:         opts.Throttle,
		Scrypt:           opts.Scrypt,
		ComputeBatchSize: opts.ComputeBatchSize,
	}
}

func (o PostSetupOpts) ToInitOpts() config.InitOpts {
	var providerID *uint32
	if o.ProviderID.Value() != nil {
		providerID = new(uint32)
		*providerID = uint32(*o.ProviderID.Value())
	}

	return config.InitOpts{
		DataDir:          o.DataDir,
		NumUnits:         o.NumUnits,
		MaxFileSize:      o.MaxFileSize,
		ProviderID:       providerID,
		Throttle:         o.Throttle,
		Scrypt:           o.Scrypt,
		ComputeBatchSize: o.ComputeBatchSize,
	}
}

// PostSetupManager implements the PostProvider interface.
type PostSetupManager struct {
	commitmentAtxId types.ATXID
	syncer          syncer

	cfg         PostConfig
	logger      *zap.Logger
	db          sql.Executor
	atxsdata    *atxsdata.Data
	goldenATXID types.ATXID
	validator   nipostValidator

	mu       sync.Mutex                  // mu protects setting the values below.
	lastOpts *PostSetupOpts              // the last options used to initiate a Post setup session.
	state    PostSetupState              // state is the current state of the Post setup.
	init     *initialization.Initializer // init is the current initializer instance.

	// delay before PoST in ATX is considered valid (counting from the time it was received)
	// used to decide whether to fully verify a candidate for commitment ATX
	postValidityDelay time.Duration
}

type PostSetupManagerOpt func(*PostSetupManager)

// PostValidityDelay sets the delay before PoST in ATX is considered valid.
func PostValidityDelay(delay time.Duration) PostSetupManagerOpt {
	return func(mgr *PostSetupManager) {
		mgr.postValidityDelay = delay
	}
}

// NewPostSetupManager creates a new instance of PostSetupManager.
func NewPostSetupManager(
	cfg PostConfig,
	logger *zap.Logger,
	db sql.Executor,
	atxsdata *atxsdata.Data,
	goldenATXID types.ATXID,
	syncer syncer,
	validator nipostValidator,
	opts ...PostSetupManagerOpt,
) (*PostSetupManager, error) {
	mgr := &PostSetupManager{
		cfg:         cfg,
		logger:      logger,
		db:          db,
		atxsdata:    atxsdata,
		goldenATXID: goldenATXID,
		state:       PostSetupStateNotStarted,
		syncer:      syncer,
		validator:   validator,

		postValidityDelay: 12 * time.Hour,
	}
	for _, opt := range opts {
		opt(mgr)
	}
	return mgr, nil
}

// Status returns the setup current status.
func (mgr *PostSetupManager) Status() *PostSetupStatus {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	switch mgr.state {
	case PostSetupStateNotStarted:
		return &PostSetupStatus{
			State: mgr.state,
		}
	case PostSetupStateError:
		return &PostSetupStatus{
			State: mgr.state,
		}
	default:
		return &PostSetupStatus{
			State:            mgr.state,
			NumLabelsWritten: mgr.init.NumLabelsWritten(),
			LastOpts:         mgr.lastOpts,
		}
	}
}

// StartSession starts (or continues) a PoST session. It supports resuming a
// previously started session, and will return an error if a session is already
// in progress. It must be ensured that PrepareInitializer is called once
// before each call to StartSession and that the node is ATX synced.
func (mgr *PostSetupManager) StartSession(ctx context.Context, nodeID types.NodeID) error {
	// Ensure only one goroutine can execute initialization at a time.
	err := func() error {
		mgr.mu.Lock()
		defer mgr.mu.Unlock()
		if mgr.state != PostSetupStatePrepared {
			return errors.New("post session not prepared")
		}
		mgr.state = PostSetupStateInProgress
		return nil
	}()
	if err != nil {
		return err
	}
	mgr.logger.Info("post setup session starting",
		zap.Stringer("node_id", nodeID),
		zap.Stringer("commitment_atx", mgr.commitmentAtxId),
		zap.String("data_dir", mgr.lastOpts.DataDir),
		zap.Uint32("num_units", mgr.lastOpts.NumUnits),
		zap.Uint64("labels_per_unit", mgr.cfg.LabelsPerUnit),
		zap.Stringer("provider", mgr.lastOpts.ProviderID),
	)
	public.InitStart.Set(float64(mgr.lastOpts.NumUnits))
	events.EmitInitStart(nodeID, mgr.commitmentAtxId)
	err = mgr.init.Initialize(ctx)

	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	var errLabelMismatch initialization.ErrReferenceLabelMismatch
	switch {
	case errors.Is(err, context.Canceled):
		mgr.logger.Info("post setup session was stopped")
		mgr.state = PostSetupStateStopped
		return err
	case errors.As(err, &errLabelMismatch):
		mgr.logger.Error(
			"post setup session failed due to an issue with the initialization provider",
			zap.Error(errLabelMismatch),
		)
		mgr.state = PostSetupStateError
		events.EmitInitFailure(nodeID, mgr.commitmentAtxId, errLabelMismatch)
		return nil
	case err != nil:
		mgr.logger.Error("post setup session failed", zap.Error(err))
		mgr.state = PostSetupStateError
		events.EmitInitFailure(nodeID, mgr.commitmentAtxId, err)
		return err
	}
	public.InitEnd.Set(float64(mgr.lastOpts.NumUnits))
	events.EmitInitComplete(nodeID)

	mgr.logger.Info("post setup completed",
		zap.Stringer("node_id", nodeID),
		zap.Stringer("commitment_atx", mgr.commitmentAtxId),
		zap.String("data_dir", mgr.lastOpts.DataDir),
		zap.Uint32("num_units", mgr.lastOpts.NumUnits),
		zap.Uint64("labels_per_unit", mgr.cfg.LabelsPerUnit),
		zap.Stringer("provider", mgr.lastOpts.ProviderID),
	)
	mgr.state = PostSetupStateComplete
	return nil
}

// PrepareInitializer prepares the initializer to begin the initialization
// process, it needs to be called before each call to StartSession. Having this
// function be separate from StartSession provides a means to understand if the
// post configuration is valid before kicking off a very long running task
// (StartSession can take days to complete). After the first call to this
// method subsequent calls to this method will return an error until
// StartSession has completed execution.
func (mgr *PostSetupManager) PrepareInitializer(ctx context.Context, opts PostSetupOpts, id types.NodeID) error {
	mgr.logger.Info("preparing post initializer", zap.Any("opts", opts))
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if mgr.state == PostSetupStatePrepared || mgr.state == PostSetupStateInProgress {
		return errors.New("post setup session in progress")
	}

	var err error
	mgr.commitmentAtxId, err = mgr.commitmentAtx(ctx, opts.DataDir, id)
	if err != nil {
		return err
	}

	newInit, err := initialization.NewInitializer(
		initialization.WithNodeId(id.Bytes()),
		initialization.WithCommitmentAtxId(mgr.commitmentAtxId.Bytes()),
		initialization.WithConfig(mgr.cfg.ToConfig()),
		initialization.WithInitOpts(opts.ToInitOpts()),
		initialization.WithLogger(mgr.logger),
	)
	if err != nil {
		mgr.state = PostSetupStateError
		return fmt.Errorf("new initializer: %w", err)
	}

	mgr.state = PostSetupStatePrepared
	mgr.init = newInit
	mgr.lastOpts = &opts
	return nil
}

func (mgr *PostSetupManager) commitmentAtx(ctx context.Context, dataDir string, id types.NodeID) (types.ATXID, error) {
	m, err := initialization.LoadMetadata(dataDir)
	switch {
	case err == nil:
		return types.ATXID(types.BytesToHash(m.CommitmentAtxId)), nil
	case errors.Is(err, initialization.ErrStateMetadataFileMissing):
		// if this node has already published an ATX, get its initial ATX and from it the commitment ATX
		atxId, err := atxs.GetFirstIDByNodeID(mgr.db, id)
		if err == nil {
			atx, err := atxs.Get(mgr.db, atxId)
			if err != nil {
				return types.EmptyATXID, err
			}
			if atx.CommitmentATX == nil {
				return types.EmptyATXID, fmt.Errorf("initial ATX %s does not contain a commitment ATX", atxId)
			}
			return *atx.CommitmentATX, nil
		}

		// if this node has not published an ATX select the best ATX with `findCommitmentAtx`
		return mgr.findCommitmentAtx(ctx)
	default:
		return types.EmptyATXID, fmt.Errorf("load metadata: %w", err)
	}
}

// findCommitmentAtx determines the best commitment ATX to use for the node.
// It will use the ATX with the highest height seen by the node and defaults to the goldenATX,
// when no ATXs have yet been published.
func (mgr *PostSetupManager) findCommitmentAtx(ctx context.Context) (types.ATXID, error) {
	mgr.logger.Info("waiting for ATXs to sync before selecting commitment ATX")
	select {
	case <-ctx.Done():
		return types.EmptyATXID, ctx.Err()
	case <-mgr.syncer.RegisterForATXSynced():
		mgr.logger.Info("ATXs synced - selecting commitment ATX")
	}

	latest, err := atxs.LatestEpoch(mgr.db)
	if err != nil {
		return types.EmptyATXID, fmt.Errorf("get latest epoch: %w", err)
	}

	atx, err := findFullyValidHighTickAtx(
		context.Background(),
		mgr.atxsdata,
		latest,
		mgr.goldenATXID,
		mgr.validator,
		mgr.logger,
		VerifyChainOpts.AssumeValidBefore(time.Now().Add(-mgr.postValidityDelay)),
		VerifyChainOpts.WithLogger(mgr.logger),
	)
	switch {
	case errors.Is(err, ErrNotFound):
		mgr.logger.Info("using golden atx as commitment atx")
		return mgr.goldenATXID, nil
	case err != nil:
		return types.EmptyATXID, fmt.Errorf("get commitment atx: %w", err)
	default:
		return atx, nil
	}
}

// Reset deletes the data file(s).
func (mgr *PostSetupManager) Reset() error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if err := mgr.init.Reset(); err != nil {
		return fmt.Errorf("reset: %w", err)
	}

	// Reset internal state.
	mgr.state = PostSetupStateNotStarted
	return nil
}
