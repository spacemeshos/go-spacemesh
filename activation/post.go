package activation

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/gpu"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/proving"
	"sync"
)

type (
	PoSTSetupComputeProvider initialization.ComputeProvider
)

type PoSTConfig struct {
	BitsPerLabel  uint `mapstructure:"post-bits-per-label"`
	LabelsPerUnit uint `mapstructure:"post-labels-per-unit"`
	MinNumUnits   uint `mapstructure:"post-min-numunits"`
	MaxNumUnits   uint `mapstructure:"post-max-numunits"`
	K1            uint `mapstructure:"post-k1"`
	K2            uint `mapstructure:"post-k2"`
}

// PoSTSetupOpts are the options used to initiate a PoST setup data creation session,
// either via the public smesher API, or on node launch (via cmd args).
type PoSTSetupOpts struct {
	DataDir           string `mapstructure:"smeshing-opts-datadir"`
	NumUnits          uint   `mapstructure:"smeshing-opts-numunits"`
	NumFiles          uint   `mapstructure:"smeshing-opts-numfiles"`
	ComputeProviderID int    `mapstructure:"smeshing-opts-provider"`
	Throttle          bool   `mapstructure:"smeshing-opts-throttle"`
}

// DefaultPoSTConfig defines the default configuration for PoST.
func DefaultPoSTConfig() PoSTConfig {
	return (PoSTConfig)(config.DefaultConfig())
}

// DefaultPoSTSetupOpts defines the default options for PoST setup.
func DefaultPoSTSetupOpts() PoSTSetupOpts {
	return (PoSTSetupOpts)(config.DefaultInitOpts())
}

// PoSTSetupProvider defines the functionality required for PoST setup.
type PoSTSetupProvider interface {
	Status() *PoSTSetupStatus
	StatusChan() <-chan *PoSTSetupStatus
	ComputeProviders() []PoSTSetupComputeProvider
	Benchmark(p PoSTSetupComputeProvider) (int, error)
	StartSession(opts PoSTSetupOpts) (chan struct{}, error)
	StopSession(deleteFiles bool) error
	GenerateProof(challenge []byte) (*types.PoST, *types.PoSTMetadata, error)
	LastError() error
	LastOpts() *PoSTSetupOpts
	Config() PoSTConfig
}

// A compile time check to ensure that PoSTSetupManager fully implements the PoSTSetupProvider interface.
var _ PoSTSetupProvider = (*PoSTSetupManager)(nil)

// PostManager implements PostProvider.
type PoSTSetupManager struct {
	id     []byte
	cfg    PoSTConfig
	logger log.Log

	stopMtx       sync.Mutex
	initStatusMtx sync.Mutex

	state             PoSTSetupState
	initCompletedChan chan struct{}

	// init is the current initializer instance. It is being
	// replaced at the beginning of every data creation session.
	init *initialization.Initializer

	lastOpts *PoSTSetupOpts
	lastErr  error

	// startedChan indicates whether a data creation session has started.
	// The channel instance is replaced in the end of the session.
	startedChan chan struct{}

	// doneChan indicates whether the current data creation session has finished.
	// The channel instance is replaced in the beginning of the session.
	doneChan chan struct{}
}

type PoSTSetupState int32

const (
	PoSTSetupStateNotStarted PoSTSetupState = 1 + iota
	PoSTSetupStateInProgress
	PoSTSetupStateComplete
	PoSTSetupStateError
)

type PoSTSetupStatus struct {
	State            PoSTSetupState
	NumLabelsWritten uint64
	LastOpts         *PoSTSetupOpts
	LastError        error
}

// NewPoSTSetupManager creates a new instance of PoSTSetupManager.
func NewPoSTSetupManager(id []byte, cfg PoSTConfig, logger log.Log) (*PoSTSetupManager, error) {
	mgr := &PoSTSetupManager{
		id:                id,
		cfg:               cfg,
		logger:            logger,
		state:             PoSTSetupStateNotStarted,
		initCompletedChan: make(chan struct{}),
		startedChan:       make(chan struct{}),
	}

	return mgr, nil
}

var errNotComplete = errors.New("not complete")

// Status returns the setup current status.
func (mgr *PoSTSetupManager) Status() *PoSTSetupStatus {
	status := &PoSTSetupStatus{}
	status.State = mgr.state

	if status.State == PoSTSetupStateNotStarted {
		return status
	}

	status.NumLabelsWritten = mgr.init.SessionNumLabelsWritten()
	status.LastOpts = mgr.lastOpts
	status.LastError = mgr.LastError()

	return status
}

