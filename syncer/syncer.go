package syncer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/layerfetcher"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./syncer.go

type layerTicker interface {
	GetCurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}

type layerFetcher interface {
	PollLayerContent(ctx context.Context, layerID types.LayerID) chan layerfetcher.LayerPromiseResult
	GetEpochATXs(ctx context.Context, id types.EpochID) error
}

type layerPatrol interface {
	IsHareInCharge(types.LayerID) bool
}

type layerValidator interface {
	ValidateLayer(context.Context, *types.Layer)
}

// Configuration is the config params for syncer.
type Configuration struct {
	SyncInterval time.Duration
	AlwaysListen bool
}

const (
	outOfSyncThreshold  uint32 = 3 // see notSynced
	numGossipSyncLayers uint32 = 2 // see gossipSync

	// the max amount of layer delays syncer can tolerate before it tries to validate a layer.
	maxHareDelayLayers uint32 = 10

	// the max number of attempts syncer tries to advance the processed layer within a sync run.
	maxAttemptWithinRun = 3
)

type syncState uint32

const (
	// notSynced is the state where the node is outOfSyncThreshold layers or more behind the current layer.
	notSynced syncState = iota
	// gossipSync is the state in which a node listens to at least one full layer of gossip before participating
	// in the protocol. this is to protect the node from participating in the consensus without full information.
	// for example, when a node wakes up in the middle of layer N, since it didn't receive all relevant messages and
	// blocks of layer N, it shouldn't vote or produce blocks in layer N+1. it instead listens to gossip for all
	// through layer N+1 and starts producing blocks and participates in hare committee in layer N+2.
	gossipSync
	// synced is the state where the node is in sync with its peers.
	synced
)

func (s syncState) String() string {
	switch s {
	case notSynced:
		return "notSynced"
	case gossipSync:
		return "gossipSync"
	case synced:
		return "synced"
	default:
		return "unknown"
	}
}

// Syncer is responsible to keep the node in sync with the network.
type Syncer struct {
	logger log.Log

	conf      Configuration
	ticker    layerTicker
	mesh      *mesh.Mesh
	validator layerValidator
	fetcher   layerFetcher
	patrol    layerPatrol
	syncOnce  sync.Once
	// access via atomic.[Load|Store]Uint32
	syncState syncState
	// access via atomic.[Load|Store]Uint32
	isBusy    uint32
	syncTimer *time.Ticker
	// targetSyncedLayer is used to signal at which layer we can set this node to synced state
	// TODO(nkryuchkov): Consider getting rid of unsafe.
	targetSyncedLayer unsafe.Pointer

	// awaitSyncedCh is the list of subscribers' channels to notify when this node enters synced state
	awaitSyncedCh []chan struct{}
	awaitSyncedMu sync.Mutex

	forceSyncCh chan struct{}

	shutdownCtx context.Context
	cancelFunc  context.CancelFunc
	eg          errgroup.Group

	// recording the run # since started. for logging/debugging only.
	run uint64
}

// NewSyncer creates a new Syncer instance.
func NewSyncer(ctx context.Context, conf Configuration, ticker layerTicker, mesh *mesh.Mesh, fetcher layerFetcher, patrol layerPatrol, logger log.Log) *Syncer {
	shutdownCtx, cancel := context.WithCancel(ctx)
	return &Syncer{
		logger:            logger,
		conf:              conf,
		ticker:            ticker,
		mesh:              mesh,
		validator:         mesh,
		fetcher:           fetcher,
		patrol:            patrol,
		syncState:         notSynced,
		syncTimer:         time.NewTicker(conf.SyncInterval),
		targetSyncedLayer: unsafe.Pointer(&types.LayerID{}),
		awaitSyncedCh:     make([]chan struct{}, 0),
		forceSyncCh:       make(chan struct{}, 1),
		shutdownCtx:       shutdownCtx,
		cancelFunc:        cancel,
	}
}

// Close stops the syncing process and the goroutines syncer spawns.
func (s *Syncer) Close() {
	s.syncTimer.Stop()
	s.cancelFunc()
	s.logger.With().Info("waiting for syncer goroutines to finish")
	err := s.eg.Wait()
	s.logger.With().Info("all syncer goroutines finished", log.Err(err))
}

