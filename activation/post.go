package activation

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/proving"
	"sync"
)

// DefaultConfig defines the default configuration for PoST.
func DefaultConfig() config.Config {
	return *config.DefaultConfig()
}

// BestProviderID can be used for selecting the most performant provider
// based on a short benchmarking session.
const BestProviderID = -1

// DefaultPostInitOps defines the default options for post init.
func DefaultPostInitOps() PostInitOpts {
	cfg := DefaultConfig()
	return PostInitOpts{
		DataDir:           cfg.DataDir,
		NumUnits:          cfg.MinNumUnits + 1,
		NumFiles:          cfg.NumFiles,
		ComputeProviderID: BestProviderID,
		Throttle:          false,
	}
}

// PostInitOpts are the options used to initiate a post data creation session,
// either via the public smesher API, or on node launch (via cmd args).
type PostInitOpts struct {
	DataDir           string `mapstructure:"post-init-datadir"`
	NumUnits          uint   `mapstructure:"post-init-numunits"`
	NumFiles          uint   `mapstructure:"post-init-numfiles"`
	ComputeProviderID int    `mapstructure:"post-init-provider"`
	Throttle          bool   `mapstructure:"post-init-throttle"`
}

// PostProvider defines the functionality required for the node's Smesher API.
type PostProvider interface {
	PostStatus() *PostStatus
	PostComputeProviders() []initialization.ComputeProvider
	CreatePostData(opts *PostInitOpts) (chan struct{}, error)
	StopPostDataCreationSession(deleteFiles bool) error
	PostDataCreationProgressStream() <-chan *PostStatus
	InitStatus() InitStatus
	InitStatusComplete() bool
	GenerateProof(challenge []byte) (*types.PoST, *types.PoSTMetadata, error)
	LastError() error
	LastOpts() *PostInitOpts
	Config() config.Config
}

// A compile time check to ensure that PostManager fully implements the PostProvider interface.
var _ PostProvider = (*PostManager)(nil)

// PostManager implements PostProvider.
type PostManager struct {
	id []byte

	cfg    config.Config
	logger log.Log

	stopMtx       sync.Mutex
	initStatusMtx sync.Mutex

	initStatus        InitStatus
	initCompletedChan chan struct{}

	// init is the current initializer instance. It is being
	// replaced at the beginning of every data creation session.
	init *initialization.Initializer

	lastOpts *PostInitOpts
	lastErr  error

	// startedChan indicates whether a data creation session has started.
	// The channel instance is replaced in the end of the session.
	startedChan chan struct{}

	// doneChan indicates whether the current data creation session has finished.
	// The channel instance is replaced in the beginning of the session.
	doneChan chan struct{}
}

type InitStatus int32

const (
	InitStatusNotStarted InitStatus = 1 + iota
	InitStatusInProgress
	InitStatusComplete
	InitStatusError
)

type PostStatus struct {
	InitStatus       InitStatus
	InitOpts         *PostInitOpts
	NumLabelsWritten uint64
	Error            error
}

// NewPostManager creates a new instance of PostManager.
func NewPostManager(id []byte, cfg config.Config, logger log.Log) (*PostManager, error) {
	mgr := &PostManager{
		id:                id,
		cfg:               cfg, // LabelBatchSize, LabelSize, K1 & K2 will be used, others are to be overridden when calling to CreateDataSession.
		logger:            logger,
		initStatus:        InitStatusNotStarted,
		initCompletedChan: make(chan struct{}),
		startedChan:       make(chan struct{}),
	}

	//var err error
	//mgr.init, err = initialization.NewInitializer(&mgr.cfg, mgr.id)
	//if err != nil {
	//	return nil, err
	//}
	//diskState, err := mgr.init.DiskState()
	//if err != nil {
	//	return nil, err
	//}
	//
	//if diskState.InitState == initialization.InitStateCompleted {
	//	mgr.InitStatus = InitStatusComplete
	//	close(mgr.initCompletedChan)
	//}

	return mgr, nil
}

var errNotComplete = errors.New("not complete")

// PostStatus returns the node's post data status.
func (mgr *PostManager) PostStatus() *PostStatus {
	status := &PostStatus{}
	status.InitStatus = mgr.initStatus

	if status.InitStatus == InitStatusNotStarted {
		return status
	}

	status.InitOpts = mgr.lastOpts
	status.NumLabelsWritten = mgr.init.SessionNumLabelsWritten()
	status.Error = mgr.LastError()

	return status
}

// PostComputeProviders returns a list of available compute providers for creating the post data.
func (mgr *PostManager) PostComputeProviders() []initialization.ComputeProvider {
	return initialization.Providers()
}

