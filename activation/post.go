package activation

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/post/shared"
	"sync"
)

// DefaultConfig defines the default configuration options for PoST
func DefaultConfig() config.Config {
	return *config.DefaultConfig()
}

// BestProviderID can be used for selecting the most performant provider
// based on a short benchmarking session.
const BestProviderID = -1

func DefaultPostOptions() PostOptions {
	cfg := DefaultConfig()
	return PostOptions{
		DataDir:           cfg.DataDir,
		DataSize:          shared.DataSize(cfg.NumLabels, cfg.LabelSize),
		Append:            false,
		Throttle:          false,
		ComputeProviderID: BestProviderID,
	}
}

// PostOptions are the options used to initiate a post data creation session,
// either via the public smesher API, or on node launch (via cmd args).
type PostOptions struct {
	DataDir           string `mapstructure:"post-options-datadir"`
	DataSize          uint64 `mapstructure:"post-options-datasize"`
	Append            bool   `mapstructure:"post-options-append"`
	Throttle          bool   `mapstructure:"post-options-throttle"`
	ComputeProviderID int    `mapstructure:"post-options-provider-id"`
}

type PostProvider interface {
	PostStatus() (*PostStatus, error)
	PostComputeProviders() []initialization.ComputeProvider
	CreatePostData(options *PostOptions) (chan struct{}, error)
	StopPostDataCreationSession(deleteFiles bool) error
	PostDataCreationProgressStream() <-chan *PostStatus
	InitCompleted() (chan struct{}, bool)
	GenerateProof(challenge []byte) (*types.PoST, *types.PoSTMetadata, error)
}

// A compile time check to ensure that PostManager fully implements the PostProvider interface.
var _ PostProvider = (*PostManager)(nil)

type PostManager struct {
	id []byte

	cfg    config.Config
	store  bytesStore
	logger log.Log

	stopMtx       sync.Mutex
	initStatusMtx sync.Mutex

	initStatus        initStatus
	initCompletedChan chan struct{}

	// init is the current initializer instance. It is being
	// replaced at the beginning of every data creation session.
	init *initialization.Initializer

	// startedChan indicates whether a data creation session has started.
	// The channel instance is replaced in the end of the session.
	startedChan chan struct{}

	// doneChan indicates whether the current data creation session has finished.
	// The channel instance is replaced in the beginning of the session.
	doneChan chan struct{}
}

type initStatus int32

const (
	statusIdle initStatus = iota
	statusInProgress
	statusCompleted
)

var postOptionsStoreKey = []byte("postOptions")

var emptyStatus = &PostStatus{
	FilesStatus: FilesStatusNotFound,
}

type FilesStatus int

const (
	FilesStatusNotFound  FilesStatus = 1
	FilesStatusPartial   FilesStatus = 2
	FilesStatusCompleted FilesStatus = 3
)

// TODO(moshababo): apply custom error type inspection
type ErrorType int

const (
	ErrorTypeFilesNotFound   ErrorType = 1
	ErrorTypeFilesReadError  ErrorType = 2
	ErrorTypeFilesWriteError ErrorType = 3
)

type PostStatus struct {
	LastOptions    *PostOptions
	FilesStatus    FilesStatus
	InitInProgress bool
	BytesWritten   uint64
	ErrorType      ErrorType
	ErrorMessage   string
}

func NewPostManager(id []byte, cfg config.Config, store bytesStore, logger log.Log) (*PostManager, error) {
	mgr := &PostManager{
		id:                id,
		cfg:               cfg,
		store:             store,
		logger:            logger,
		initStatus:        statusIdle,
		initCompletedChan: make(chan struct{}),
		startedChan:       make(chan struct{}),
	}

	// Retrieve the last used options to override the configured datadir.
	options, err := mgr.loadPostOptions()
	if err != nil {
		return nil, err
	}
	if options != nil {
		mgr.cfg.DataDir = options.DataDir
		mgr.cfg.NumLabels = shared.NumLabels(options.DataSize, mgr.cfg.LabelSize)
	}

	mgr.init, err = initialization.NewInitializer(&mgr.cfg, mgr.id)
	if err != nil {
		return nil, err
	}
	diskState, err := mgr.init.DiskState()
	if err != nil {
		return nil, err
	}

	if diskState.InitState == initialization.InitStateCompleted {
		mgr.initStatus = statusCompleted
		close(mgr.initCompletedChan)
	}

	return mgr, nil
}

func (mgr *PostManager) PostStatus() (*PostStatus, error) {
	options, err := mgr.loadPostOptions()
	if err != nil {
		return nil, err
	}

	if options == nil {
		return emptyStatus, nil
	}

	diskState, err := mgr.init.DiskState()
	if err != nil {
		return nil, err
	}
	status := &PostStatus{}
	status.LastOptions = options
	status.BytesWritten = diskState.BytesWritten

	if mgr.initStatus == statusInProgress {
		status.FilesStatus = FilesStatusPartial
		status.InitInProgress = true
		return status, nil
	}

	switch diskState.InitState {
	case initialization.InitStateNotStarted:
		status.FilesStatus = FilesStatusNotFound
	case initialization.InitStateCompleted:
		status.FilesStatus = FilesStatusCompleted
	case initialization.InitStateStopped:
		status.FilesStatus = FilesStatusPartial
	case initialization.InitStateCrashed:
		status.FilesStatus = FilesStatusPartial
		status.ErrorMessage = "crashed"
	}
	return status, nil
}