// RegisterChForSynced registers ch for notification when the node enters synced state.
func (s *Syncer) RegisterChForSynced(ctx context.Context, ch chan struct{}) {
	if s.IsSynced(ctx) {
		close(ch)
		return
	}
	s.awaitSyncedMu.Lock()
	defer s.awaitSyncedMu.Unlock()
	s.awaitSyncedCh = append(s.awaitSyncedCh, ch)
}

// ListenToGossip returns true if the node is listening to gossip for blocks/TXs/ATXs data.
func (s *Syncer) ListenToGossip() bool {
	return s.conf.AlwaysListen || s.getSyncState() >= gossipSync
}

// IsSynced returns true if the node is in synced state.
func (s *Syncer) IsSynced(ctx context.Context) bool {
	// TODO: at startup, ctx contains no sessionId here
	res := s.getSyncState() == synced
	s.logger.WithContext(ctx).With().Info("node sync state",
		log.Bool("synced", res),
		log.FieldNamed("current", s.ticker.GetCurrentLayer()),
		log.FieldNamed("latest", s.mesh.LatestLayer()),
		log.FieldNamed("processed", s.mesh.ProcessedLayer()))
	return res
}

// Start starts the main sync loop that tries to sync data for every SyncInterval.
func (s *Syncer) Start(ctx context.Context) {
	s.syncOnce.Do(func() {
		s.logger.WithContext(ctx).Info("Starting syncer loop")
		s.eg.Go(func() error {
			if s.ticker.GetCurrentLayer().Uint32() <= 1 {
				s.setSyncState(ctx, synced)
			}
			for {
				select {
				case <-s.shutdownCtx.Done():
					s.logger.WithContext(ctx).Info("stopping sync to shutdown")
					return fmt.Errorf("shutdown context done: %w", s.shutdownCtx.Err())
				case <-s.syncTimer.C:
					s.logger.WithContext(ctx).Debug("synchronize on tick")
					s.synchronize(ctx)
				case <-s.forceSyncCh:
					s.logger.WithContext(ctx).Debug("force synchronize")
					s.synchronize(ctx)
				}
			}
		})
	})
}

// ForceSync manually starts a sync process outside the main sync loop. If the node is already running a sync process,
// ForceSync will be ignored.
func (s *Syncer) ForceSync(ctx context.Context) bool {
	s.logger.WithContext(ctx).Debug("executing ForceSync")
	if s.isClosed() {
		s.logger.WithContext(ctx).Info("shutting down. dropping ForceSync request")
		return false
	}
	if len(s.forceSyncCh) > 0 {
		s.logger.WithContext(ctx).Info("another ForceSync already in progress. dropping this one")
		return false
	}
	s.forceSyncCh <- struct{}{}
	return true
}

func (s *Syncer) isClosed() bool {
	select {
	case <-s.shutdownCtx.Done():
		return true
	default:
		return false
	}
}

func (s *Syncer) getSyncState() syncState {
	return (syncState)(atomic.LoadUint32((*uint32)(&s.syncState)))
}

func (s *Syncer) setSyncState(ctx context.Context, newState syncState) {
	oldState := syncState(atomic.SwapUint32((*uint32)(&s.syncState), uint32(newState)))
	if oldState != newState {
		s.logger.WithContext(ctx).With().Info("sync state change",
			log.String("from_state", oldState.String()),
			log.String("to_state", newState.String()),
			log.FieldNamed("current", s.ticker.GetCurrentLayer()),
			log.FieldNamed("latest", s.mesh.LatestLayer()),
			log.FieldNamed("processed", s.mesh.ProcessedLayer()))
		events.ReportNodeStatusUpdate()
		if newState != synced {
			return
		}
		// notify subscribes
		s.awaitSyncedMu.Lock()
		defer s.awaitSyncedMu.Unlock()
		for _, ch := range s.awaitSyncedCh {
			close(ch)
		}
		s.awaitSyncedCh = make([]chan struct{}, 0)
	}
}

// setSyncerBusy returns false if the syncer is already running a sync process.
// otherwise it sets syncer to be busy and returns true.
func (s *Syncer) setSyncerBusy() bool {
	return atomic.CompareAndSwapUint32(&s.isBusy, 0, 1)
}

