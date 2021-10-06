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
	"github.com/spacemeshos/go-spacemesh/log"
)

type (
	// PostSetupComputeProvider represent a compute provider for Post setup data creation.
	PostSetupComputeProvider initialization.ComputeProvider
)

// PostConfig is the configuration of the Post protocol, used for data creation, proofs generation and validation.
type PostConfig struct {
	BitsPerLabel  uint `mapstructure:"post-bits-per-label"`
	LabelsPerUnit uint `mapstructure:"post-labels-per-unit"`
	MinNumUnits   uint `mapstructure:"post-min-numunits"`
	MaxNumUnits   uint `mapstructure:"post-max-numunits"`
	K1            uint `mapstructure:"post-k1"`
	K2            uint `mapstructure:"post-k2"`
}

// PostSetupOpts are the options used to initiate a Post setup data creation session,
// either via the public smesher API, or on node launch (via cmd args).
type PostSetupOpts struct {
	DataDir           string `mapstructure:"smeshing-opts-datadir"`
	NumUnits          uint   `mapstructure:"smeshing-opts-numunits"`
	NumFiles          uint   `mapstructure:"smeshing-opts-numfiles"`
	ComputeProviderID int    `mapstructure:"smeshing-opts-provider"`
	Throttle          bool   `mapstructure:"smeshing-opts-throttle"`
}

// DefaultPostConfig defines the default configuration for Post.
func DefaultPostConfig() PostConfig {
	return (PostConfig)(config.DefaultConfig())
}

// DefaultPostSetupOpts defines the default options for Post setup.
func DefaultPostSetupOpts() PostSetupOpts {
	return (PostSetupOpts)(config.DefaultInitOpts())
}

// PostSetupProvider defines the functionality required for Post setup.
type PostSetupProvider interface {
	Status() *PostSetupStatus
	StatusChan() <-chan *PostSetupStatus
	ComputeProviders() []PostSetupComputeProvider
	Benchmark(p PostSetupComputeProvider) (int, error)
	StartSession(opts PostSetupOpts) (chan struct{}, error)
	StopSession(deleteFiles bool) error
	GenerateProof(challenge []byte) (*types.Post, *types.PostMetadata, error)
	LastError() error
	LastOpts() *PostSetupOpts
	Config() PostConfig
}

// A compile time check to ensure that PostSetupManager fully implements the PostSetupProvider interface.
var _ PostSetupProvider = (*PostSetupManager)(nil)

// PostSetupManager implements the PostProvider interface.
type PostSetupManager struct {
	mu sync.RWMutex

	id     []byte
	cfg    PostConfig
	logger log.Log

	// stopMu  sync.Mutex

	state             postSetupState
	initCompletedChan chan struct{}

	// init is the current initializer instance. It is being
	// replaced at the beginning of every data creation session.
	initMu sync.RWMutex
	init   *initialization.Initializer

	lastOpts *PostSetupOpts
	lastErr  error

	startedChanMu sync.RWMutex
	// startedChan indicates whether a data creation session has started.
	// The channel instance is replaced in the end of the session.
	startedChan chan struct{}

	// doneChan indicates whether the current data creation session has finished.
	// The channel instance is replaced in the beginning of the session.
	doneChan chan struct{}
}

type postSetupState int32

const (
	postSetupStateNotStarted postSetupState = 1 + iota
	postSetupStateInProgress
	postSetupStateComplete
	postSetupStateError
)

// PostSetupStatus represents a status snapshot of the Post setup.
type PostSetupStatus struct {
	State            postSetupState
	NumLabelsWritten uint64
	LastOpts         *PostSetupOpts
	LastError        error
}

// NewPostSetupManager creates a new instance of PostSetupManager.
func NewPostSetupManager(id []byte, cfg PostConfig, logger log.Log) (*PostSetupManager, error) {
	mgr := &PostSetupManager{
		id:                id,
		cfg:               cfg,
		logger:            logger,
		state:             postSetupStateNotStarted,
		initCompletedChan: make(chan struct{}),
		startedChan:       make(chan struct{}),
	}

	return mgr, nil
}

var errNotComplete = errors.New("not complete")

// Status returns the setup current status.
func (mgr *PostSetupManager) Status() *PostSetupStatus {
	status := &PostSetupStatus{}
	status.State = mgr.getState()

	if status.State == postSetupStateNotStarted {
		return status
	}

	status.NumLabelsWritten = mgr.getInit().SessionNumLabelsWritten()
	status.LastOpts = mgr.LastOpts()
	status.LastError = mgr.LastError()

	return status
}

