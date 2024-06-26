package syncer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/syncer/atxsync"
	"github.com/spacemeshos/go-spacemesh/syncer/malsync"
	"github.com/spacemeshos/go-spacemesh/system"
)

// Config is the config params for syncer.
type Config struct {
	Interval         time.Duration `mapstructure:"interval"`
	EpochEndFraction float64       `mapstructure:"epochendfraction"`
	HareDelayLayers  uint32
	SyncCertDistance uint32
	// TallyVotesFrequency how often to tally votes during layers sync.
	// Setting this to 0.25 will tally votes after downloading data for quarter of the epoch.
	TallyVotesFrequency      float64
	MaxStaleDuration         time.Duration `mapstructure:"maxstaleduration"`
	Standalone               bool
	GossipDuration           time.Duration  `mapstructure:"gossipduration"`
	DisableMeshAgreement     bool           `mapstructure:"disable-mesh-agreement"`
	OutOfSyncThresholdLayers uint32         `mapstructure:"out-of-sync-threshold"`
	AtxSync                  atxsync.Config `mapstructure:"atx-sync"`
	MalSync                  malsync.Config `mapstructure:"malfeasance-sync"`
}

// DefaultConfig for the syncer.
func DefaultConfig() Config {
	return Config{
		Interval:                 10 * time.Second,
		EpochEndFraction:         0.5,
		HareDelayLayers:          10,
		SyncCertDistance:         10,
		TallyVotesFrequency:      0.25,
		MaxStaleDuration:         time.Second,
		GossipDuration:           15 * time.Second,
		OutOfSyncThresholdLayers: 3,
		AtxSync:                  atxsync.DefaultConfig(),
		MalSync:                  malsync.DefaultConfig(),
	}
}

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
	errHareInCharge  = errors.New("hare in charge of layer")
	errATXsNotSynced = errors.New("ATX not synced")
)

// Option is a type to configure a syncer.
type Option func(*Syncer)

// WithConfig ...
func WithConfig(c Config) Option {
	return func(s *Syncer) {
		s.cfg = c
	}
}

// WithLogger ...
func WithLogger(l log.Log) Option {
	return func(s *Syncer) {
		s.logger = l
	}
}

func withDataFetcher(d fetchLogic) Option {
	return func(s *Syncer) {
		s.dataFetcher = d
	}
}

func withForkFinder(f forkFinder) Option {
	return func(s *Syncer) {
		s.forkFinder = f
	}
}

// Syncer is responsible to keep the node in sync with the network.
type Syncer struct {
	logger log.Log

	cfg          Config
	cdb          *datastore.CachedDB
	atxsyncer    atxSyncer
	malsyncer    malSyncer
	ticker       layerTicker
	beacon       system.BeaconGetter
	mesh         *mesh.Mesh
	tortoise     system.Tortoise
	certHandler  certHandler
	dataFetcher  fetchLogic
	patrol       layerPatrol
	forkFinder   forkFinder
	syncOnce     sync.Once
	syncState    atomic.Value
	atxSyncState atomic.Value
	isBusy       atomic.Bool
	// syncedTargetTime is used to signal at which time we can set this node to synced state
	syncedTargetTime time.Time
	lastLayerSynced  atomic.Uint32
	lastEpochSynced  atomic.Uint32
	stateErr         atomic.Bool

	// backgroundSync always runs one sync operation in the background.
	backgroundSync struct {
		epoch  atomic.Uint32
		eg     errgroup.Group
		cancel context.CancelFunc
	}

	// malSync runs malfeasant identity sync in the background
	malSync struct {
		started bool
		eg      errgroup.Group
	}

	// awaitATXSyncedCh is the list of subscribers' channels to notify when this node enters ATX synced state
	awaitATXSyncedCh chan struct{}

	eg   errgroup.Group
	stop context.CancelFunc
}