func (s *Syncer) setSyncerIdle() {
	atomic.StoreUint32(&s.isBusy, 0)
}

// targetSyncedLayer is used to signal at which layer we can set this node to synced state.
func (s *Syncer) setTargetSyncedLayer(ctx context.Context, layerID types.LayerID) {
	oldSyncLayer := *(*types.LayerID)(atomic.SwapPointer(&s.targetSyncedLayer, unsafe.Pointer(&layerID)))
	s.logger.WithContext(ctx).With().Info("target synced layer changed",
		log.Uint32("from_layer", oldSyncLayer.Uint32()),
		log.Uint32("to_layer", layerID.Uint32()),
		log.FieldNamed("current", s.ticker.GetCurrentLayer()),
		log.FieldNamed("latest", s.mesh.LatestLayer()),
		log.FieldNamed("processed", s.mesh.ProcessedLayer()))
}

func (s *Syncer) getTargetSyncedLayer() types.LayerID {
	return *(*types.LayerID)(atomic.LoadPointer(&s.targetSyncedLayer))
}

// synchronize sync data up to the currentLayer-1 and wait for the layers to be validated.
// it returns false if the data sync failed.
func (s *Syncer) synchronize(ctx context.Context) bool {
	ctx = log.WithNewSessionID(ctx)
	logger := s.logger.WithContext(ctx)

	if s.isClosed() {
		logger.Warning("attempting to sync while shutting down")
		return false
	}

	if s.ticker.GetCurrentLayer().Uint32() == 0 {
		return false
	}

	// at most one synchronize process can run at any time
	if !s.setSyncerBusy() {
		logger.Info("sync is already running, giving up")
		return false
	}

	// no need to worry about race condition for s.run. only one instance of synchronize can run at a time
	s.run++
	logger.With().Info(fmt.Sprintf("starting sync run #%v", s.run),
		log.String("sync_state", s.getSyncState().String()),
		log.FieldNamed("current", s.ticker.GetCurrentLayer()),
		log.FieldNamed("latest", s.mesh.LatestLayer()),
		log.FieldNamed("processed", s.mesh.ProcessedLayer()))

	s.setStateBeforeSync(ctx)
	var (
		vQueue chan *types.Layer
		vDone  chan struct{}
		// number of attempts to start a validation goroutine
		attempt = 0
		// whether the data sync succeed. validation failure is not checked by design.
		success = true
	)

	attemptFunc := func() bool {
		var (
			layer *types.Layer
			err   error
		)
		// using ProcessedLayer() instead of LatestLayer() so we can validate layers on a best-efforts basis
		// and retry in the next sync run if validation fails.
		// our clock starts ticking from 1, so it is safe to skip layer 0
		// always sync to currentLayer-1 to reduce race with gossip and hare/tortoise
		for layerID := s.mesh.ProcessedLayer().Add(1); layerID.Before(s.ticker.GetCurrentLayer()); layerID = layerID.Add(1) {
			if layer, err = s.syncLayer(ctx, layerID); err != nil {
				return false
			}
			vQueue <- layer
			logger.With().Debug("finished data sync", layerID)
		}
		logger.With().Debug("data is synced, waiting for validation",
			log.Int("attempt", attempt),
			log.FieldNamed("current", s.ticker.GetCurrentLayer()),
			log.FieldNamed("latest", s.mesh.LatestLayer()),
			log.FieldNamed("processed", s.mesh.ProcessedLayer()))
		return true
	}

	// check if we are on target. if not, do the sync loop again
	for success && !s.stateOnTarget() {
		attempt++
		if attempt > maxAttemptWithinRun {
			logger.Info("all data synced but unable to advance processed layer after max attempts")
			break
		}
		vQueue, vDone = s.startValidating(ctx, s.run, attempt)
		success = attemptFunc()
		close(vQueue)
		select {
		case <-vDone:
		case <-s.shutdownCtx.Done():
			return false
		}
	}

	s.setStateAfterSync(ctx, success)
	logger.With().Info(fmt.Sprintf("finished sync run #%v", s.run),
		log.Bool("success", success),
		log.Int("attempt", attempt),
		log.String("sync_state", s.getSyncState().String()),
		log.FieldNamed("current", s.ticker.GetCurrentLayer()),
		log.FieldNamed("latest", s.mesh.LatestLayer()),
		log.FieldNamed("processed", s.mesh.ProcessedLayer()))
	s.setSyncerIdle()
	return success
}