// StatusChan returns a channel with status updates of the setup current or the upcoming session.
func (mgr *PostSetupManager) StatusChan() <-chan *PostSetupStatus {
	// Wait for session to start because only then the initializer instance
	// used for retrieving the progress updates is already set.
	mgr.startedChanMu.RLock()
	startedChan := mgr.startedChan
	mgr.startedChanMu.RUnlock()

	<-startedChan

	statusChan := make(chan *PostSetupStatus, 1024)
	go func() {
		defer close(statusChan)

		initialStatus := mgr.Status()
		statusChan <- initialStatus

		ch := mgr.getInit().SessionNumLabelsWrittenChan()
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
func (mgr *PostSetupManager) ComputeProviders() []PostSetupComputeProvider {
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

// StartSession starts (or continues) a data creation session.
// It supports resuming a previously started session, as well as changing the Post setup options (e.g., number of units)
// after initial setup.
func (mgr *PostSetupManager) StartSession(opts PostSetupOpts) (chan struct{}, error) {
	if mgr.getState() == postSetupStateInProgress {
		return nil, fmt.Errorf("post setup session in progress")
	}
	if mgr.getState() == postSetupStateComplete {
		// Check whether the new request invalidates the current status.
		lastOpts := mgr.LastOpts()
		invalidate := opts.DataDir != lastOpts.DataDir || opts.NumUnits != lastOpts.NumUnits
		if !invalidate {
			// Already complete.
			return mgr.doneChan, nil
		}
		mgr.initCompletedChan = make(chan struct{})
	}

	mgr.setState(postSetupStateInProgress)

	if opts.ComputeProviderID == config.BestProviderID {
		p, err := mgr.BestProvider()
		if err != nil {
			return nil, err
		}

		mgr.logger.Info("found best compute provider: id: %d, model: %v, computeAPI: %v", p.ID, p.Model, p.ComputeAPI)
		opts.ComputeProviderID = int(p.ID)
	}

	newInit, err := initialization.NewInitializer(config.Config(mgr.cfg), config.InitOpts(opts), mgr.id)
	if err != nil {
		mgr.setState(postSetupStateError)
		mgr.setLastErr(err)
		return nil, fmt.Errorf("new initializer: %w", err)
	}

	newInit.SetLogger(mgr.logger)

	mgr.setInit(newInit)

	mgr.setLastOpts(&opts)
	mgr.setLastErr(nil)

	mgr.startedChanMu.Lock()
	close(mgr.startedChan)
	mgr.startedChanMu.Unlock()

	mgr.doneChan = make(chan struct{})
	go func() {
		defer func() {
			mgr.startedChanMu.Lock()
			mgr.startedChan = make(chan struct{})
			mgr.startedChanMu.Unlock()

			close(mgr.doneChan)
		}()

		mgr.logger.With().Info("post setup session starting",
			log.String("data_dir", opts.DataDir),
			log.String("num_units", fmt.Sprintf("%d", opts.NumUnits)),
			log.String("labels_per_unit", fmt.Sprintf("%d", mgr.cfg.LabelsPerUnit)),
			log.String("bits_per_label", fmt.Sprintf("%d", mgr.cfg.BitsPerLabel)),
			log.String("provider", fmt.Sprintf("%d", opts.ComputeProviderID)),
		)

		if err := newInit.Initialize(); err != nil {
			if errors.Is(err, initialization.ErrStopped) {
				mgr.logger.Info("post setup session stopped")
				mgr.setState(postSetupStateNotStarted)
			} else {
				mgr.setState(postSetupStateError)
				mgr.setLastErr(err)
			}
			return
		}

		mgr.logger.With().Info("post setup completed",
			log.String("datadir", opts.DataDir),
			log.String("num_units", fmt.Sprintf("%d", opts.NumUnits)),
			log.String("labels_per_unit", fmt.Sprintf("%d", mgr.cfg.LabelsPerUnit)),
			log.String("bits_per_label", fmt.Sprintf("%d", mgr.cfg.BitsPerLabel)),
		)

		mgr.setState(postSetupStateComplete)
		close(mgr.initCompletedChan)
	}()

	return mgr.doneChan, nil
}

// StopSession stops the current Post setup data creation session
// and optionally attempts to delete the data file(s).
func (mgr *PostSetupManager) StopSession(deleteFiles bool) error {
	// mgr.stopMu.Lock()
	// defer mgr.stopMu.Unlock()

	if mgr.getState() == postSetupStateInProgress {
		if err := mgr.getInit().Stop(); err != nil {
			return fmt.Errorf("stop: %w", err)
		}

		// Block until the current data creation session will be finished.
		<-mgr.doneChan
	}

	if deleteFiles {
		if err := mgr.getInit().Reset(); err != nil {
			return fmt.Errorf("reset: %w", err)
		}

		// Reset internal state.
		mgr.setState(postSetupStateNotStarted)
		mgr.initCompletedChan = make(chan struct{})
	}

	return nil
}

// GenerateProof generates a new Post.
func (mgr *PostSetupManager) GenerateProof(challenge []byte) (*types.Post, *types.PostMetadata, error) {
	if mgr.getState() != postSetupStateComplete {
		return nil, nil, errNotComplete
	}

	prover, err := proving.NewProver(config.Config(mgr.cfg), mgr.LastOpts().DataDir, mgr.id)
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
	m.BitsPerLabel = proofMetadata.BitsPerLabel
	m.LabelsPerUnit = proofMetadata.LabelsPerUnit
	m.K1 = proofMetadata.K1
	m.K2 = proofMetadata.K2

	p := (*types.Post)(proof)

	return p, m, nil
}

// LastError returns the Post setup last error.
func (mgr *PostSetupManager) LastError() error {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	return mgr.lastErr
}

// LastOpts returns the Post setup last session options.
func (mgr *PostSetupManager) LastOpts() *PostSetupOpts {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	return mgr.lastOpts
}

// Config returns the Post protocol config.
func (mgr *PostSetupManager) Config() PostConfig {
	return mgr.cfg
}

func (mgr *PostSetupManager) getState() postSetupState {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	return mgr.state
}

func (mgr *PostSetupManager) setState(state postSetupState) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	mgr.state = state
}

func (mgr *PostSetupManager) getInit() *initialization.Initializer {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	return mgr.init
}

func (mgr *PostSetupManager) setInit(newInit *initialization.Initializer) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	mgr.init = newInit
}

func (mgr *PostSetupManager) setLastOpts(opts *PostSetupOpts) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	mgr.lastOpts = opts
}

func (mgr *PostSetupManager) setLastErr(err error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	mgr.lastErr = err
}