// NewSyncer creates a new Syncer instance.
func NewSyncer(
	cdb *datastore.CachedDB,
	ticker layerTicker,
	beacon system.BeaconGetter,
	mesh *mesh.Mesh,
	tortoise system.Tortoise,
	fetcher fetcher,
	patrol layerPatrol,
	ch certHandler,
	atxSyncer atxSyncer,
	malSyncer malSyncer,
	opts ...Option,
) *Syncer {
	s := &Syncer{
		logger:           log.NewNop(),
		cfg:              DefaultConfig(),
		cdb:              cdb,
		atxsyncer:        atxSyncer,
		malsyncer:        malSyncer,
		ticker:           ticker,
		beacon:           beacon,
		mesh:             mesh,
		tortoise:         tortoise,
		certHandler:      ch,
		patrol:           patrol,
		awaitATXSyncedCh: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}

	if s.dataFetcher == nil {
		s.dataFetcher = NewDataFetch(mesh, fetcher, tortoise, s.logger)
	}
	if s.forkFinder == nil {
		s.forkFinder = NewForkFinder(s.logger, cdb, fetcher, s.cfg.MaxStaleDuration)
	}
	s.syncState.Store(notSynced)
	s.atxSyncState.Store(notSynced)
	s.isBusy.Store(false)
	s.lastLayerSynced.Store(s.mesh.LatestLayer().Uint32())
	s.lastEpochSynced.Store(types.GetEffectiveGenesis().GetEpoch().Uint32() - 1)
	return s
}

// Close stops the syncing process and the goroutines syncer spawns.
func (s *Syncer) Close() {
	if s.stop == nil {
		return // not started yet
	}
	s.stop()
	s.logger.With().Info("waiting for syncer goroutines to finish")
	err := s.eg.Wait()
	s.logger.With().Info("all syncer goroutines finished", log.Err(err))
}

// RegisterForATXSynced returns a channel for notification when the node enters ATX synced state.
func (s *Syncer) RegisterForATXSynced() <-chan struct{} {
	return s.awaitATXSyncedCh
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
	return s.getSyncState() == synced
}

func (s *Syncer) IsBeaconSynced(epoch types.EpochID) bool {
	_, err := s.beacon.GetBeacon(epoch)
	return err == nil
}

// Start starts the main sync loop that tries to sync data for every SyncInterval.
func (s *Syncer) Start() {
	s.syncOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		s.stop = cancel
		s.logger.WithContext(ctx).Info("starting syncer loop")
		s.eg.Go(func() error {
			if s.ticker.CurrentLayer() <= types.GetEffectiveGenesis() {
				s.setSyncState(ctx, synced)
			}
			for {
				select {
				case <-ctx.Done():
					s.logger.WithContext(ctx).Info("stopping sync to shutdown")
					return fmt.Errorf("shutdown context done: %w", ctx.Err())
				case <-time.After(s.cfg.Interval):
					ok := s.synchronize(ctx)
					if ok {
						runSuccess.Inc()
					} else {
						runFail.Inc()
					}
				}
			}
		})
		s.logger.WithContext(ctx).Info("starting syncer layer processing loop")
		s.eg.Go(func() error {
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(s.cfg.Interval):
					if err := s.processLayers(ctx); err != nil {
						sRunFail.Inc()
					} else {
						sRunSuccess.Inc()
					}
					s.forkFinder.Purge(false)
				}
			}
		})
	})
}

func (s *Syncer) setATXSynced() {
	s.atxSyncState.Store(synced)
	select {
	case <-s.awaitATXSyncedCh:
	default:
		s.logger.Info("reached ATX synced state")
		close(s.awaitATXSyncedCh)
		atxSynced.Set(1)
	}
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
			log.String("from state", oldState.String()),
			log.String("to state", newState.String()),
			log.Stringer("current", s.ticker.CurrentLayer()),
			log.Stringer("last synced", s.getLastSyncedLayer()),
			log.Stringer("latest", s.mesh.LatestLayer()),
			log.Stringer("processed", s.mesh.ProcessedLayer()))
		events.ReportNodeStatusUpdate()
	}
	switch newState {
	case notSynced:
		nodeNotSynced.Set(1)
		nodeGossip.Set(0)
		nodeSynced.Set(0)
	case gossipSync:
		nodeNotSynced.Set(0)
		nodeGossip.Set(1)
		nodeSynced.Set(0)
	case synced:
		nodeNotSynced.Set(0)
		nodeGossip.Set(0)
		nodeSynced.Set(1)
	}
}

