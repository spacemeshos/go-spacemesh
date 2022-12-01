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

	atypes "github.com/spacemeshos/go-spacemesh/activation/types"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate mockgen -package=mocks -destination=./mocks/post.go -source=./post.go

// DefaultPostConfig defines the default configuration for Post.
func DefaultPostConfig() atypes.PostConfig {
	return (atypes.PostConfig)(config.DefaultConfig())
}

// DefaultPostSetupOpts defines the default options for Post setup.
func DefaultPostSetupOpts() atypes.PostSetupOpts {
	return (atypes.PostSetupOpts)(config.DefaultInitOpts())
}

// PostSetupProvider defines the functionality required for Post setup.
type PostSetupProvider interface {
	Status() *atypes.PostSetupStatus
	ComputeProviders() []atypes.PostSetupComputeProvider
	Benchmark(p atypes.PostSetupComputeProvider) (int, error)
	StartSession(context context.Context, opts atypes.PostSetupOpts, commitmentAtx types.ATXID) error
	Reset() error
	GenerateProof(challenge []byte, commitmentAtx types.ATXID) (*types.Post, *types.PostMetadata, error)
	LastOpts() *atypes.PostSetupOpts
	Config() atypes.PostConfig
}

// PostSetupManager implements the PostProvider interface.
type PostSetupManager struct {
	mu sync.Mutex

	id          types.NodeID
	cfg         atypes.PostConfig
	logger      log.Log
	db          *datastore.CachedDB
	goldenATXID types.ATXID

	state atypes.PostSetupState

	// init is the current initializer instance. It is being
	// replaced at the beginning of every data creation session.
	init *initialization.Initializer

	lastOpts *atypes.PostSetupOpts
}

// NewPostSetupManager creates a new instance of PostSetupManager.
func NewPostSetupManager(id types.NodeID, cfg atypes.PostConfig, logger log.Log, db *datastore.CachedDB, goldenATXID types.ATXID) (*PostSetupManager, error) {
	mgr := &PostSetupManager{
		id:          id,
		cfg:         cfg,
		logger:      logger,
		db:          db,
		goldenATXID: goldenATXID,
		state:       atypes.PostSetupStateNotStarted,
	}

	return mgr, nil
}

var errNotComplete = errors.New("not complete")

// Status returns the setup current status.
func (mgr *PostSetupManager) Status() *atypes.PostSetupStatus {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	switch mgr.state {
	case atypes.PostSetupStateNotStarted:
		return &atypes.PostSetupStatus{
			State: mgr.state,
		}
	default:
		return &atypes.PostSetupStatus{
			State:            mgr.state,
			NumLabelsWritten: mgr.init.NumLabelsWritten(),
			LastOpts:         mgr.lastOpts,
		}
	}
}

// ComputeProviders returns a list of available compute providers for Post setup.
func (mgr *PostSetupManager) ComputeProviders() []atypes.PostSetupComputeProvider {
	providers := initialization.Providers()

	providersAlias := make([]atypes.PostSetupComputeProvider, len(providers))
	for i, p := range providers {
		providersAlias[i] = atypes.PostSetupComputeProvider(p)
	}

	return providersAlias
}

