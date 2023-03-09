package activation

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/gpu"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/proving"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

// PostSetupComputeProvider represent a compute provider for Post setup data creation.
type PostSetupComputeProvider initialization.ComputeProvider

// PostConfig is the configuration of the Post protocol, used for data creation, proofs generation and validation.
type PostConfig struct {
	MinNumUnits   uint32 `mapstructure:"post-min-numunits"`
	MaxNumUnits   uint32 `mapstructure:"post-max-numunits"`
	BitsPerLabel  uint8  `mapstructure:"post-bits-per-label"`
	LabelsPerUnit uint64 `mapstructure:"post-labels-per-unit"`
	K1            uint32 `mapstructure:"post-k1"`
	K2            uint32 `mapstructure:"post-k2"`
	B             uint32 `mapstructure:"post-b"`
	N             uint32 `mapstructure:"post-n"`
}

// PostSetupOpts are the options used to initiate a Post setup data creation session,
// either via the public smesher API, or on node launch (via cmd args).
type PostSetupOpts struct {
	DataDir           string `mapstructure:"smeshing-opts-datadir"`
	NumUnits          uint32 `mapstructure:"smeshing-opts-numunits"`
	MaxFileSize       uint64 `mapstructure:"smeshing-opts-maxfilesize"`
	ComputeProviderID int    `mapstructure:"smeshing-opts-provider"`
	Throttle          bool   `mapstructure:"smeshing-opts-throttle"`
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
	return (PostConfig)(config.DefaultConfig())
}

// DefaultPostSetupOpts defines the default options for Post setup.
func DefaultPostSetupOpts() PostSetupOpts {
	return (PostSetupOpts)(config.DefaultInitOpts())
}

// PostSetupManager implements the PostProvider interface.
type PostSetupManager struct {
	id              types.NodeID
	commitmentAtxId types.ATXID

	cfg         PostConfig
	logger      log.Log
	db          *datastore.CachedDB
	goldenATXID types.ATXID

	mu       sync.Mutex                  // mu protects setting the values below.
	lastOpts *PostSetupOpts              // the last options used to initiate a Post setup session.
	state    PostSetupState              // state is the current state of the Post setup.
	init     *initialization.Initializer // init is the current initializer instance.
}

// NewPostSetupManager creates a new instance of PostSetupManager.
func NewPostSetupManager(id types.NodeID, cfg PostConfig, logger log.Log, db *datastore.CachedDB, goldenATXID types.ATXID) (*PostSetupManager, error) {
	mgr := &PostSetupManager{
		id:          id,
		cfg:         cfg,
		logger:      logger,
		db:          db,
		goldenATXID: goldenATXID,
		state:       PostSetupStateNotStarted,
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
	default:
		return &PostSetupStatus{
			State:            mgr.state,
			NumLabelsWritten: mgr.init.NumLabelsWritten(),
			LastOpts:         mgr.lastOpts,
		}
	}
}

// ComputeProviders returns a list of available compute providers for Post setup.
func (*PostSetupManager) ComputeProviders() []PostSetupComputeProvider {
	providers := initialization.Providers()

	providersAlias := make([]PostSetupComputeProvider, len(providers))
	for i, p := range providers {
		providersAlias[i] = PostSetupComputeProvider(p)
	}

	return providersAlias
}

