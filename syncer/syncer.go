package syncer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/system"
)

// Configuration is the config params for syncer.
type Configuration struct {
	SyncInterval time.Duration
}

const (
	outOfSyncThreshold  uint32 = 3 // see notSynced
	numGossipSyncLayers uint32 = 2 // see gossipSync

	// the max amount of layer delays syncer can tolerate before it tries to validate a layer.
	maxHareDelayLayers uint32 = 10
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

var (
	errShuttingDown      = errors.New("shutting down")
	errBeaconUnavailable = errors.New("beacon unavailable")
	errHareInCharge      = errors.New("hare in charge of layer")
	errATXsNotSynced     = errors.New("ATX not synced")
)

// Syncer is responsible to keep the node in sync with the network.
type Syncer struct {
	logger log.Log

	conf          Configuration
	ticker        layerTicker
	beacons       system.BeaconGetter
	mesh          *mesh.Mesh
	lyrProcessor  layerProcessor
	fetcher       layerFetcher
	patrol        layerPatrol
	syncOnce      sync.Once
	syncState     atomic.Value
	atxSyncState  atomic.Value
	isBusy        atomic.Value
	syncTimer     *time.Ticker
	validateTimer *time.Ticker
	// targetSyncedLayer is used to signal at which layer we can set this node to synced state
	targetSyncedLayer atomic.Value
	lastLayerSynced   atomic.Value
	lastATXsSynced    atomic.Value

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
func NewSyncer(ctx context.Context, conf Configuration, ticker layerTicker, beacons system.BeaconGetter, mesh *mesh.Mesh, fetcher layerFetcher, patrol layerPatrol, logger log.Log) *Syncer {
	shutdownCtx, cancel := context.WithCancel(ctx)
	s := &Syncer{
		logger:        logger,
		conf:          conf,
		ticker:        ticker,
		beacons:       beacons,
		mesh:          mesh,
		lyrProcessor:  mesh,
		fetcher:       fetcher,
		patrol:        patrol,
		syncTimer:     time.NewTicker(conf.SyncInterval),
		validateTimer: time.NewTicker(conf.SyncInterval),
		awaitSyncedCh: make([]chan struct{}, 0),
		forceSyncCh:   make(chan struct{}, 1),
		shutdownCtx:   shutdownCtx,
		cancelFunc:    cancel,
	}
	s.syncState.Store(notSynced)
	s.atxSyncState.Store(notSynced)
	s.isBusy.Store(0)
	s.targetSyncedLayer.Store(types.LayerID{})
	s.lastLayerSynced.Store(s.mesh.ProcessedLayer())
	s.lastATXsSynced.Store(types.EpochID(0))
	return s
}

// Close stops the syncing process and the goroutines syncer spawns.
func (s *Syncer) Close() {
	s.syncTimer.Stop()
	s.validateTimer.Stop()
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

// ListenToGossip returns true if the node is listening to gossip for blocks/TXs data.
func (s *Syncer) ListenToGossip() bool {
	return s.getSyncState() >= gossipSync
}

// ListenToATXGossip returns true if the node is listening to gossip for ATXs data.
func (s *Syncer) ListenToATXGossip() bool {
	return s.getATXSyncState() == synced
}

// IsSynced returns true if the node is in synced state.
func (s *Syncer) IsSynced(ctx context.Context) bool {
	// TODO: at startup, ctx contains no sessionId here
	res := s.getSyncState() == synced
	s.logger.WithContext(ctx).With().Info("node sync state",
		log.Bool("synced", res),
		log.Stringer("current", s.ticker.GetCurrentLayer()),
		log.Stringer("latest", s.mesh.LatestLayer()),
		log.Stringer("processed", s.mesh.ProcessedLayer()))
	return res
}

// Start starts the main sync loop that tries to sync data for every SyncInterval.
func (s *Syncer) Start(ctx context.Context) {
	s.syncOnce.Do(func() {
		s.logger.WithContext(ctx).Info("starting syncer loop")
		s.eg.Go(func() error {
			if s.ticker.GetCurrentLayer().Uint32() <= 1 {
				s.setATXSynced()
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
		s.logger.WithContext(ctx).Info("starting syncer layer processing loop")
		s.eg.Go(func() error {
			for {
				select {
				case <-s.shutdownCtx.Done():
					return nil
				case <-s.validateTimer.C:
					s.processLayers(ctx)
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

func (s *Syncer) setATXSynced() {
	s.atxSyncState.Store(synced)
}

func (s *Syncer) getATXSyncState() syncState {
	return s.atxSyncState.Load().(syncState)
}

func (s *Syncer) getSyncState() syncState {
	return s.syncState.Load().(syncState)
}

func (s *Syncer) setSyncState(ctx context.Context, newState syncState) {
	oldState := s.syncState.Swap(newState).(syncState)
	if oldState != newState {
		s.logger.WithContext(ctx).With().Info("sync state change",
			log.String("from_state", oldState.String()),
			log.String("to_state", newState.String()),
			log.Stringer("current", s.ticker.GetCurrentLayer()),
			log.Stringer("latest", s.mesh.LatestLayer()),
			log.Stringer("processed", s.mesh.ProcessedLayer()))
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
	return s.isBusy.CompareAndSwap(0, 1)
}

func (s *Syncer) setSyncerIdle() {
	s.isBusy.Store(0)
}

// targetSyncedLayer is used to signal at which layer we can set this node to synced state.
func (s *Syncer) setTargetSyncedLayer(ctx context.Context, layerID types.LayerID) {
	oldSyncLayer := s.targetSyncedLayer.Swap(layerID).(types.LayerID)
	s.logger.WithContext(ctx).With().Info("target synced layer changed",
		log.Uint32("from_layer", oldSyncLayer.Uint32()),
		log.Uint32("to_layer", layerID.Uint32()),
		log.Stringer("current", s.ticker.GetCurrentLayer()),
		log.Stringer("latest", s.mesh.LatestLayer()),
		log.Stringer("processed", s.mesh.ProcessedLayer()))
}

func (s *Syncer) getTargetSyncedLayer() types.LayerID {
	return s.targetSyncedLayer.Load().(types.LayerID)
}

func (s *Syncer) setLastSyncedLayer(lid types.LayerID) {
	s.lastLayerSynced.Store(lid)
}

func (s *Syncer) getLastSyncedLayer() types.LayerID {
	return s.lastLayerSynced.Load().(types.LayerID)
}

func (s *Syncer) setLastSyncedATXs(epoch types.EpochID) {
	s.lastATXsSynced.Store(epoch)
}

func (s *Syncer) getLastSyncedATXs() types.EpochID {
	return s.lastATXsSynced.Load().(types.EpochID)
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
	defer s.setSyncerIdle()

	// no need to worry about race condition for s.run. only one instance of synchronize can run at a time
	s.run++
	logger.With().Info(fmt.Sprintf("starting sync run #%v", s.run),
		log.Stringer("sync_state", s.getSyncState()),
		log.Stringer("last_synced", s.getLastSyncedLayer()),
		log.Stringer("current", s.ticker.GetCurrentLayer()),
		log.Stringer("latest", s.mesh.LatestLayer()),
		log.Stringer("in_state", s.mesh.LatestLayerInState()),
		log.Stringer("processed", s.mesh.ProcessedLayer()))

	s.setStateBeforeSync(ctx)
	syncFunc := func() bool {
		if !s.ListenToATXGossip() {
			logger.Info("syncing atx before everything else")
			for epoch := s.getLastSyncedATXs() + 1; epoch <= s.ticker.GetCurrentLayer().GetEpoch(); epoch++ {
				logger.With().Info("syncing atxs for epoch", epoch)
				if err := s.fetcher.GetEpochATXs(ctx, epoch); err != nil {
					s.logger.WithContext(ctx).With().Error("failed to fetch epoch atxs", epoch, log.Err(err))
					return false
				}
				s.setLastSyncedATXs(epoch)
			}
			logger.With().Info("atxs synced to epoch", s.getLastSyncedATXs())
			s.setATXSynced()
		}

		if missing := s.mesh.MissingLayer(); (missing != types.LayerID{}) {
			logger.With().Info("fetching data for missing layer", missing)
			if err := s.syncLayer(ctx, missing); err != nil {
				logger.With().Warning("failed to fetch missing layer", missing, log.Err(err))
				return false
			}
		}
		// no need to sync a layer that's already processed (advanced by the hare)
		processed := s.mesh.ProcessedLayer()
		if s.getLastSyncedLayer().Before(processed) {
			s.setLastSyncedLayer(processed)
		}
		// always sync to currentLayer-1 to reduce race with gossip and hare/tortoise
		for layerID := s.getLastSyncedLayer().Add(1); layerID.Before(s.ticker.GetCurrentLayer()); layerID = layerID.Add(1) {
			if err := s.syncLayer(ctx, layerID); err != nil {
				logger.With().Warning("failed to fetch layer", layerID, log.Err(err))
				return false
			}
			s.setLastSyncedLayer(layerID)
		}
		logger.With().Debug("data is synced",
			log.Stringer("current", s.ticker.GetCurrentLayer()),
			log.Stringer("latest", s.mesh.LatestLayer()),
			log.Stringer("last_synced", s.getLastSyncedLayer()))
		return true
	}

	success := syncFunc()
	s.setStateAfterSync(ctx, success)
	logger.With().Info(fmt.Sprintf("finished sync run #%v", s.run),
		log.Bool("success", success),
		log.String("sync_state", s.getSyncState().String()),
		log.Stringer("current", s.ticker.GetCurrentLayer()),
		log.Stringer("latest", s.mesh.LatestLayer()),
		log.Stringer("last_synced", s.getLastSyncedLayer()),
		log.Stringer("processed", s.mesh.ProcessedLayer()))
	return success
}

func isTooFarBehind(current, latest types.LayerID, logger log.Logger) bool {
	if current.After(latest) && current.Difference(latest) >= outOfSyncThreshold {
		logger.With().Info("node is too far behind",
			log.Stringer("current", current),
			log.Stringer("latest", latest),
			log.Uint32("behind_threshold", outOfSyncThreshold))
		return true
	}
	return false
}

func (s *Syncer) setStateBeforeSync(ctx context.Context) {
	current := s.ticker.GetCurrentLayer()
	if current.Uint32() <= 1 {
		s.setATXSynced()
		s.setSyncState(ctx, synced)
		return
	}
	latest := s.mesh.LatestLayer()
	if isTooFarBehind(current, latest, s.logger.WithContext(ctx)) {
		s.setSyncState(ctx, notSynced)
	}
}

func (s *Syncer) dataSynced() bool {
	current := s.ticker.GetCurrentLayer()
	return current.Uint32() <= 1 || !s.getLastSyncedLayer().Before(current.Sub(1))
}

func (s *Syncer) stateSynced() bool {
	current := s.ticker.GetCurrentLayer()
	return current.Uint32() <= 1 || !s.mesh.ProcessedLayer().Before(current.Sub(1))
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
		if !success || !s.dataSynced() {
			// push out the target synced layer
			s.setTargetSyncedLayer(ctx, current.Add(numGossipSyncLayers))
			break
		}
		// if we have gossip-synced to the target synced layer, we are ready to participate in consensus
		if !s.getTargetSyncedLayer().After(current) {
			s.setSyncState(ctx, synced)
		}
	case notSynced:
		if success && s.dataSynced() {
			// wait till s.ticker.GetCurrentLayer() + numGossipSyncLayers to participate in consensus
			s.setSyncState(ctx, gossipSync)
			s.setTargetSyncedLayer(ctx, current.Add(numGossipSyncLayers))
		}
	}
}

func (s *Syncer) syncLayer(ctx context.Context, layerID types.LayerID) error {
	if s.isClosed() {
		return errors.New("shutdown")
	}

	s.logger.WithContext(ctx).With().Info("polling layer content", layerID)
	if err := s.getLayerFromPeers(ctx, layerID); err != nil {
		return err
	}

	return nil
}

func (s *Syncer) getLayerFromPeers(ctx context.Context, layerID types.LayerID) error {
	bch := s.fetcher.PollLayerContent(ctx, layerID)
	res := <-bch
	if res.Err != nil {
		return fmt.Errorf("PollLayerContent: %w", res.Err)
	}
	return nil
}

func minLayer(a, b types.LayerID) types.LayerID {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *Syncer) processLayers(ctx context.Context) error {
	if !s.ListenToATXGossip() {
		return errATXsNotSynced
	}

	s.logger.WithContext(ctx).With().Info("processing synced layers",
		log.Stringer("processed", s.mesh.ProcessedLayer()),
		log.Stringer("in_state", s.mesh.LatestLayerInState()),
		log.Stringer("last_synced", s.getLastSyncedLayer()))

	start := minLayer(s.mesh.LatestLayerInState(), s.mesh.ProcessedLayer())
	if !start.Before(s.getLastSyncedLayer()) {
		return nil
	}

	for lid := start.Add(1); !lid.After(s.getLastSyncedLayer()); lid = lid.Add(1) {
		if s.isClosed() {
			return errShuttingDown
		}

		// layers should be processed in order. once we skip one layer, there is no point
		// continuing with later layers. return on error
		epoch := lid.GetEpoch()
		if _, err := s.beacons.GetBeacon(epoch); err != nil {
			s.logger.With().Info("skip validating layer: beacon not available", lid, epoch)
			return errBeaconUnavailable
		}

		if s.patrol.IsHareInCharge(lid) {
			lag := types.NewLayerID(0)
			current := s.ticker.GetCurrentLayer()
			if current.After(lid) {
				lag = current.Sub(lid.Uint32())
			}
			if lag.Value < maxHareDelayLayers {
				s.logger.With().Info("skip validating layer: hare still working", lid)
				return errHareInCharge
			}
		}

		if err := s.lyrProcessor.ProcessLayer(ctx, lid); err != nil {
			s.logger.WithContext(ctx).With().Warning("mesh failed to process layer from sync", log.Err(err))
			return fmt.Errorf("process layer: %w", err)
		}
	}
	s.logger.With().Info("end of state sync",
		log.Bool("state_synced", s.stateSynced()),
		log.Stringer("last_synced", s.getLastSyncedLayer()),
		log.Stringer("processed", s.mesh.ProcessedLayer()))
	return nil
}