// BestProvider returns the most performant provider based on a short benchmarking session.
func (mgr *PostManager) BestProvider() (*initialization.ComputeProvider, error) {
	var bestProvider initialization.ComputeProvider
	var maxHS int
	for _, p := range mgr.PostComputeProviders() {
		hs, err := p.Benchmark()
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

// CreatePostData starts (or continues) a data creation session.
// It supports resuming a previously started session, as well as changing post options (e.g., number of labels)
// after initial setup.
func (mgr *PostManager) CreatePostData(opts *PostInitOpts) (chan struct{}, error) {
	mgr.initStatusMtx.Lock()
	if mgr.initStatus == InitStatusInProgress {
		mgr.initStatusMtx.Unlock()
		return nil, fmt.Errorf("data creation session in-progress")
	}
	if mgr.initStatus == InitStatusComplete {
		// Check whether the new request invalidates the current status.
		var invalidate = opts.DataDir != mgr.lastOpts.DataDir || opts.NumUnits != mgr.lastOpts.NumUnits
		if !invalidate {
			mgr.initStatusMtx.Unlock()

			// Already complete.
			return mgr.doneChan, nil
		}
		mgr.initCompletedChan = make(chan struct{})
	}

	mgr.initStatus = InitStatusInProgress
	mgr.initStatusMtx.Unlock()

	// Overriding the existing cfg with the new opts.
	newCfg := mgr.cfg
	newCfg.DataDir = opts.DataDir
	newCfg.NumFiles = opts.NumFiles

	newInit, err := initialization.NewInitializer(&newCfg, mgr.id)
	if err != nil {
		mgr.initStatus = InitStatusError
		mgr.lastErr = err
		return nil, err
	}

	//if err := newInit.VerifyNotCompleted(); err != nil {
	//	mgr.InitStatus = InitStatusIdle
	//	return nil, err
	//}

	if opts.ComputeProviderID == BestProviderID {
		p, err := mgr.BestProvider()
		if err != nil {
			return nil, err
		}

		mgr.logger.Info("Found best compute provider: id: %d, model: %v, computeAPI: %v", p.ID, p.Model, p.ComputeAPI)
		opts.ComputeProviderID = int(p.ID)
	}

	newInit.SetLogger(mgr.logger)
	mgr.init = newInit
	mgr.cfg = newCfg
	mgr.lastOpts = opts
	mgr.lastErr = nil

	close(mgr.startedChan)
	mgr.doneChan = make(chan struct{})
	go func() {
		defer func() {
			mgr.startedChan = make(chan struct{})
			close(mgr.doneChan)
		}()

		mgr.logger.With().Info("PoST initialization starting...",
			log.String("data_dir", opts.DataDir),
			log.String("num_units", fmt.Sprintf("%d", opts.NumUnits)),
			log.String("labels_per_unit", fmt.Sprintf("%d", mgr.cfg.LabelsPerUnit)),
			log.String("bits_per_label", fmt.Sprintf("%d", mgr.cfg.BitsPerLabel)),
		)

		if err := newInit.Initialize(uint(opts.ComputeProviderID), opts.NumUnits); err != nil {
			if err == initialization.ErrStopped {
				mgr.logger.Info("PoST initialization stopped")
				mgr.initStatus = InitStatusNotStarted
			} else {
				mgr.initStatus = InitStatusError
				mgr.lastErr = err
			}
			return
		}

		mgr.logger.With().Info("PoST initialization completed",
			log.String("datadir", opts.DataDir),
			log.String("num_units", fmt.Sprintf("%d", opts.NumUnits)),
			log.String("labels_per_unit", fmt.Sprintf("%d", mgr.cfg.LabelsPerUnit)),
			log.String("bits_per_label", fmt.Sprintf("%d", mgr.cfg.BitsPerLabel)),
		)

		mgr.initStatus = InitStatusComplete
		close(mgr.initCompletedChan)
	}()

	return mgr.doneChan, nil
}

// PostDataCreationProgressStream returns a stream of updates regarding
// the current or the upcoming post data creation session.
func (mgr *PostManager) PostDataCreationProgressStream() <-chan *PostStatus {
	// Wait for session to start because only then the initializer instance
	// used for retrieving the progress updates is already set.
	<-mgr.startedChan

	statusChan := make(chan *PostStatus, 1024)
	go func() {
		defer close(statusChan)

		initialStatus := mgr.PostStatus()
		statusChan <- initialStatus

		for numLabelsWritten := range mgr.init.SessionNumLabelsWrittenChan() {
			status := *initialStatus
			status.NumLabelsWritten = numLabelsWritten
			statusChan <- &status
		}

		if finalStatus := mgr.PostStatus(); finalStatus.Error != nil {
			statusChan <- finalStatus
		}
	}()

	return statusChan
}

// StopPostDataCreationSession stops the current post data creation session
// and optionally attempts to delete the post data file(s).
func (mgr *PostManager) StopPostDataCreationSession(deleteFiles bool) error {
	mgr.stopMtx.Lock()
	defer mgr.stopMtx.Unlock()

	if mgr.initStatus == InitStatusInProgress {
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
		mgr.initStatus = InitStatusNotStarted
		mgr.initCompletedChan = make(chan struct{})
	}

	return nil
}

// InitCompleted indicates whether the post init phase has been complete.
func (mgr *PostManager) InitStatusComplete() bool {
	return mgr.initStatus == InitStatusComplete
}

func (mgr *PostManager) InitStatus() InitStatus {
	return mgr.initStatus
}

// GenerateProof generates a new PoST.
func (mgr *PostManager) GenerateProof(challenge []byte) (*types.PoST, *types.PoSTMetadata, error) {
	if mgr.initStatus != InitStatusComplete {
		return nil, nil, errNotComplete
	}

	prover, err := proving.NewProver(&mgr.cfg, mgr.id)
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

func (mgr *PostManager) LastError() error {
	return mgr.lastErr
}

func (mgr *PostManager) LastOpts() *PostInitOpts {
	return mgr.lastOpts
}

func (mgr *PostManager) Config() config.Config {
	return mgr.cfg
}