// BestProvider returns the most performant compute provider based on a short benchmarking session.
func (mgr *PostSetupManager) BestProvider() (*PostSetupComputeProvider, error) {
	var bestProvider PostSetupComputeProvider
	var maxHS int
	for _, p := range mgr.ComputeProviders() {
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
func (mgr *PostSetupManager) Benchmark(p PostSetupComputeProvider) (int, error) {
	score, err := gpu.Benchmark(initialization.ComputeProvider(p))
	if err != nil {
		return score, fmt.Errorf("benchmark GPU: %w", err)
	}

	return score, nil
}

// StartSession starts (or continues) a PoST session. It supports resuming a previously started
// session, and will return an error if a session is already in progress.
//
// Ensure that before calling this method, the node is ATX synced.
func (mgr *PostSetupManager) StartSession(ctx context.Context, opts PostSetupOpts) error {
	if err := mgr.prepareInitializer(ctx, opts); err != nil {
		return err
	}

	mgr.logger.With().Info("post setup session starting",
		log.String("node_id", mgr.id.String()),
		log.String("commitment_atx", mgr.commitmentAtxId.String()),
		log.String("data_dir", opts.DataDir),
		log.String("num_units", fmt.Sprintf("%d", opts.NumUnits)),
		log.String("labels_per_unit", fmt.Sprintf("%d", mgr.cfg.LabelsPerUnit)),
		log.String("bits_per_label", fmt.Sprintf("%d", mgr.cfg.BitsPerLabel)),
		log.String("provider", fmt.Sprintf("%d", opts.ComputeProviderID)),
	)

	err := mgr.init.Initialize(ctx)

	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	switch {
	case errors.Is(err, context.Canceled):
		mgr.logger.Info("post setup session was stopped")
		mgr.state = PostSetupStateStopped
		return err
	case err != nil:
		mgr.logger.With().Error("post setup session failed", log.Err(err))
		mgr.state = PostSetupStateError
		return err
	}

	mgr.logger.With().Info("post setup completed",
		log.String("node_id", mgr.id.String()),
		log.String("commitment_atx", mgr.commitmentAtxId.String()),
		log.String("data_dir", opts.DataDir),
		log.String("num_units", fmt.Sprintf("%d", opts.NumUnits)),
		log.String("labels_per_unit", fmt.Sprintf("%d", mgr.cfg.LabelsPerUnit)),
		log.String("bits_per_label", fmt.Sprintf("%d", mgr.cfg.BitsPerLabel)),
		log.String("provider", fmt.Sprintf("%d", opts.ComputeProviderID)),
	)
	mgr.state = PostSetupStateComplete
	return nil
}

func (mgr *PostSetupManager) prepareInitializer(ctx context.Context, opts PostSetupOpts) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if mgr.state == PostSetupStateInProgress {
		return fmt.Errorf("post setup session in progress")
	}

	if opts.ComputeProviderID == config.BestProviderID {
		p, err := mgr.BestProvider()
		if err != nil {
			return err
		}

		mgr.logger.Info("found best compute provider: id: %d, model: %v, computeAPI: %v", p.ID, p.Model, p.ComputeAPI)
		opts.ComputeProviderID = int(p.ID)
	}

	var err error
	mgr.commitmentAtxId, err = mgr.commitmentAtx(ctx, opts.DataDir)
	if err != nil {
		return err
	}

	newInit, err := initialization.NewInitializer(
		initialization.WithNodeId(mgr.id.Bytes()),
		initialization.WithCommitmentAtxId(mgr.commitmentAtxId.Bytes()),
		initialization.WithConfig(config.Config(mgr.cfg)),
		initialization.WithInitOpts(config.InitOpts(opts)),
		initialization.WithLogger(mgr.logger),
	)
	if err != nil {
		mgr.state = PostSetupStateError
		return fmt.Errorf("new initializer: %w", err)
	}

	mgr.state = PostSetupStateInProgress
	mgr.init = newInit
	mgr.lastOpts = &opts
	return nil
}

func (mgr *PostSetupManager) CommitmentAtx() (types.ATXID, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if mgr.commitmentAtxId != *types.EmptyATXID {
		return mgr.commitmentAtxId, nil
	}
	return *types.EmptyATXID, errNotStarted
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
				return *types.EmptyATXID, err
			}
			return *atx.CommitmentATX, nil
		}

		// if this node has not published an ATX select the best ATX with `findCommitmentAtx`
		return mgr.findCommitmentAtx(ctx)
	default:
		return *types.EmptyATXID, fmt.Errorf("load metadata: %w", err)
	}
}

// findCommitmentAtx determines the best commitment ATX to use for the node.
// It will use the ATX with the highest height seen by the node and defaults to the goldenATX,
// when no ATXs have yet been published.
func (mgr *PostSetupManager) findCommitmentAtx(ctx context.Context) (types.ATXID, error) {
	atx, err := atxs.GetAtxIDWithMaxHeight(mgr.db)
	switch {
	case errors.Is(err, sql.ErrNotFound):
		mgr.logger.With().Info("using golden atx as commitment atx")
		return mgr.goldenATXID, nil
	case err != nil:
		return *types.EmptyATXID, fmt.Errorf("get commitment atx: %w", err)
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

// GenerateProof generates a new Post.
func (mgr *PostSetupManager) GenerateProof(ctx context.Context, challenge []byte) (*types.Post, *types.PostMetadata, error) {
	mgr.mu.Lock()

	if mgr.state != PostSetupStateComplete {
		mgr.mu.Unlock()
		return nil, nil, errNotComplete
	}
	mgr.mu.Unlock()

	proof, proofMetadata, err := proving.Generate(ctx, challenge, config.Config(mgr.cfg), mgr.logger,
		proving.WithDataSource(config.Config(mgr.cfg), mgr.id.Bytes(), mgr.commitmentAtxId.Bytes(), mgr.lastOpts.DataDir),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("generate proof: %w", err)
	}

	p := (*types.Post)(proof)
	m := &types.PostMetadata{
		Challenge:     proofMetadata.Challenge,
		BitsPerLabel:  proofMetadata.BitsPerLabel,
		LabelsPerUnit: proofMetadata.LabelsPerUnit,
		K1:            proofMetadata.K1,
		K2:            proofMetadata.K2,
		B:             proofMetadata.B,
		N:             proofMetadata.N,
	}
	return p, m, nil
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