func isTooFarBehind(current, latest types.LayerID, logger log.Logger) bool {
	if current.After(latest) && current.Difference(latest) >= outOfSyncThreshold {
		logger.With().Info("node is too far behind",
			log.FieldNamed("current", current),
			log.FieldNamed("latest", latest),
			log.Uint32("behind_threshold", outOfSyncThreshold))
		return true
	}
	return false
}

func (s *Syncer) setStateBeforeSync(ctx context.Context) {
	current := s.ticker.GetCurrentLayer()
	if current.Uint32() <= 1 {
		s.setSyncState(ctx, synced)
		return
	}
	latest := s.mesh.LatestLayer()
	if isTooFarBehind(current, latest, s.logger.WithContext(ctx)) {
		s.setSyncState(ctx, notSynced)
	}
}

func (s *Syncer) stateOnTarget() bool {
	return !s.mesh.ProcessedLayer().Before(s.ticker.GetCurrentLayer().Sub(1))
}

func (s *Syncer) setStateAfterSync(ctx context.Context, success bool) {
	currSyncState := s.getSyncState()
	current := s.ticker.GetCurrentLayer()

	// for the gossipSync/notSynced states, we check if the mesh state is on target before we advance sync state.
	// but for the synced state, we don't check the mesh state because gossip+hare+tortoise are in charge of
	// advancing processed/verified layers.  syncer is just auxiliary that fetches data in case of a temporary
	// network outage.
	switch currSyncState {
	case synced:
		latest := s.mesh.LatestLayer()
		if !success && isTooFarBehind(current, latest, s.logger.WithContext(ctx)) {
			s.setSyncState(ctx, notSynced)
		}
	case gossipSync:
		if !success || !s.stateOnTarget() {
			// push out the target synced layer
			s.setTargetSyncedLayer(ctx, current.Add(numGossipSyncLayers))
			break
		}
		// if we have gossip-synced to the target synced layer, we are ready to participate in consensus
		if !s.getTargetSyncedLayer().After(current) {
			s.setSyncState(ctx, synced)
		}
	case notSynced:
		if success && s.stateOnTarget() {
			// wait till s.ticker.GetCurrentLayer() + numGossipSyncLayers to participate in consensus
			s.setSyncState(ctx, gossipSync)
			s.setTargetSyncedLayer(ctx, current.Add(numGossipSyncLayers))
		}
	}
}

func (s *Syncer) syncLayer(ctx context.Context, layerID types.LayerID) (*types.Layer, error) {
	if s.isClosed() {
		return nil, errors.New("shutdown")
	}

	var layer *types.Layer
	var err error
	if layerID.Before(types.GetEffectiveGenesis()) {
		if err = s.mesh.SetZeroBlockLayer(layerID); err != nil {
			s.logger.WithContext(ctx).With().Panic("failed to set zero-block for genesis layer", layerID, log.Err(err))
		}
	}
	if !layerID.After(types.GetEffectiveGenesis()) {
		if layer, err = s.mesh.GetLayer(layerID); err != nil {
			s.logger.WithContext(ctx).With().Panic("failed to get genesis layer", layerID, log.Err(err))
		}
	} else {
		s.logger.WithContext(ctx).With().Info("polling layer content", layerID)
		if layer, err = s.getLayerFromPeers(ctx, layerID); err != nil {
			return nil, err
		}
	}

	if err = s.getATXs(ctx, layerID); err != nil {
		return nil, err
	}

	return layer, nil
}

func (s *Syncer) getLayerFromPeers(ctx context.Context, layerID types.LayerID) (*types.Layer, error) {
	bch := s.fetcher.PollLayerContent(ctx, layerID)
	res := <-bch
	if res.Err != nil {
		return nil, fmt.Errorf("PollLayerContent: %w", res.Err)
	}

	layer, err := s.mesh.GetLayer(layerID)
	if err != nil {
		return nil, fmt.Errorf("GetLayer: %w", err)
	}

	return layer, nil
}