// StatusChan returns a channel with status updates of the setup current or the upcoming session.
func (mgr *PoSTSetupManager) StatusChan() <-chan *PoSTSetupStatus {
	// Wait for session to start because only then the initializer instance
	// used for retrieving the progress updates is already set.
	<-mgr.startedChan

	statusChan := make(chan *PoSTSetupStatus, 1024)
	go func() {
		defer close(statusChan)

		initialStatus := mgr.Status()
		statusChan <- initialStatus

		for numLabelsWritten := range mgr.init.SessionNumLabelsWrittenChan() {
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

// ComputeProviders returns a list of available compute providers for PoST setup.
func (mgr *PoSTSetupManager) ComputeProviders() []PoSTSetupComputeProvider {
	providers := initialization.Providers()

	providersAlias := make([]PoSTSetupComputeProvider, len(providers))
	for i, p := range providers {
		providersAlias[i] = PoSTSetupComputeProvider(p)
	}

	return providersAlias
}

// BestProvider returns the most performant compute provider based on a short benchmarking session.
func (mgr *PoSTSetupManager) BestProvider() (*PoSTSetupComputeProvider, error) {
	var bestProvider PoSTSetupComputeProvider
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

func (mgr *PoSTSetupManager) Benchmark(p PoSTSetupComputeProvider) (int, error) {
	return gpu.Benchmark(initialization.ComputeProvider(p))
}

// CreatePostData starts (or continues) a data creation session.
// It supports resuming a previously started session, as well as changing post options (e.g., number of labels)
// after initial setup.
func (mgr *PoSTSetupManager) StartSession(opts PoSTSetupOpts) (chan struct{}, error) {
	mgr.initStatusMtx.Lock()
	if mgr.state == PoSTSetupStateInProgress {
		mgr.initStatusMtx.Unlock()
		return nil, fmt.Errorf("PoST setup session in-progress")
	}
	if mgr.state == PoSTSetupStateComplete {
		// Check whether the new request invalidates the current status.
		var invalidate = opts.DataDir != mgr.lastOpts.DataDir || opts.NumUnits != mgr.lastOpts.NumUnits
		if !invalidate {
			mgr.initStatusMtx.Unlock()

			// Already complete.
			return mgr.doneChan, nil
		}
		mgr.initCompletedChan = make(chan struct{})
	}

	mgr.state = PoSTSetupStateInProgress
	mgr.initStatusMtx.Unlock()

	newInit, err := initialization.NewInitializer(config.Config(mgr.cfg), config.InitOpts(opts), mgr.id)
	if err != nil {
		mgr.state = PoSTSetupStateError
		mgr.lastErr = err
		return nil, err
	}

	if opts.ComputeProviderID == config.BestProviderID {
		p, err := mgr.BestProvider()
		if err != nil {
			return nil, err
		}

		mgr.logger.Info("Found best compute provider: id: %d, model: %v, computeAPI: %v", p.ID, p.Model, p.ComputeAPI)
		opts.ComputeProviderID = int(p.ID)
	}

	newInit.SetLogger(mgr.logger)
	mgr.init = newInit
	mgr.lastOpts = &opts
	mgr.lastErr = nil

	close(mgr.startedChan)
	mgr.doneChan = make(chan struct{})
	go func() {
		defer func() {
			mgr.startedChan = make(chan struct{})
			close(mgr.doneChan)
		}()

		mgr.logger.With().Info("PoST setup session starting...",
			log.String("data_dir", opts.DataDir),
			log.String("num_units", fmt.Sprintf("%d", opts.NumUnits)),
			log.String("labels_per_unit", fmt.Sprintf("%d", mgr.cfg.LabelsPerUnit)),
			log.String("bits_per_label", fmt.Sprintf("%d", mgr.cfg.BitsPerLabel)),
		)

		if err := newInit.Initialize(); err != nil {
			if err == initialization.ErrStopped {
				mgr.logger.Info("PoST setup session stopped")
				mgr.state = PoSTSetupStateNotStarted
			} else {
				mgr.state = PoSTSetupStateError
				mgr.lastErr = err
			}
			return
		}

		mgr.logger.With().Info("PoST setup completed",
			log.String("datadir", opts.DataDir),
			log.String("num_units", fmt.Sprintf("%d", opts.NumUnits)),
			log.String("labels_per_unit", fmt.Sprintf("%d", mgr.cfg.LabelsPerUnit)),
			log.String("bits_per_label", fmt.Sprintf("%d", mgr.cfg.BitsPerLabel)),
		)

		mgr.state = PoSTSetupStateComplete
		close(mgr.initCompletedChan)
	}()

	return mgr.doneChan, nil
}

// StopPostDataCreationSession stops the current post data creation session
// and optionally attempts to delete the post data file(s).
func (mgr *PoSTSetupManager) StopSession(deleteFiles bool) error {
	mgr.stopMtx.Lock()
	defer mgr.stopMtx.Unlock()

	if mgr.state == PoSTSetupStateInProgress {
		if err := mgr.init.Stop(); err != nil {
			return err
		}

		// Block until the current data creation session will be finished.
		<-mgr.doneChan
	}

	if deleteFiles {
		if err := mgr.init.Reset(); err != nil {
			return err
		}

		// Reset internal state.
		mgr.state = PoSTSetupStateNotStarted
		mgr.initCompletedChan = make(chan struct{})
	}

	return nil
}

// GenerateProof generates a new PoST.
func (mgr *PoSTSetupManager) GenerateProof(challenge []byte) (*types.PoST, *types.PoSTMetadata, error) {
	if mgr.state != PoSTSetupStateComplete {
		return nil, nil, errNotComplete
	}

	prover, err := proving.NewProver(config.Config(mgr.cfg), mgr.lastOpts.DataDir, mgr.id)
	if err != nil {
		return nil, nil, err
	}

	prover.SetLogger(mgr.logger)
	proof, proofMetadata, err := prover.GenerateProof(challenge)
	if err != nil {
		return nil, nil, err
	}

	m := new(types.PoSTMetadata)
	m.Challenge = proofMetadata.Challenge
	m.BitsPerLabel = proofMetadata.BitsPerLabel
	m.LabelsPerUnit = proofMetadata.LabelsPerUnit
	m.K1 = proofMetadata.K1
	m.K2 = proofMetadata.K2

	p := (*types.PoST)(proof)

	return p, m, nil
}

func (mgr *PoSTSetupManager) LastError() error {
	return mgr.lastErr
}

func (mgr *PoSTSetupManager) LastOpts() *PoSTSetupOpts {
	return mgr.lastOpts
}

func (mgr *PoSTSetupManager) Config() PoSTConfig {
	return mgr.cfg
}