// setSyncerBusy returns false if the syncer is already running a sync process.
// otherwise it sets syncer to be busy and returns true.
func (s *Syncer) setSyncerBusy() bool {
	return s.isBusy.CompareAndSwap(false, true)
}

func (s *Syncer) setSyncerIdle() {
	s.isBusy.Store(false)
}

func (s *Syncer) setLastSyncedLayer(lid types.LayerID) {
	s.lastLayerSynced.Store(lid.Uint32())
	syncedLayer.Set(float64(lid))
}

func (s *Syncer) getLastSyncedLayer() types.LayerID {
	return types.LayerID(s.lastLayerSynced.Load())
}

func (s *Syncer) setLastAtxEpoch(epoch types.EpochID) {
	s.lastEpochSynced.Store(epoch.Uint32())
}

func (s *Syncer) lastAtxEpoch() types.EpochID {
	return types.EpochID(s.lastEpochSynced.Load())
}

// synchronize sync data up to the currentLayer-1 and wait for the layers to be validated.
// it returns false if the data sync failed.
func (s *Syncer) synchronize(ctx context.Context) bool {
	ctx = log.WithNewSessionID(ctx)

	select {
	case <-ctx.Done():
		s.logger.WithContext(ctx).Warning("attempting to sync while shutting down")
		return false
	default:
	}
	// at most one synchronize process can run at any time
	if !s.setSyncerBusy() {
		s.logger.WithContext(ctx).Info("sync is already running, giving up")
		return false
	}
	defer s.setSyncerIdle()

	s.setStateBeforeSync(ctx)
	if s.ticker.CurrentLayer().Uint32() == 0 {
		return false
	}

	// no need to worry about race condition for s.run. only one instance of synchronize can run at a time
	s.logger.WithContext(ctx).With().Debug("starting sync run",
		log.Stringer("sync_state", s.getSyncState()),
		log.Stringer("last_synced", s.getLastSyncedLayer()),
		log.Stringer("current", s.ticker.CurrentLayer()),
		log.Stringer("latest", s.mesh.LatestLayer()),
		log.Stringer("in_state", s.mesh.LatestLayerInState()),
		log.Stringer("processed", s.mesh.ProcessedLayer()),
	)
	// TODO
	// https://github.com/spacemeshos/go-spacemesh/issues/3987
	syncFunc := func() bool {
		if s.cfg.Standalone {
			s.setLastSyncedLayer(s.ticker.CurrentLayer().Sub(1))
			s.setATXSynced()
			return true
		}
		// check that we have any peers
		if len(s.dataFetcher.SelectBestShuffled(1)) == 0 {
			return false
		}

		if err := s.syncAtx(ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				s.logger.With().Error("failed to sync atxs", log.Context(ctx), log.Err(err))
			}
			return false
		}

		if s.ticker.CurrentLayer() <= types.GetEffectiveGenesis() {
			return true
		}
		// always sync to currentLayer-1 to reduce race with gossip and hare/tortoise
		for layer := s.getLastSyncedLayer().Add(1); layer.Before(s.ticker.CurrentLayer()); layer = layer.Add(1) {
			if err := s.syncLayer(ctx, layer); err != nil {
				batchError := &fetch.BatchError{}
				if errors.As(err, &batchError) && batchError.Ignore() {
					s.logger.With().
						Info("remaining ballots are rejected in the layer", log.Context(ctx), log.Err(err), layer)
				} else {
					if !errors.Is(err, context.Canceled) {
						// BatchError spams too much, in case of no progress enable debug mode for sync
						s.logger.With().
							Debug("failed to sync layer", log.Context(ctx), log.Err(err), layer)
					}
					return false
				}
			}
			s.setLastSyncedLayer(layer)
		}
		s.logger.WithContext(ctx).With().Debug("data is synced",
			log.Stringer("current", s.ticker.CurrentLayer()),
			log.Stringer("latest", s.mesh.LatestLayer()),
			log.Stringer("last_synced", s.getLastSyncedLayer()))
		return true
	}

	success := syncFunc()
	s.setStateAfterSync(ctx, success)
	s.logger.WithContext(ctx).With().Debug("finished sync run",
		log.Bool("success", success),
		log.Stringer("sync_state", s.getSyncState()),
		log.Stringer("last_synced", s.getLastSyncedLayer()),
		log.Stringer("current", s.ticker.CurrentLayer()),
		log.Stringer("latest", s.mesh.LatestLayer()),
		log.Stringer("in_state", s.mesh.LatestLayerInState()),
		log.Stringer("processed", s.mesh.ProcessedLayer()),
	)
	return success
}

