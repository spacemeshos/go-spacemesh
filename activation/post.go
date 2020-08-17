package activation

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/post/validation"
	"sync"
)

// DefaultConfig defines the default configuration options for PoST
func DefaultConfig() config.Config {
	return *config.DefaultConfig()
}

type PostProvider interface {
	PostStatus() (*PostStatus, error)
	PostComputeProviders() []initialization.ComputeProvider
	CreatePostData(options *PostOptions) (chan struct{}, error)
	StopPostDataCreationSession(deleteFiles bool) error
	PostDataCreationProgressStream() <-chan *PostStatus
	InitCompleted() (chan struct{}, bool)
	GenerateProof(challenge []byte) (*types.PostProof, error)
	Cfg() config.Config
	SetLogger(logger log.Log)
}

// A compile time check to ensure that PostManager fully implements the PostAPI interface.
var _ PostProvider = (*PostManager)(nil)

type PostManager struct {
	// id is the Miner ID which the
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

type PostOptions struct {
	DataDir           string
	DataSize          uint64
	Append            bool
	Throttle          bool
	ComputeProviderId uint
}

type FilesStatus int

const (
	FilesStatusNotFound  FilesStatus = 1
	FilesStatusPartial   FilesStatus = 2
	FilesStatusCompleted FilesStatus = 3
)

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
	}

	mgr.init, err = initialization.NewInitializer(&mgr.cfg, mgr.id)
	if err != nil {
		return nil, err
	}
	diskState, err := mgr.init.DiskState()
	if err != nil {
		return nil, err
	}

	switch diskState.InitState {
	//	case initialization.InitStateNotStarted:
	case initialization.InitStateCompleted:
		mgr.initStatus = statusCompleted
		close(mgr.initCompletedChan)
		//	case initialization.InitStateStopped:
		//	case initialization.InitStateCrashed:

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
	return mgr.init.Providers()
}

func (mgr *PostManager) CreatePostData(options *PostOptions) (chan struct{}, error) {
	mgr.initStatusMtx.Lock()
	if mgr.initStatus == statusInProgress {
		mgr.initStatusMtx.Unlock()
		return nil, fmt.Errorf("data creation session in-progress")
	}
	if mgr.initStatus == statusCompleted {
		// Check whether the new request invalidates the current status.
		var invalidate = options.DataDir != mgr.cfg.DataDir || options.DataSize != uint64(mgr.cfg.NumFiles)
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
	newCfg.NumLabels = options.DataSize // TODO(moshababo): fix
	newInit, err := initialization.NewInitializer(&newCfg, mgr.id)
	if err != nil {
		mgr.initStatus = statusIdle
		return nil, err
	}
	if err := newInit.VerifyInitAllowed(); err != nil {
		mgr.initStatus = statusIdle
		return nil, err
	}
	newInit.SetLogger(mgr.logger)

	if err := mgr.storePostOptions(options); err != nil {
		return nil, err
	}

	mgr.cfg = newCfg
	mgr.init = newInit
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

		if err := newInit.Initialize(options.ComputeProviderId); err != nil {
			if err == initialization.ErrStopped {
				mgr.logger.Info("PoST initialization stopped")
			} else {
				mgr.logger.Error("PoST initialization failed: %v", err) // TODO: should not crash nor print stacktrace.
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
	// Wait for init to start because at that point it already replaced
	// the initializer instance used for retrieving the progress updates.
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

func (mgr *PostManager) GenerateProof(challenge []byte) (*types.PostProof, error) {
	p, err := proving.NewProver(&mgr.cfg, mgr.id)
	if err != nil {
		return nil, err
	}

	p.SetLogger(mgr.logger)
	proof, err := p.GenerateProof(challenge)
	return (*types.PostProof)(proof), err
}

func (mgr *PostManager) Cfg() config.Config {
	return mgr.cfg
}

func (mgr *PostManager) SetLogger(logger log.Log) {
	mgr.logger = logger
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

func verifyPoST(proof *types.PostProof, id []byte, numLabels uint64, labelSize, k1, k2 uint) error {
	cfg := config.Config{
		NumLabels: numLabels,
		LabelSize: labelSize,
		K1:        k1,
		K2:        k2,
		NumFiles:  1,
	}

	v, err := validation.NewValidator(&cfg)
	if err != nil {
		return err
	}
	if err := v.Validate(id, (*proving.Proof)(proof)); err != nil {
		return err
	}

	return nil
}