// BestProvider returns the most performant compute provider based on a short benchmarking session.
func (mgr *PostSetupManager) BestProvider() (*atypes.PostSetupComputeProvider, error) {
	var bestProvider atypes.PostSetupComputeProvider
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
func (mgr *PostSetupManager) Benchmark(p atypes.PostSetupComputeProvider) (int, error) {
	score, err := gpu.Benchmark(initialization.ComputeProvider(p))
	if err != nil {
		return score, fmt.Errorf("benchmark GPU: %w", err)
	}

	return score, nil
}

// StartSession starts (or continues) a data creation session.
// It supports resuming a previously started session, as well as changing the Post setup options (e.g., number of units)
// after initial setup.
func (mgr *PostSetupManager) StartSession(ctx context.Context, opts atypes.PostSetupOpts, commitmentAtx types.ATXID) error {
	mgr.mu.Lock()

	switch mgr.state {
	case atypes.PostSetupStateInProgress:
		mgr.mu.Unlock()
		return fmt.Errorf("post setup session in progress")

	case atypes.PostSetupStateComplete:
		// Check whether the new request invalidates the current status.
		lastOpts := mgr.lastOpts
		invalidate := opts.DataDir != lastOpts.DataDir || opts.NumUnits != lastOpts.NumUnits || opts.NumFiles != lastOpts.NumFiles
		if !invalidate {
			// Already complete.
			mgr.mu.Unlock()
			return nil
		}
	}

	if opts.ComputeProviderID == config.BestProviderID {
		p, err := mgr.BestProvider()
		if err != nil {
			mgr.mu.Unlock()
			return err
		}

		mgr.logger.Info("found best compute provider: id: %d, model: %v, computeAPI: %v", p.ID, p.Model, p.ComputeAPI)
		opts.ComputeProviderID = int(p.ID)
	}

	newInit, err := initialization.NewInitializer(
		initialization.WithNodeId(mgr.id.Bytes()),
		initialization.WithCommitmentAtxId(commitmentAtx.Bytes()),
		initialization.WithConfig(config.Config(mgr.cfg)),
		initialization.WithInitOpts(config.InitOpts(opts)),
		initialization.WithLogger(mgr.logger),
	)
	if err != nil {
		mgr.state = atypes.PostSetupStateError
		mgr.mu.Unlock()
		return fmt.Errorf("new initializer: %w", err)
	}

	mgr.state = atypes.PostSetupStateInProgress
	mgr.init = newInit
	mgr.lastOpts = &opts

	mgr.mu.Unlock()
	mgr.logger.With().Info("post setup session starting",
		log.String("data_dir", opts.DataDir),
		log.String("num_units", fmt.Sprintf("%d", opts.NumUnits)),
		log.String("labels_per_unit", fmt.Sprintf("%d", mgr.cfg.LabelsPerUnit)),
		log.String("bits_per_label", fmt.Sprintf("%d", mgr.cfg.BitsPerLabel)),
		log.String("provider", fmt.Sprintf("%d", opts.ComputeProviderID)),
	)

	if err := newInit.Initialize(ctx); err != nil {
		mgr.mu.Lock()
		defer mgr.mu.Unlock()

		switch {
		case errors.Is(err, context.Canceled):
			mgr.logger.Info("post setup session was stopped")
			mgr.state = atypes.PostSetupStateNotStarted
			return err
		default:
			mgr.state = atypes.PostSetupStateError
		}
		return err
	}

	mgr.logger.With().Info("post setup completed",
		log.String("datadir", opts.DataDir),
		log.String("num_units", fmt.Sprintf("%d", opts.NumUnits)),
		log.String("labels_per_unit", fmt.Sprintf("%d", mgr.cfg.LabelsPerUnit)),
		log.String("bits_per_label", fmt.Sprintf("%d", mgr.cfg.BitsPerLabel)),
	)

	mgr.mu.Lock()
	mgr.state = atypes.PostSetupStateComplete
	mgr.mu.Unlock()

	return nil
}

// Reset deletes the data file(s).
func (mgr *PostSetupManager) Reset() error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if err := mgr.init.Reset(); err != nil {
		return fmt.Errorf("reset: %w", err)
	}

	// Reset internal state.
	mgr.state = atypes.PostSetupStateNotStarted

	return nil
}

// GenerateProof generates a new Post.
func (mgr *PostSetupManager) GenerateProof(challenge []byte, commitmentAtx types.ATXID) (*types.Post, *types.PostMetadata, error) {
	mgr.mu.Lock()

	if mgr.state != atypes.PostSetupStateComplete {
		mgr.mu.Unlock()
		return nil, nil, errNotComplete
	}
	mgr.mu.Unlock()

	prover, err := proving.NewProver(config.Config(mgr.cfg), mgr.lastOpts.DataDir, mgr.id.Bytes(), commitmentAtx.Bytes())
	if err != nil {
		return nil, nil, fmt.Errorf("new prover: %w", err)
	}

	prover.SetLogger(mgr.logger)
	proof, proofMetadata, err := prover.GenerateProof(challenge)
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
	}
	return p, m, nil
}

// GetPowNonce returns the PoW nonce found during initialization.
func (mgr *PostSetupManager) GetPowNonce() (uint64, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if mgr.state != atypes.PostSetupStateComplete {
		return 0, errNotComplete
	}

	return *mgr.init.Nonce(), nil
}

// LastOpts returns the Post setup last session options.
func (mgr *PostSetupManager) LastOpts() *atypes.PostSetupOpts {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	return mgr.lastOpts
}

// Config returns the Post protocol config.
func (mgr *PostSetupManager) Config() atypes.PostConfig {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	return mgr.cfg
}