func (s *Syncer) syncAtx(ctx context.Context) error {
	current := s.ticker.CurrentLayer()
	// on startup always download all activations that were published before current epoch
	if !s.ListenToATXGossip() {
		s.logger.With().Debug("syncing atx from genesis", log.Context(ctx), current, s.lastAtxEpoch())
		for epoch := s.lastAtxEpoch() + 1; epoch < current.GetEpoch(); epoch++ {
			if err := s.fetchATXsForEpoch(ctx, epoch, false); err != nil {
				return err
			}
		}
		s.logger.With().Debug("atxs synced to epoch", log.Context(ctx), s.lastAtxEpoch())

		// FIXME https://github.com/spacemeshos/go-spacemesh/issues/3987
		s.logger.With().Info("syncing malicious proofs", log.Context(ctx))
		if err := s.syncMalfeasance(ctx, current.GetEpoch()); err != nil {
			return err
		}
		s.logger.With().Info("malicious IDs synced", log.Context(ctx))
		s.setATXSynced()
	}

	publish := current.GetEpoch()
	if publish == 0 {
		return nil // nothing to sync in epoch 0
	}

	// if we are not advanced enough sync previous epoch, otherwise start syncing activations published in this epoch
	if current.OrdinalInEpoch() <= uint32(float64(types.GetLayersPerEpoch())*s.cfg.EpochEndFraction) {
		publish -= 1
	}
	if epoch := s.backgroundSync.epoch.Load(); epoch != 0 && epoch != publish.Uint32() {
		s.backgroundSync.cancel()
		s.backgroundSync.eg.Wait()
		s.backgroundSync.epoch.Store(0)
	}
	if s.backgroundSync.epoch.Load() == 0 && publish.Uint32() != 0 {
		s.logger.With().Debug("download atx for epoch in background", publish, log.Context(ctx))
		s.backgroundSync.epoch.Store(publish.Uint32())
		ctx, cancel := context.WithCancel(ctx)
		s.backgroundSync.cancel = cancel
		s.backgroundSync.eg.Go(func() error {
			err := s.fetchATXsForEpoch(ctx, publish, true)
			if err == nil {
				return nil
			}
			if !errors.Is(err, context.Canceled) {
				s.logger.With().
					Warning("background atx sync failed", log.Context(ctx), publish.Field(), log.Err(err))
			} else {
				s.logger.With().Debug("background atx sync stopped", log.Context(ctx), publish.Field())
			}
			s.backgroundSync.epoch.Store(0)
			return err
		})
	}
	if !s.malSync.started {
		s.malSync.started = true
		s.malSync.eg.Go(func() error {
			select {
			case <-ctx.Done():
				return nil
			case <-s.awaitATXSyncedCh:
				err := s.malsyncer.DownloadLoop(ctx)
				if err != nil && !errors.Is(err, context.Canceled) {
					s.logger.WithContext(ctx).Error("malfeasance sync failed", log.Err(err))
				}
				return nil
			}
		})
	}
	return nil
}

func isTooFarBehind(
	ctx context.Context,
	logger log.Log,
	current, lastSynced types.LayerID,
	outOfSyncThreshold uint32,
) bool {
	if current.After(lastSynced) && current.Difference(lastSynced) >= outOfSyncThreshold {
		logger.WithContext(ctx).With().Info("node is too far behind",
			log.Stringer("current", current),
			log.Stringer("last synced", lastSynced),
			log.Uint32("behind threshold", outOfSyncThreshold))
		return true
	}
	return false
}