func (mgr *PostManager) PostComputeProviders() []initialization.ComputeProvider {
	return initialization.Providers()
}

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

func (mgr *PostManager) CreatePostData(options *PostOptions) (chan struct{}, error) {
	mgr.initStatusMtx.Lock()
	if mgr.initStatus == statusInProgress {
		mgr.initStatusMtx.Unlock()
		return nil, fmt.Errorf("data creation session in-progress")
	}
	if mgr.initStatus == statusCompleted {
		// Check whether the new request invalidates the current status.
		var invalidate = options.DataDir != mgr.cfg.DataDir || shared.NumLabels(options.DataSize, mgr.cfg.LabelSize) != uint64(mgr.cfg.NumLabels)
		if !invalidate {
			mgr.initStatusMtx.Unlock()
			return nil, fmt.Errorf("already completed")
		}
		mgr.initCompletedChan = make(chan struct{})
	}
	mgr.initStatus = statusInProgress
	mgr.initStatusMtx.Unlock()

	newCfg := mgr.cfg
	newCfg.DataDir = options.DataDir
	newCfg.NumLabels = shared.NumLabels(options.DataSize, mgr.cfg.LabelSize)
	newInit, err := initialization.NewInitializer(&newCfg, mgr.id)
	if err != nil {
		mgr.initStatus = statusIdle
		return nil, err
	}
	if err := newInit.VerifyInitAllowed(); err != nil {
		mgr.initStatus = statusIdle
		return nil, err
	}

	if options.ComputeProviderID == BestProviderID {
		p, err := mgr.BestProvider()
		if err != nil {
			return nil, err
		}

		mgr.logger.Info("Best compute provider found: id: %d, model: %v, computeAPI: %v", p.ID, p.Model, p.ComputeAPI)
		options.ComputeProviderID = int(p.ID)
	}

	if err := mgr.storePostOptions(options); err != nil {
		return nil, err
	}

	newInit.SetLogger(mgr.logger)
	mgr.init = newInit
	mgr.cfg = newCfg
	close(mgr.startedChan)
	mgr.doneChan = make(chan struct{})

	go func() {
		defer func() {
			mgr.startedChan = make(chan struct{})
			close(mgr.doneChan)
		}()

		mgr.logger.With().Info("PoST initialization starting...",
			log.String("datadir", options.DataDir),
			log.String("numLabels", fmt.Sprintf("%d", options.DataSize)),
		)

		if err := newInit.Initialize(uint(options.ComputeProviderID)); err != nil {
			if err == initialization.ErrStopped {
				mgr.logger.Info("PoST initialization stopped")
			} else {
				mgr.logger.Error("PoST initialization failed: %v", err)
			}
			mgr.initStatus = statusIdle
			return
		}

		mgr.logger.With().Info("PoST initialization completed",
			log.String("datadir", options.DataDir),
			log.String("numLabels", fmt.Sprintf("%d", options.DataSize)),
		)

		mgr.initStatus = statusCompleted
		close(mgr.initCompletedChan)
	}()

	return mgr.doneChan, nil
}

func (mgr *PostManager) PostDataCreationProgressStream() <-chan *PostStatus {
	// Wait for init to start because only then the initializer instance
	// used for retrieving the progress updates is already set.
	<-mgr.startedChan

	statusChan := make(chan *PostStatus, 1024)
	go func() {
		defer close(statusChan)
		var firstStatus *PostStatus
		for p := range mgr.init.Progress() {
			// Retrieve the first status after init started.
			if firstStatus == nil {
				var err error
				firstStatus, err = mgr.PostStatus()
				if err != nil {
					return
				}
			}

			// Clone the first status and update relevant fields by using the channel updates.
			status := *firstStatus
			status.BytesWritten = uint64(p * float64(status.LastOptions.DataSize))
			if int(p) == 1 {
				status.FilesStatus = FilesStatusCompleted
				status.InitInProgress = false
			}
			statusChan <- &status
		}
	}()

	return statusChan
}

func (mgr *PostManager) StopPostDataCreationSession(deleteFiles bool) error {
	mgr.stopMtx.Lock()
	defer mgr.stopMtx.Unlock()

	if mgr.initStatus == statusInProgress {
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

		mgr.initStatus = statusIdle
		mgr.initCompletedChan = make(chan struct{})
	}

	return nil
}

func (mgr *PostManager) InitCompleted() (chan struct{}, bool) {
	return mgr.initCompletedChan, mgr.initStatus == statusCompleted
}

func (mgr *PostManager) GenerateProof(challenge []byte) (*types.PoST, *types.PoSTMetadata, error) {
	p, err := proving.NewProver(&mgr.cfg, mgr.id)
	if err != nil {
		return nil, nil, err
	}

	p.SetLogger(mgr.logger)
	proof, proofMetadata, err := p.GenerateProof(challenge)
	return (*types.PoST)(proof), (*types.PoSTMetadata)(proofMetadata), err
}

func (mgr *PostManager) storePostOptions(options *PostOptions) error {
	b, err := types.InterfaceToBytes(options)
	if err != nil {
		return err
	}

	return mgr.store.Put(postOptionsStoreKey, b)
}

func (mgr *PostManager) loadPostOptions() (*PostOptions, error) {
	b, err := mgr.store.Get(postOptionsStoreKey)
	if err != nil || len(b) == 0 {
		return nil, nil
	}

	val := &PostOptions{}
	err = types.BytesToInterface(b, val)
	if err != nil {
		return nil, err
	}

	return val, nil
}