func (s *Syncer) getATXs(ctx context.Context, layerID types.LayerID) error {
	if layerID.GetEpoch() == 0 {
		s.logger.WithContext(ctx).Info("skip getting atx in epoch 0")
		return nil
	}
	epoch := layerID.GetEpoch()
	atCurrentEpoch := epoch == s.ticker.GetCurrentLayer().GetEpoch()
	atLastLayerOfEpoch := layerID == (epoch + 1).FirstLayer().Sub(1)
	// only get ATXs if
	// - layerID is in the current epoch
	// - layerID is the last layer of a previous epoch
	// i.e. for older epochs we sync ATXs once per epoch. for current epoch we sync ATXs in every layer
	if atCurrentEpoch || atLastLayerOfEpoch {
		s.logger.WithContext(ctx).With().Debug("getting atxs", epoch, layerID)
		ctx = log.WithNewRequestID(ctx, layerID.GetEpoch())
		if err := s.fetcher.GetEpochATXs(ctx, epoch); err != nil {
			// don't fail sync if we cannot fetch atxs for the current epoch before the last layer
			if !atCurrentEpoch || atLastLayerOfEpoch {
				s.logger.WithContext(ctx).With().Error("failed to fetch epoch atxs", layerID, epoch, log.Err(err))
				return fmt.Errorf("get epoch ATXs: %w", err)
			}
			s.logger.WithContext(ctx).With().Warning("failed to fetch epoch atxs", layerID, epoch, log.Err(err))
		}
	}
	return nil
}

// syncer should NOT validate a layer if hare protocol is already running for that layer.
// however, hare can fail for various reasons, one of which is failure to fetch blocks for the hare output.
// maxHareDelayLayers is used to safeguard such scenario and make sure layer data is synced and validated.
func (s *Syncer) shouldValidate(layerID types.LayerID) bool {
	lag := s.mesh.LatestLayer().Sub(layerID.Uint32())
	if s.patrol.IsHareInCharge(layerID) && lag.Value < maxHareDelayLayers {
		s.logger.With().Debug("skip validating layer", layerID)
		return false
	}
	return true
}

// start a dedicated process to validate layers one by one.
// it is caller's responsibility to close the returned "queue" channel to signal end of workload.
// "done" channel will be closed once all workload in "queue" is completed.
func (s *Syncer) startValidating(ctx context.Context, run uint64, attempt int) (chan *types.Layer, chan struct{}) {
	logger := s.logger.WithContext(ctx).WithName("validation")

	// start a dedicated process for validation.
	// do not use an unbuffered channel for queue. we don't want it to block if the receiver isn't ready.
	// i.e. if validation for the last layer is still running
	queue := make(chan *types.Layer, s.ticker.GetCurrentLayer().Uint32())
	done := make(chan struct{})

	s.eg.Go(func() error {
		logger.Debug("validation started for run #%v attempt #%v", run, attempt)
		for layer := range queue {
			if s.isClosed() {
				return nil
			}
			if s.shouldValidate(layer.Index()) {
				s.validateLayer(ctx, layer)
			}
		}
		close(done)
		logger.Debug("validation done for run #%v attempt #%v", run, attempt)
		return nil
	})
	return queue, done
}

func (s *Syncer) validateLayer(ctx context.Context, layer *types.Layer) {
	if s.isClosed() {
		s.logger.WithContext(ctx).Error("shutting down")
		return
	}

	s.logger.WithContext(ctx).With().Debug("validating layer",
		layer.Index(),
		log.String("blocks", fmt.Sprint(types.BlockIDs(layer.Blocks()))))

	// TODO: re-architect this so the syncer does not need to actually wait for tortoise to finish running.
	//   It should be sufficient to call GetLayer (above), and maybe, to queue a request to tortoise to analyze this
	//   layer (without waiting for this to finish -- it should be able to run async).
	//   See https://github.com/spacemeshos/go-spacemesh/issues/2415
	s.validator.ValidateLayer(ctx, layer)
}