func (s *Syncer) setStateBeforeSync(ctx context.Context) {
	current := s.ticker.CurrentLayer()
	if s.ticker.CurrentLayer() <= types.GetEffectiveGenesis() {
		s.setSyncState(ctx, synced)
		if current.GetEpoch() == 0 {
			s.setATXSynced()
		}
		return
	}
	if isTooFarBehind(
		ctx,
		s.logger,
		current,
		s.getLastSyncedLayer(),
		s.cfg.OutOfSyncThresholdLayers,
	) {
		s.setSyncState(ctx, notSynced)
	}
}

func (s *Syncer) dataSynced() bool {
	current := s.ticker.CurrentLayer()
	return current.Uint32() <= 1 || !s.getLastSyncedLayer().Before(current.Sub(1))
}

func (s *Syncer) setStateAfterSync(ctx context.Context, success bool) {
	currSyncState := s.getSyncState()
	current := s.ticker.CurrentLayer()

	// for the gossipSync/notSynced states, we check if the mesh state is on target before we advance sync state.
	// but for the synced state, we don't check the mesh state because gossip+hare+tortoise are in charge of
	// advancing processed/verified layers.  syncer is just auxiliary that fetches data in case of a temporary
	// network outage.
	switch currSyncState {
	case synced:
		if !success &&
			isTooFarBehind(
				ctx,
				s.logger,
				current,
				s.getLastSyncedLayer(),
				s.cfg.OutOfSyncThresholdLayers,
			) {
			s.setSyncState(ctx, notSynced)
		}
	case gossipSync:
		if !success || !s.dataSynced() || !s.stateSynced() {
			// push out the target synced layer
			s.syncedTargetTime = time.Now().Add(s.cfg.GossipDuration)
			s.logger.With().Info("extending gossip sync",
				log.Bool("success", success),
				log.Bool("data", s.dataSynced()),
				log.Bool("state", s.stateSynced()),
			)
			break
		}
		// if we have gossip-synced long enough, we are ready to participate in consensus
		if !time.Now().Before(s.syncedTargetTime) {
			s.setSyncState(ctx, synced)
		}
	case notSynced:
		if success && s.dataSynced() && s.stateSynced() {
			// wait till s.ticker.GetCurrentLayer() + numGossipSyncLayers to participate in consensus
			s.setSyncState(ctx, gossipSync)
			s.syncedTargetTime = time.Now().Add(s.cfg.GossipDuration)
		}
	}
}

func (s *Syncer) syncMalfeasance(ctx context.Context, epoch types.EpochID) error {
	epochStart := s.ticker.LayerToTime(epoch.FirstLayer())
	epochEnd := s.ticker.LayerToTime(epoch.Add(1).FirstLayer())
	if err := s.malsyncer.EnsureInSync(ctx, epochStart, epochEnd); err != nil {
		return fmt.Errorf("syncing malfeasance proof: %w", err)
	}
	return nil
}

func (s *Syncer) syncLayer(ctx context.Context, layerID types.LayerID, peers ...p2p.Peer) error {
	if err := s.dataFetcher.PollLayerData(ctx, layerID, peers...); err != nil {
		return err
	}
	dataLayer.Set(float64(layerID))
	return nil
}

// fetching ATXs published the specified epoch.
func (s *Syncer) fetchATXsForEpoch(ctx context.Context, publish types.EpochID, background bool) error {
	target := publish + 1
	if background {
		target++
	}
	downloadUntil := s.ticker.LayerToTime(target.FirstLayer())
	if err := s.atxsyncer.Download(ctx, publish, downloadUntil); err != nil {
		return err
	}
	s.setLastAtxEpoch(publish)
	atxEpoch.Set(float64(publish))
	return nil
}

// waitBackgroundSync is a helper to wait for the background sync to finish.
func (s *Syncer) waitBackgroundSync() {
	s.backgroundSync.eg.Wait()
}
