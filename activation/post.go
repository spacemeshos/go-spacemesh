package activation

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"sync"

	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/metrics/public"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

// PostSetupProvider represent a compute provider for Post setup data creation.
type PostSetupProvider initialization.Provider

// PostConfig is the configuration of the Post protocol, used for data creation, proofs generation and validation.
type PostConfig struct {
	MinNumUnits   uint32        `mapstructure:"post-min-numunits"`
	MaxNumUnits   uint32        `mapstructure:"post-max-numunits"`
	LabelsPerUnit uint64        `mapstructure:"post-labels-per-unit"`
	K1            uint32        `mapstructure:"post-k1"`
	K2            uint32        `mapstructure:"post-k2"`
	K3            uint32        `mapstructure:"post-k3"`
	PowDifficulty PowDifficulty `mapstructure:"post-pow-difficulty"`
}

func (c PostConfig) ToConfig() config.Config {
	return config.Config{
		MinNumUnits:   c.MinNumUnits,
		MaxNumUnits:   c.MaxNumUnits,
		LabelsPerUnit: c.LabelsPerUnit,
		K1:            c.K1,
		K2:            c.K2,
		K3:            c.K3,
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

type PostProviderID struct {
	value *int64
}

// String implements pflag.Value.String.
func (id PostProviderID) String() string {
	if id.value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *id.value)
}

// Type implements pflag.Value.Type.
func (PostProviderID) Type() string {
	return "PostProviderID"
}

// Set implements pflag.Value.Set.
func (id *PostProviderID) Set(value string) error {
	if len(value) == 0 {
		id.value = nil
		return nil
	}

	i, err := strconv.ParseInt(value, 10, 33)
	if err != nil {
		return fmt.Errorf("failed to parse PoST Provider ID (\"%s\"): %w", value, err)
	}

	id.value = new(int64)
	*id.value = int64(i)
	return nil
}

// SetInt64 sets the value of the PostProviderID to the given int64.
func (id *PostProviderID) SetInt64(value int64) {
	id.value = &value
}

// Value returns the value of the PostProviderID as a pointer to uint32.
func (id *PostProviderID) Value() *int64 {
	return id.value
}

// PostProvingOpts are the options controlling POST proving process.
type PostProvingOpts struct {
	// Number of threads used in POST proving process.
	Threads uint `mapstructure:"smeshing-opts-proving-threads"`
	// Number of nonces tried in parallel in POST proving process.
	Nonces uint `mapstructure:"smeshing-opts-proving-nonces"`
	// Flags used in the PoW computation.
	Flags config.PowFlags `mapstructure:"smeshing-opts-proving-powflags"`
}

func DefaultPostProvingOpts() PostProvingOpts {
	return PostProvingOpts{
		Threads: 1,
		Nonces:  16,
		Flags:   config.DefaultProvingPowFlags(),
	}
}

// PostProvingOpts are the options controlling POST proving process.
type PostProofVerifyingOpts struct {
	// Number of workers spawned to verify proofs.
	Workers int `mapstructure:"smeshing-opts-verifying-workers"`
	// Flags used for the PoW verification.
	Flags config.PowFlags `mapstructure:"smeshing-opts-verifying-powflags"`
}

func DefaultPostVerifyingOpts() PostProofVerifyingOpts {
	workers := runtime.NumCPU() * 3 / 4
	if workers < 1 {
		workers = 1
	}
	return PostProofVerifyingOpts{
		Workers: workers,
		Flags:   config.DefaultVerifyingPowFlags(),
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

var (
	errNotComplete = errors.New("not complete")
	errNotStarted  = errors.New("not started")
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
		K3:            cfg.K3,
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
	id              types.NodeID
	commitmentAtxId types.ATXID

	cfg         PostConfig
	logger      *zap.Logger
	db          *datastore.CachedDB
	goldenATXID types.ATXID

	mu          sync.Mutex                  // mu protects setting the values below.
	lastOpts    *PostSetupOpts              // the last options used to initiate a Post setup session.
	state       PostSetupState              // state is the current state of the Post setup.
	init        *initialization.Initializer // init is the current initializer instance.
	provingOpts PostProvingOpts
}

// NewPostSetupManager creates a new instance of PostSetupManager.
func NewPostSetupManager(id types.NodeID, cfg PostConfig, logger *zap.Logger, db *datastore.CachedDB, goldenATXID types.ATXID, provingOpts PostProvingOpts) (*PostSetupManager, error) {
	mgr := &PostSetupManager{
		id:          id,
		cfg:         cfg,
		logger:      logger,
		db:          db,
		goldenATXID: goldenATXID,
		state:       PostSetupStateNotStarted,
		provingOpts: provingOpts,
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

// Providers returns a list of available compute providers for Post setup.
func (*PostSetupManager) Providers() ([]PostSetupProvider, error) {
	providers, err := initialization.OpenCLProviders()
	if err != nil {
		return nil, err
	}

	providersAlias := make([]PostSetupProvider, len(providers))
	for i, p := range providers {
		providersAlias[i] = PostSetupProvider(p)
	}

	return providersAlias, nil
}

// bestProvider returns the most performant compute provider based on a short benchmarking session.
func (mgr *PostSetupManager) bestProvider() (*PostSetupProvider, error) {
	providers, err := mgr.Providers()
	if err != nil {
		return nil, fmt.Errorf("fetch best provider: %w", err)
	}

	var bestProvider PostSetupProvider
	var maxHS int
	for _, p := range providers {
		hs, err := mgr.Benchmark(p)
		if err != nil {
			return nil, err
		}
		if hs > maxHS {
			maxHS = hs
			bestProvider = p
		}
	}
	return &bestProvider, nil
}

// Benchmark runs a short benchmarking session for a given provider to evaluate its performance.
func (mgr *PostSetupManager) Benchmark(p PostSetupProvider) (int, error) {
	score, err := initialization.Benchmark(initialization.Provider(p))
	if err != nil {
		return score, fmt.Errorf("benchmark GPU: %w", err)
	}

	return score, nil
}

// StartSession starts (or continues) a PoST session. It supports resuming a
// previously started session, and will return an error if a session is already
// in progress. It must be ensured that PrepareInitializer is called once
// before each call to StartSession and that the node is ATX synced.
func (mgr *PostSetupManager) StartSession(ctx context.Context) error {
	// Ensure only one goroutine can execute initialization at a time.
	err := func() error {
		mgr.mu.Lock()
		defer mgr.mu.Unlock()
		if mgr.state != PostSetupStatePrepared {
			return fmt.Errorf("post session not prepared")
		}
		mgr.state = PostSetupStateInProgress
		return nil
	}()
	if err != nil {
		return err
	}
	mgr.logger.Info("post setup session starting",
		zap.Stringer("node_id", mgr.id),
		zap.Stringer("commitment_atx", mgr.commitmentAtxId),
		zap.String("data_dir", mgr.lastOpts.DataDir),
		zap.Uint32("num_units", mgr.lastOpts.NumUnits),
		zap.Uint64("labels_per_unit", mgr.cfg.LabelsPerUnit),
		zap.Stringer("provider", mgr.lastOpts.ProviderID),
	)
	public.InitStart.Set(float64(mgr.lastOpts.NumUnits))
	events.EmitInitStart(mgr.id, mgr.commitmentAtxId)
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
		mgr.logger.Error("post setup session failed due to an issue with the initialization provider", zap.Error(errLabelMismatch))
		mgr.state = PostSetupStateError
		events.EmitInitFailure(mgr.id, mgr.commitmentAtxId, errLabelMismatch)
		return nil
	case err != nil:
		mgr.logger.Error("post setup session failed", zap.Error(err))
		mgr.state = PostSetupStateError
		events.EmitInitFailure(mgr.id, mgr.commitmentAtxId, err)
		return err
	}
	public.InitEnd.Set(float64(mgr.lastOpts.NumUnits))
	events.EmitInitComplete()

	mgr.logger.Info("post setup completed",
		zap.Stringer("node_id", mgr.id),
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
func (mgr *PostSetupManager) PrepareInitializer(ctx context.Context, opts PostSetupOpts) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if mgr.state == PostSetupStatePrepared || mgr.state == PostSetupStateInProgress {
		return fmt.Errorf("post setup session in progress")
	}

	// TODO(mafa): remove this, see https://github.com/spacemeshos/go-spacemesh/issues/4801
	if opts.ProviderID.Value() != nil && *opts.ProviderID.Value() == -1 {
		mgr.logger.Warn("DEPRECATED: auto-determining compute provider is deprecated, please specify a valid provider ID in the config file")

		p, err := mgr.bestProvider()
		if err != nil {
			return err
		}

		mgr.logger.Warn("DEPRECATED: found best compute provider",
			zap.Uint32("id", p.ID),
			zap.String("model", p.Model),
			zap.Stringer("device type", p.DeviceType),
		)
		mgr.logger.Sugar().Warnf("DEPRECATED: please update your config file: {\"smeshing\": {\"smeshing-opts\": {\"smeshing-opts-provider\": %d }}}", p.ID)
		opts.ProviderID.SetInt64(int64(p.ID))
	}

	var err error
	mgr.commitmentAtxId, err = mgr.commitmentAtx(ctx, opts.DataDir)
	if err != nil {
		return err
	}

	newInit, err := initialization.NewInitializer(
		initialization.WithNodeId(mgr.id.Bytes()),
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

func (mgr *PostSetupManager) CommitmentAtx() (types.ATXID, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if mgr.commitmentAtxId != types.EmptyATXID {
		return mgr.commitmentAtxId, nil
	}
	return types.EmptyATXID, errNotStarted
}

func (mgr *PostSetupManager) commitmentAtx(ctx context.Context, dataDir string) (types.ATXID, error) {
	m, err := initialization.LoadMetadata(dataDir)
	switch {
	case err == nil:
		return types.ATXID(types.BytesToHash(m.CommitmentAtxId)), nil
	case errors.Is(err, initialization.ErrStateMetadataFileMissing):
		// if this node has already published an ATX, get its initial ATX and from it the commitment ATX
		atxId, err := atxs.GetFirstIDByNodeID(mgr.db, mgr.id)
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
	atx, err := atxs.GetIDWithMaxHeight(mgr.db, types.EmptyNodeID)
	switch {
	case errors.Is(err, sql.ErrNotFound):
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

// VRFNonce returns the VRF nonce found during initialization.
func (mgr *PostSetupManager) VRFNonce() (*types.VRFPostIndex, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if mgr.state != PostSetupStateComplete {
		return nil, errNotComplete
	}

	return (*types.VRFPostIndex)(mgr.init.Nonce()), nil
}

// LastOpts returns the Post setup last session options.
func (mgr *PostSetupManager) LastOpts() *PostSetupOpts {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	return mgr.lastOpts
}

// Config returns the Post protocol config.
func (mgr *PostSetupManager) Config() PostConfig {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	return mgr.cfg
}
