package activation

import (
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/gpu"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/proving"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate mockgen -package=mocks -destination=./mocks/post_mock.go -source=./post.go

// DefaultPostConfig defines the default configuration for Post.
func DefaultPostConfig() types.PostConfig {
	return (types.PostConfig)(config.DefaultConfig())
}

// DefaultPostSetupOpts defines the default options for Post setup.
func DefaultPostSetupOpts() types.PostSetupOpts {
	return (types.PostSetupOpts)(config.DefaultInitOpts())
}

// PostSetupProvider defines the functionality required for Post setup.
type PostSetupProvider interface {
	Status() *types.PostSetupStatus
	StatusChan() <-chan *types.PostSetupStatus
	ComputeProviders() []initialization.ComputeProvider
	Benchmark(p initialization.ComputeProvider) (int, error)
	StartSession(opts types.PostSetupOpts, commitmentAtx types.ATXID) (chan struct{}, error)
	StopSession(deleteFiles bool) error
	GenerateProof(challenge []byte, commitmentAtx types.ATXID) (*types.Post, *types.PostMetadata, error)
	LastError() error
	LastOpts() *types.PostSetupOpts
	Config() types.PostConfig
}

// A compile time check to ensure that PostSetupManager fully implements the PostSetupProvider interface.
var _ PostSetupProvider = (*PostSetupManager)(nil)

// PostSetupManager implements the PostProvider interface.
type PostSetupManager struct {
	mu sync.Mutex

	id          types.NodeID
	cfg         types.PostConfig
	logger      log.Log
	db          *datastore.CachedDB
	goldenATXID types.ATXID

	state             types.PostSetupState
	initCompletedChan chan struct{}

	// init is the current initializer instance. It is being
	// replaced at the beginning of every data creation session.
	init *initialization.Initializer

	lastOpts *types.PostSetupOpts
	lastErr  error

	// startedChan indicates whether a data creation session has started.
	// The channel instance is replaced in the end of the session.
	startedChan chan struct{}

	// doneChan indicates whether the current data creation session has finished.
	// The channel instance is replaced in the beginning of the session.
	doneChan chan struct{}
}

const (
	postSetupStateNotStarted types.PostSetupState = 1 + iota
	postSetupStateInProgress
	postSetupStateComplete
	postSetupStateError
)

// NewPostSetupManager creates a new instance of PostSetupManager.
func NewPostSetupManager(id types.NodeID, cfg types.PostConfig, logger log.Log, db *datastore.CachedDB, goldenATXID types.ATXID) (*PostSetupManager, error) {
	mgr := &PostSetupManager{
		id:                id,
		cfg:               cfg,
		logger:            logger,
		db:                db,
		goldenATXID:       goldenATXID,
		state:             postSetupStateNotStarted,
		initCompletedChan: make(chan struct{}),
		startedChan:       make(chan struct{}),
	}

	return mgr, nil
}

var errNotComplete = errors.New("not complete")

// Status returns the setup current status.
func (mgr *PostSetupManager) Status() *types.PostSetupStatus {
	status := &types.PostSetupStatus{}

	mgr.mu.Lock()
	status.State = mgr.state
	init := mgr.init
	mgr.mu.Unlock()

	if status.State == postSetupStateNotStarted {
		return status
	}

	status.NumLabelsWritten = init.SessionNumLabelsWritten()
	status.LastOpts = mgr.LastOpts()
	status.LastError = mgr.LastError()

	return status
}

// StatusChan returns a channel with status updates of the setup current or the upcoming session.
func (mgr *PostSetupManager) StatusChan() <-chan *types.PostSetupStatus {
	// Wait for session to start because only then the initializer instance
	// used for retrieving the progress updates is already set.
	mgr.mu.Lock()
	startedChan := mgr.startedChan
	mgr.mu.Unlock()

	<-startedChan

	statusChan := make(chan *types.PostSetupStatus, 1024)
	go func() {
		defer close(statusChan)

		initialStatus := mgr.Status()
		statusChan <- initialStatus

		mgr.mu.Lock()
		init := mgr.init
		mgr.mu.Unlock()

		ch := init.SessionNumLabelsWrittenChan()
		for numLabelsWritten := range ch {
			status := *initialStatus
			status.NumLabelsWritten = numLabelsWritten
			statusChan <- &status
		}

		if finalStatus := mgr.Status(); finalStatus.LastError != nil {
			statusChan <- finalStatus
		}
	}()

	return statusChan
}

// ComputeProviders returns a list of available compute providers for Post setup.
func (mgr *PostSetupManager) ComputeProviders() []initialization.ComputeProvider {
	providers := initialization.Providers()

	providersAlias := make([]initialization.ComputeProvider, len(providers))
	for i, p := range providers {
		providersAlias[i] = initialization.ComputeProvider(p)
	}

	return providersAlias
}

// BestProvider returns the most performant compute provider based on a short benchmarking session.
func (mgr *PostSetupManager) BestProvider() (*initialization.ComputeProvider, error) {
	var bestProvider initialization.ComputeProvider
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
func (mgr *PostSetupManager) Benchmark(p initialization.ComputeProvider) (int, error) {
	score, err := gpu.Benchmark(initialization.ComputeProvider(p))
	if err != nil {
		return score, fmt.Errorf("benchmark GPU: %w", err)
	}

	return score, nil
}

// StartSession starts (or continues) a data creation session.
// It supports resuming a previously started session, as well as changing the Post setup options (e.g., number of units)
// after initial setup.
func (mgr *PostSetupManager) StartSession(opts types.PostSetupOpts, commitmentAtx types.ATXID) (chan struct{}, error) {
	state := mgr.getState()

	if state == postSetupStateInProgress {
		return nil, fmt.Errorf("post setup session in progress")
	}
	if state == postSetupStateComplete {
		// Check whether the new request invalidates the current status.
		lastOpts := mgr.LastOpts()
		invalidate := opts.DataDir != lastOpts.DataDir || opts.NumUnits != lastOpts.NumUnits
		if !invalidate {
			// Already complete.
			return mgr.doneChan, nil
		}

		mgr.mu.Lock()
		mgr.initCompletedChan = make(chan struct{})
		mgr.mu.Unlock()
	}

	mgr.mu.Lock()
	mgr.state = postSetupStateInProgress
	mgr.mu.Unlock()

	if opts.ComputeProviderID == config.BestProviderID {
		p, err := mgr.BestProvider()
		if err != nil {
			return nil, err
		}

		mgr.logger.Info("found best compute provider: id: %d, model: %v, computeAPI: %v", p.ID, p.Model, p.ComputeAPI)
		opts.ComputeProviderID = int(p.ID)
	}

	commitment := GetCommitmentBytes(mgr.id, commitmentAtx)
	newInit, err := initialization.NewInitializer(config.Config(mgr.cfg), config.InitOpts(opts), commitment)
	if err != nil {
		mgr.mu.Lock()
		mgr.state = postSetupStateError
		mgr.lastErr = err
		mgr.mu.Unlock()
		return nil, fmt.Errorf("new initializer: %w", err)
	}

	newInit.SetLogger(mgr.logger)

	mgr.mu.Lock()
	mgr.init = newInit
	mgr.lastOpts = &opts
	mgr.lastErr = nil
	close(mgr.startedChan)
	mgr.doneChan = make(chan struct{})
	mgr.mu.Unlock()

	go func() {
		defer func() {
			mgr.mu.Lock()
			mgr.startedChan = make(chan struct{})
			close(mgr.doneChan)
			mgr.mu.Unlock()
		}()

		mgr.logger.With().Info("post setup session starting",
			log.String("data_dir", opts.DataDir),
			log.String("num_units", fmt.Sprintf("%d", opts.NumUnits)),
			log.String("labels_per_unit", fmt.Sprintf("%d", mgr.cfg.LabelsPerUnit)),
			log.String("bits_per_label", fmt.Sprintf("%d", mgr.cfg.BitsPerLabel)),
			log.String("provider", fmt.Sprintf("%d", opts.ComputeProviderID)),
		)

		if err := newInit.Initialize(); err != nil {
			mgr.mu.Lock()
			defer mgr.mu.Unlock()

			if errors.Is(err, initialization.ErrStopped) {
				mgr.logger.Info("post setup session stopped")
				mgr.state = postSetupStateNotStarted
			} else {
				mgr.state = postSetupStateError
				mgr.lastErr = err
			}
			return
		}

		mgr.logger.With().Info("post setup completed",
			log.String("datadir", opts.DataDir),
			log.String("num_units", fmt.Sprintf("%d", opts.NumUnits)),
			log.String("labels_per_unit", fmt.Sprintf("%d", mgr.cfg.LabelsPerUnit)),
			log.String("bits_per_label", fmt.Sprintf("%d", mgr.cfg.BitsPerLabel)),
		)

		mgr.mu.Lock()
		mgr.state = postSetupStateComplete
		close(mgr.initCompletedChan)
		mgr.mu.Unlock()
	}()

	return mgr.doneChan, nil
}

// StopSession stops the current Post setup data creation session
// and optionally attempts to delete the data file(s).
func (mgr *PostSetupManager) StopSession(deleteFiles bool) error {
	mgr.mu.Lock()
	state := mgr.state
	init := mgr.init
	doneChan := mgr.doneChan
	mgr.mu.Unlock()

	if state == postSetupStateInProgress {
		if err := init.Stop(); err != nil {
			return fmt.Errorf("stop: %w", err)
		}

		// Block until the current data creation session will be finished.
		<-doneChan
	}

	if deleteFiles {
		if err := init.Reset(); err != nil {
			return fmt.Errorf("reset: %w", err)
		}

		mgr.mu.Lock()
		// Reset internal state.
		mgr.state = postSetupStateNotStarted
		mgr.initCompletedChan = make(chan struct{})
		mgr.mu.Unlock()
	}

	return nil
}

// GenerateProof generates a new Post.
func (mgr *PostSetupManager) GenerateProof(challenge []byte, commitmentAtx types.ATXID) (*types.Post, *types.PostMetadata, error) {
	state := mgr.getState()

	if state != postSetupStateComplete {
		return nil, nil, errNotComplete
	}

	// TODO(mafa): id field in post package should be renamed to commitment otherwise error messages are confusing
	commitment := GetCommitmentBytes(mgr.id, commitmentAtx)
	prover, err := proving.NewProver(config.Config(mgr.cfg), mgr.LastOpts().DataDir, commitment)
	if err != nil {
		return nil, nil, fmt.Errorf("new prover: %w", err)
	}

	prover.SetLogger(mgr.logger)
	proof, proofMetadata, err := prover.GenerateProof(challenge)
	if err != nil {
		return nil, nil, fmt.Errorf("generate proof: %w", err)
	}

	m := new(types.PostMetadata)
	m.Challenge = proofMetadata.Challenge
	m.BitsPerLabel = byte(proofMetadata.BitsPerLabel)
	m.LabelsPerUnit = uint64(proofMetadata.LabelsPerUnit)
	m.K1 = uint32(proofMetadata.K1)
	m.K2 = uint32(proofMetadata.K2)

	p := (*types.Post)(proof)

	return p, m, nil
}

// LastError returns the Post setup last error.
func (mgr *PostSetupManager) LastError() error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	return mgr.lastErr
}

// LastOpts returns the Post setup last session options.
func (mgr *PostSetupManager) LastOpts() *types.PostSetupOpts {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	return mgr.lastOpts
}

// Config returns the Post protocol config.
func (mgr *PostSetupManager) Config() types.PostConfig {
	return mgr.cfg
}

func (mgr *PostSetupManager) getState() types.PostSetupState {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	return mgr.state
}

func GetCommitmentBytes(id types.NodeID, commitmentAtx types.ATXID) []byte {
	h := hash.Sum(append(id.ToBytes(), commitmentAtx.Bytes()...))
	return h[:]
}
