package sync

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	p2pconf "github.com/spacemeshos/go-spacemesh/p2p/config"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

type txMemPool interface {
	Get(id types.TransactionID) (*types.Transaction, error)
	Put(id types.TransactionID, item *types.Transaction)
}

type atxDB interface {
	ProcessAtx(atx *types.ActivationTx) error
	GetFullAtx(id types.ATXID) (*types.ActivationTx, error)
	GetEpochAtxs(epochID types.EpochID) (atxs []types.ATXID)
}

type poetDb interface {
	HasProof(proofRef []byte) bool
	ValidateAndStore(proofMessage *types.PoetProofMessage) error
	GetProofMessage(proofRef []byte) ([]byte, error)
}

type blockEligibilityValidator interface {
	BlockSignedAndEligible(block *types.Block) (bool, error)
}

type ticker interface {
	Subscribe() timesync.LayerTimer
	Unsubscribe(timer timesync.LayerTimer)
	GetCurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}

type net struct {
	peers
	RequestTimeout time.Duration
	*server.MessageServer
	exit chan struct{}
}

func (ms net) Close() {
	ms.MessageServer.Close()
	ms.peers.Close()
}

func (ms net) GetTimeout() time.Duration {
	return ms.RequestTimeout
}

func (ms net) GetExit() chan struct{} {
	return ms.exit
}

// Configuration represents all config params needed by syncer
type Configuration struct {
	LayersPerEpoch  uint16
	Concurrency     int // number of workers for sync method
	LayerSize       int
	RequestTimeout  time.Duration
	SyncInterval    time.Duration
	ValidationDelta time.Duration
	AtxsLimit       int
	Hdist           int
	AlwaysListen    bool
	GoldenATXID     types.ATXID
}

var (
	errDupTx           = errors.New("duplicate TransactionID in block")
	errDupAtx          = errors.New("duplicate ATXID in block")
	errNoBlocksInLayer = errors.New("layer has no blocks")
	errNoActiveSet     = errors.New("block does not declare active set")
	errZeroActiveSet   = errors.New("block declares empty active set")
	errInvalidATXID    = errors.New("invalid ATXID")

	emptyLayer = types.Layer{}.Hash()
)

type status int

func (s *status) String() string {
	if *s == 0 {
		return "pending"
	}
	if *s == 1 {
		return "inProgress"
	}
	if *s == 3 {
		return "inProgress2"
	}

	return "done"
}

const (
	pending     status = 0
	inProgress  status = 1
	done        status = 2
	inProgress2 status = 3

	blockMsg      server.MessageType = 1
	layerHashMsg  server.MessageType = 2
	layerIdsMsg   server.MessageType = 3
	txMsg         server.MessageType = 4
	atxMsg        server.MessageType = 5
	poetMsg       server.MessageType = 6
	atxIdsMsg     server.MessageType = 7
	atxIdrHashMsg server.MessageType = 8
	inputVecMsg   server.MessageType = 9

	syncProtocol                      = "/sync/1.0/"
	validatingLayerNone types.LayerID = 0
)

// Syncer is used to sync the node with the network
// periodically the Syncer will check if the node is synced with the rest of the network
// and will follow the sync protocol in order to fetch all missing data and become synced again
type Syncer struct {
	log.Log
	Configuration
	*mesh.Mesh
	blockEligibilityValidator
	*net
	ticker

	poetDb poetDb
	txpool txMemPool
	atxDb  atxDB

	validatingLayer      types.LayerID
	validatingLayerMutex sync.Mutex
	syncLock             types.TryMutex
	startLock            types.TryMutex
	forceSync            chan bool
	syncTimer            *time.Ticker
	exit                 chan struct{}
	gossipLock           sync.RWMutex
	gossipSynced         status
	awaitCh              chan struct{}

	blockQueue *blockQueue
	txQueue    *txQueue
	atxQueue   *atxQueue
}

var _ service.Fetcher = (*Syncer)(nil)

// NewSync fires a sync every sm.SyncInterval or on force space from outside
func NewSync(ctx context.Context, srv service.Service, layers *mesh.Mesh, txpool txMemPool, atxDB atxDB, bv blockEligibilityValidator, poetdb poetDb, conf Configuration, clock ticker, logger log.Log) *Syncer {
	exit := make(chan struct{})
	srvr := &net{
		RequestTimeout: conf.RequestTimeout,
		MessageServer:  server.NewMsgServer(ctx, srv.(server.Service), syncProtocol, conf.RequestTimeout, make(chan service.DirectMessage, p2pconf.Values.BufferSize), logger),
		peers:          p2ppeers.NewPeers(srv, logger.WithName("peers")),
		exit:           exit,
	}

	s := &Syncer{
		blockEligibilityValidator: bv,
		Configuration:             conf,
		Log:                       logger,
		Mesh:                      layers,
		net:                       srvr,
		ticker:                    clock,
		syncLock:                  types.TryMutex{},
		poetDb:                    poetdb,
		txpool:                    txpool,
		atxDb:                     atxDB,
		startLock:                 types.TryMutex{},
		forceSync:                 make(chan bool),
		validatingLayer:           validatingLayerNone,
		syncTimer:                 time.NewTicker(conf.SyncInterval),
		exit:                      exit,
		gossipSynced:              pending,
		awaitCh:                   make(chan struct{}),
	}

	s.blockQueue = newValidationQueue(ctx, srvr, conf, s)
	s.txQueue = newTxQueue(ctx, s)
	s.atxQueue = newAtxQueue(ctx, s, s.FetchPoetProof)
	srvr.RegisterBytesMsgHandler(layerHashMsg, newLayerHashRequestHandler(layers, logger))
	srvr.RegisterBytesMsgHandler(blockMsg, newBlockRequestHandler(layers, logger))
	srvr.RegisterBytesMsgHandler(layerIdsMsg, newLayerBlockIdsRequestHandler(layers, logger))
	srvr.RegisterBytesMsgHandler(txMsg, newTxsRequestHandler(s, logger))
	srvr.RegisterBytesMsgHandler(atxMsg, newAtxsRequestHandler(s, logger))
	srvr.RegisterBytesMsgHandler(poetMsg, newPoetRequestHandler(s, logger))
	srvr.RegisterBytesMsgHandler(atxIdsMsg, newEpochAtxsRequestHandler(s, logger))
	srvr.RegisterBytesMsgHandler(atxIdrHashMsg, newAtxHashRequestHandler(s, logger))
	srvr.RegisterBytesMsgHandler(inputVecMsg, newInputVecRequestHandler(s, logger))

	return s
}

// ForceSync signals syncer to run the synchronise flow
func (s *Syncer) ForceSync() {
	s.forceSync <- true
}

// Close closes all running goroutines
func (s *Syncer) Close() {
	s.Info("closing sync")
	s.startLock.Lock()
	close(s.exit)
	close(s.forceSync)
	s.startLock.Unlock()
	s.peers.Close()
	s.syncLock.Lock()
	s.syncLock.Unlock()
	s.MessageServer.Close()
	s.blockQueue.Close()
	s.atxQueue.Close()
	s.txQueue.Close()

	s.Info("sync closed")
}

// check if syncer was closed
func (s *Syncer) isClosed() bool {
	select {
	case <-s.exit:
		s.Info("received interrupt")
		return true
	default:
		return false
	}
}

// equivalent to s.LatestLayer() >= s.lastTickedLayer()-1
// means we have data from the previous layer
func (s *Syncer) weaklySynced(layer types.LayerID) bool {
	return s.LatestLayer()+1 >= layer
}

func (s *Syncer) getGossipBufferingStatus() status {
	s.gossipLock.RLock()
	b := s.gossipSynced
	s.gossipLock.RUnlock()
	return b
}

// Await returns a channel that blocks until the node is synced
func (s *Syncer) Await() chan struct{} {
	return s.awaitCh
}

func (s *Syncer) notifySubscribers(prevStatus, status status) {
	if (status == done) == (prevStatus == done) {
		return
	}
	if status == done {
		close(s.awaitCh)
	} else {
		s.awaitCh = make(chan struct{})
	}
}

// ListenToGossip enables other modules to check if they should listen to gossip
func (s *Syncer) ListenToGossip() bool {
	return s.AlwaysListen || s.getGossipBufferingStatus() != pending
}

func (s *Syncer) setGossipBufferingStatus(status status) {
	s.gossipLock.Lock()
	defer s.gossipLock.Unlock()
	if status == s.gossipSynced {
		return
	}
	s.Info("setting gossip to '%s'", status.String())
	s.notifySubscribers(s.gossipSynced, status)
	s.gossipSynced = status

}

// IsSynced returns true if the node is synced false otherwise
func (s *Syncer) IsSynced(ctx context.Context) bool {
	s.WithContext(ctx).Info("sync state w: %v, g:%v layer : %v latest: %v",
		s.weaklySynced(s.GetCurrentLayer()),
		s.getGossipBufferingStatus(),
		s.GetCurrentLayer(),
		s.LatestLayer())
	return s.weaklySynced(s.GetCurrentLayer()) && s.getGossipBufferingStatus() == done
}

// IsHareSynced returns true if the hare is synced false otherwise
func (s *Syncer) IsHareSynced(ctx context.Context) bool {
	return s.getGossipBufferingStatus() == inProgress2 || s.IsSynced(ctx)
}

// Start starts the main pooling routine that checks the sync status every set interval
// and calls synchronise if the node is out of sync
func (s *Syncer) Start(ctx context.Context) {
	logger := s.WithContext(ctx)
	if s.startLock.TryLock() {
		defer s.startLock.Unlock()
		if s.isClosed() {
			logger.Warning("sync started after closed")
			return
		}
		logger.Info("start syncer")
		go s.run(log.WithNewSessionID(ctx))
		s.forceSync <- true
		return
	}
	logger.Info("syncer already started")
}

// fires a sync every sm.SyncInterval or on force sync from outside
func (s *Syncer) run(ctx context.Context) {
	s.WithContext(ctx).Debug("start running")
	for {
		select {
		case <-s.exit:
			s.WithContext(ctx).Debug("work stopped")
			return
		case <-s.forceSync:
			go s.synchronise(ctx)
		case <-s.syncTimer.C:
			go s.synchronise(ctx)
		}
	}
}

func (s *Syncer) synchronise(ctx context.Context) {
	s.WithContext(ctx).Debug("synchronizing")

	// only one concurrent synchronise
	if s.syncLock.TryLock() == false {
		s.WithContext(ctx).Info("sync is already running, giving up")
		return
	}

	// release synchronise lock
	defer s.syncLock.Unlock()
	curr := s.GetCurrentLayer()

	// node is synced and blocks from current layer have already been validated
	if curr == s.ProcessedLayer() {
		s.WithContext(ctx).Debug("node is synced")
		// fully-synced, make sure we listen to p2p
		s.setGossipBufferingStatus(done)
		return
	}

	// we have all the data of the prev layers so we can simply validate
	if s.weaklySynced(curr) {
		s.handleWeaklySynced(ctx)
		if err := s.syncEpochActivations(ctx, curr.GetEpoch()); err != nil {
			if curr.GetEpoch().IsGenesis() {
				s.WithContext(ctx).With().Info("cannot fetch epoch atxs (expected during genesis)", curr, log.Err(err))
			} else {
				s.WithContext(ctx).With().Error("cannot fetch epoch atxs", curr, log.Err(err))
			}
		}
	} else {
		s.handleNotSynced(ctx, s.ProcessedLayer()+1)
	}
}

func (s *Syncer) handleWeaklySynced(ctx context.Context) {
	logger := s.WithContext(ctx)
	logger.With().Info("node is weakly synced",
		s.LatestLayer(),
		s.GetCurrentLayer())
	events.ReportNodeStatusUpdate()

	// handle all layers from processed+1 to current -1
	s.handleLayersTillCurrent(ctx)

	if s.isClosed() {
		return
	}

	// validate current layer if more than s.ValidationDelta has passed
	// TODO: remove this since hare runs it?
	if err := s.handleCurrentLayer(ctx); err != nil {
		logger.With().Error("node is out of sync", log.Err(err))
		s.setGossipBufferingStatus(pending)
		return
	}

	if s.isClosed() {
		return
	}

	// fully-synced, make sure we listen to p2p
	s.setGossipBufferingStatus(done)
	logger.Info("node is synced")
	return
}

// validate all layers except current one
func (s *Syncer) handleLayersTillCurrent(ctx context.Context) {
	logger := s.WithContext(ctx)

	// dont handle current
	if s.ProcessedLayer()+1 >= s.GetCurrentLayer() {
		return
	}

	logger.With().Info("handle layers",
		log.FieldNamed("from", s.ProcessedLayer()+1),
		log.FieldNamed("to", s.GetCurrentLayer()-1))
	for currentSyncLayer := s.ProcessedLayer() + 1; currentSyncLayer < s.GetCurrentLayer(); currentSyncLayer++ {
		if s.isClosed() {
			return
		}
		if err := s.getAndValidateLayer(currentSyncLayer); err != nil {
			if currentSyncLayer.GetEpoch().IsGenesis() {
				logger.With().Info("failed getting layer even though we are weakly synced (expected during genesis)",
					log.FieldNamed("currentSyncLayer", currentSyncLayer),
					log.FieldNamed("currentLayer", s.GetCurrentLayer()),
					log.Err(err))
			} else {
				logger.With().Panic("failed getting layer even though we are weakly synced",
					log.FieldNamed("current_layer", currentSyncLayer),
					log.FieldNamed("last_ticked_layer", s.GetCurrentLayer()),
					log.Err(err))
			}
		}
	}
	return
}

// handle the current consensus layer if it is older than s.ValidationDelta
func (s *Syncer) handleCurrentLayer(ctx context.Context) error {
	curr := s.GetCurrentLayer()
	if s.LatestLayer() == curr && time.Now().Sub(s.LayerToTime(s.LatestLayer())) > s.ValidationDelta {
		if err := s.getAndValidateLayer(s.LatestLayer()); err != nil {
			if err != database.ErrNotFound {
				s.WithContext(ctx).With().Panic("failed handling current layer",
					log.FieldNamed("current_layer", s.LatestLayer()),
					log.FieldNamed("last_ticked_layer", s.GetCurrentLayer()),
					log.Err(err))
			}
			if err := s.SetZeroBlockLayer(curr); err != nil {
				return err
			}
		}
	}

	if s.LatestLayer()+1 == curr && curr.GetEpoch().IsGenesis() {
		_, err := s.GetLayer(s.LatestLayer())
		if err == database.ErrNotFound {
			err := s.SetZeroBlockLayer(s.LatestLayer())
			return err
		}
	}
	return nil
}

func (s *Syncer) handleNotSynced(ctx context.Context, currentSyncLayer types.LayerID) {
	logger := s.WithContext(ctx)
	logger.Info("node is out of sync, setting gossip-synced to false and starting sync")
	events.ReportNodeStatusUpdate()
	s.setGossipBufferingStatus(pending) // don't listen to gossip while not synced

	// first, bring all the data of the prev layers
	// Note: lastTicked() is not constant but updates as ticks are received
	for ; currentSyncLayer < s.GetCurrentLayer(); currentSyncLayer++ {
		logger.With().Info("syncing layer",
			log.FieldNamed("current_sync_layer", currentSyncLayer),
			log.FieldNamed("last_ticked_layer", s.GetCurrentLayer()))

		if s.isClosed() {
			return
		}

		lyr, err := s.getLayerFromNeighbors(log.WithNewRequestID(ctx, currentSyncLayer), currentSyncLayer)
		if err != nil {
			logger.With().Info("could not get layer from neighbors", currentSyncLayer, log.Err(err))
			return
		}

		if len(lyr.Blocks()) == 0 {
			if err := s.SetZeroBlockLayer(currentSyncLayer); err != nil {
				logger.With().Error("error attempting to tag zero block layer", currentSyncLayer, log.Err(err))
				return
			}
		}
		s.syncAtxs(ctx, currentSyncLayer)
		logger.With().Info("done with layer", log.FieldNamed("current_sync_layer", currentSyncLayer))
		s.ValidateLayer(lyr) // wait for layer validation
	}

	// wait for two ticks to ensure we are fully synced before we open gossip or validate the current layer
	if err := s.gossipSyncForOneFullLayer(ctx, currentSyncLayer); err != nil {
		logger.With().Error("failed getting layer from db even though we listened to gossip",
			currentSyncLayer,
			log.Err(err))
	}
}

func (s *Syncer) syncAtxs(ctx context.Context, currentSyncLayer types.LayerID) {
	lastLayerOfEpoch := (currentSyncLayer.GetEpoch() + 1).FirstLayer() - 1
	if currentSyncLayer == lastLayerOfEpoch {
		ctx = log.WithNewRequestID(ctx, currentSyncLayer.GetEpoch())
		if err := s.syncEpochActivations(ctx, currentSyncLayer.GetEpoch()); err != nil {
			if currentSyncLayer.GetEpoch().IsGenesis() {
				s.WithContext(ctx).With().Info("cannot fetch epoch atxs (expected during genesis)",
					currentSyncLayer,
					log.Err(err))
			} else {
				s.WithContext(ctx).With().Error("cannot fetch epoch atxs", currentSyncLayer, log.Err(err))
			}
		}
	}
}

// Waits two ticks (while weakly-synced) in order to ensure that we listened to gossip for one full layer
// after that we are assumed to have all the data required for validation so we can validate and open gossip
// opening gossip in weakly-synced transition us to fully-synced
func (s *Syncer) gossipSyncForOneFullLayer(ctx context.Context, currentSyncLayer types.LayerID) error {
	logger := s.WithContext(ctx)

	// listen to gossip
	// subscribe and wait for two ticks
	logger.With().Info("waiting for two ticks while p2p is open", currentSyncLayer.GetEpoch())
	ch := s.ticker.Subscribe()

	var exit bool
	var flayer types.LayerID

	if flayer, exit = s.waitLayer(ch); exit {
		return fmt.Errorf("cloed while buffering first layer")
	}

	if err := s.syncSingleLayer(ctx, currentSyncLayer); err != nil {
		return err
	}

	// get & validate first tick
	if err := s.getAndValidateLayer(currentSyncLayer); err != nil {
		if err != database.ErrNotFound {
			return err
		}
		if err := s.SetZeroBlockLayer(currentSyncLayer); err != nil {
			return err
		}
	}

	//todo: just set hare to listen when inProgress and remove inProgress2
	s.setGossipBufferingStatus(inProgress2)

	if _, done := s.waitLayer(ch); done {
		return fmt.Errorf("cloed while buffering second layer ")
	}

	if err := s.syncSingleLayer(ctx, flayer); err != nil {
		return err
	}

	// get & validate second tick
	if err := s.getAndValidateLayer(flayer); err != nil {
		if err != database.ErrNotFound {
			return err
		}
		if err := s.SetZeroBlockLayer(flayer); err != nil {
			return err
		}
	}

	s.ticker.Unsubscribe(ch) // unsub, we won't be listening on this ch anymore
	logger.Info("done waiting for ticks and validation, setting gossip true")

	// fully-synced - set gossip -synced to true
	s.setGossipBufferingStatus(done)

	return nil
}

func (s *Syncer) syncSingleLayer(ctx context.Context, currentSyncLayer types.LayerID) error {
	logger := s.WithContext(ctx)
	logger.With().Info("syncing single layer",
		log.FieldNamed("current_sync_layer", currentSyncLayer),
		log.FieldNamed("last_ticked_layer", s.GetCurrentLayer()))

	if s.isClosed() {
		return errors.New("shutdown")
	}

	lyr, err := s.getLayerFromNeighbors(ctx, currentSyncLayer)
	if err != nil {
		logger.With().Info("could not get layer from neighbors", currentSyncLayer, log.Err(err))
		return err
	}

	if len(lyr.Blocks()) == 0 {
		if err := s.SetZeroBlockLayer(currentSyncLayer); err != nil {
			logger.With().Error("handleNotSynced failed ", currentSyncLayer, log.Err(err))
			return err
		}
	}
	s.syncAtxs(ctx, currentSyncLayer)
	return nil
}

func (s *Syncer) waitLayer(ch timesync.LayerTimer) (types.LayerID, bool) {
	var l types.LayerID
	select {
	case l = <-ch:
		s.Debug("waited one layer")
	case <-s.exit:
		s.Debug("exit while buffering")
		return l, true
	}
	return l, false
}

func (s *Syncer) getLayerFromNeighbors(ctx context.Context, currentSyncLayer types.LayerID) (*types.Layer, error) {
	if len(s.peers.GetPeers()) == 0 {
		return nil, fmt.Errorf("no peers")
	}

	// fetch layer hash from each peer
	s.WithContext(ctx).With().Info("fetch layer hash", currentSyncLayer)
	m, err := s.fetchLayerHashes(ctx, currentSyncLayer)
	if err != nil {
		if err == errNoBlocksInLayer {
			return types.NewLayer(currentSyncLayer), nil
		}
		return nil, err
	}

	if s.isClosed() {
		return nil, fmt.Errorf("received interrupt")
	}

	// fetch ids for each hash
	s.WithContext(ctx).With().Info("fetch layer ids", currentSyncLayer)
	blockIds, err := s.fetchLayerBlockIds(ctx, m, currentSyncLayer)
	if err != nil {
		return nil, err
	}

	if s.isClosed() {
		return nil, fmt.Errorf("received interrupt")
	}

	blocksArr, err := s.syncLayer(ctx, currentSyncLayer, blockIds)
	if len(blocksArr) == 0 || err != nil {
		return nil, fmt.Errorf("could not get blocks for layer %v: %v", currentSyncLayer, err)
	}

	input, err := s.syncInputVector(ctx, currentSyncLayer)
	if err != nil {
		input = nil
	}

	if err := s.DB.SaveLayerInputVector(currentSyncLayer, input); err != nil {
		s.WithContext(ctx).With().Warning("couldn't save input vector to database", log.Err(err))
	}

	return types.NewExistingLayer(currentSyncLayer, blocksArr), nil
}

func (s *Syncer) syncEpochActivations(ctx context.Context, epoch types.EpochID) error {
	logger := s.WithContext(ctx)
	logger.Info("syncing atxs")
	hashes, err := s.fetchEpochAtxHashes(ctx, epoch)
	if err != nil {
		return err
	}

	atxIds, err := s.fetchEpochAtxs(ctx, hashes, epoch)
	if err != nil {
		return err
	}

	atxFields := make([]log.LoggableField, len(atxIds))
	for i := range atxIds {
		atxFields[i] = atxIds[i]
	}
	logger.With().Info("fetched atxs for epoch", log.Int("count", len(atxIds)))
	logger.With().Debug("fetched atxs for epoch", atxFields...)

	_, err = s.atxQueue.HandleAtxs(ctx, atxIds)

	return err
}

// GetAtxs fetches list of atxs from remote peers if possible
func (s *Syncer) GetAtxs(ctx context.Context, IDs []types.ATXID) error {
	_, err := s.atxQueue.HandleAtxs(ctx, IDs)
	return err
}

func (s *Syncer) syncLayer(ctx context.Context, layerID types.LayerID, blockIds []types.BlockID) ([]*types.Block, error) {
	logger := s.WithContext(ctx).WithFields(layerID)
	logger.Info("attempting to sync blocks for layer")
	ch := make(chan bool, 1)
	foo := func(ctx context.Context, res bool) error {
		logger.WithContext(ctx).Info("sync layer done")
		ch <- res
		return nil
	}

	tmr := newMilliTimer(syncLayerTime)
	defer tmr.ObserveDuration()
	if res, err := s.blockQueue.addDependencies(ctx, layerID, blockIds, foo); err != nil {
		return nil, fmt.Errorf("failed adding layer %v blocks to queue %v", layerID, err)
	} else if res == false {
		logger.Info("syncLayer: no missing blocks for layer")
		return s.LayerBlocks(layerID)
	}

	logger.With().Info("wait for layer blocks",
		log.Int("num_blocks", len(blockIds)),
		types.BlockIdsField(blockIds))

	// LANE: need a timeout here
	select {
	case <-s.exit:
		return nil, fmt.Errorf("received interupt")
	case result := <-ch:
		if !result {
			return nil, fmt.Errorf("could not get all blocks for layer %v", layerID)
		}
	}

	blocks, err := s.LayerBlocks(layerID)
	if err != nil {
		logger.With().Error("failed to get layer blocks", log.Err(err))
	} else {
		logger.With().Info("got layer blocks", log.Int("count", len(blocks)))
	}
	return blocks, err
}

func (s *Syncer) syncInputVector(ctx context.Context, layerID types.LayerID) ([]types.BlockID, error) {
	//tmr := newMilliTimer(syncLaye	Time)
	if r, err := s.DB.GetLayerInputVector(layerID); err == nil {
		return r, nil
	}

	out := <-fetchWithFactory(ctx, newNeighborhoodWorker(ctx, s, 1, inputVectorReqFactory(layerID.Bytes())))
	if out == nil {
		return nil, fmt.Errorf("could not find input vector with any neighbor")
	}

	inputvec := out.([]types.BlockID)

	//tmr.ObserveDuration()

	return inputvec, nil
}

func (s *Syncer) getBlocks(ctx context.Context, blockIds []types.BlockID) error {
	// Generate a random job ID
	jobID := rand.Int31()
	logger := s.WithContext(ctx).WithFields(log.Int32("jobID", jobID))
	ch := make(chan bool, 1)
	foo := func(ctx context.Context, res bool) error {
		logger.Info("get blocks for layer done")
		ch <- res
		return nil
	}

	tmr := newMilliTimer(syncLayerTime)
	if res, err := s.blockQueue.addDependencies(ctx, jobID, blockIds, foo); err != nil {
		return fmt.Errorf("failed adding layer %v blocks to queue %v", jobID, err)
	} else if res == false {
		logger.Info("getBlocks: no missing blocks for layer")
		return nil
	}

	logger.With().Info("wait for blocks", log.Int("num_blocks", len(blockIds)))
	select {
	case <-s.exit:
		return fmt.Errorf("received interrupt")
	case result := <-ch:
		if !result {
			return nil
		}
	}

	tmr.ObserveDuration()
	return nil
}

// GetBlocks fetches list of blocks from peers
func (s *Syncer) GetBlocks(ctx context.Context, blockIds []types.BlockID) error {
	return s.getBlocks(ctx, blockIds)
}

func (s *Syncer) fastValidation(block *types.Block) error {
	// block eligibility
	if eligible, err := s.BlockSignedAndEligible(block); err != nil || !eligible {
		return fmt.Errorf("block eligibility check failed - err: %v", err)
	}

	// validate unique tx atx
	if err := validateUniqueTxAtx(block); err != nil {
		return err
	}
	return nil

}

func validateUniqueTxAtx(b *types.Block) error {
	// check for duplicate tx id
	mt := make(map[types.TransactionID]struct{}, len(b.TxIDs))
	for _, tx := range b.TxIDs {
		if _, exist := mt[tx]; exist {
			return errDupTx
		}
		mt[tx] = struct{}{}
	}

	// check for duplicate atx id
	if b.ActiveSet != nil {
		ma := make(map[types.ATXID]struct{}, len(*b.ActiveSet))
		for _, atx := range *b.ActiveSet {
			if _, exist := ma[atx]; exist {
				return errDupAtx
			}
			ma[atx] = struct{}{}
		}
	}

	return nil
}

func (s *Syncer) fetchRefBlock(ctx context.Context, block *types.Block) error {
	if block.RefBlock == nil {
		return fmt.Errorf("called fetch ref block with nil ref block %v", block.ID())
	}
	_, err := s.GetBlock(*block.RefBlock)
	if err != nil {
		s.With().Info("fetching block", *block.RefBlock)
		fetched := s.fetchBlock(ctx, *block.RefBlock)
		if !fetched {
			return fmt.Errorf("failed to fetch ref block %v", *block.RefBlock)
		}
	}
	return nil
}

func (s *Syncer) fetchAllReferencedAtxs(ctx context.Context, blk *types.Block) error {
	s.WithContext(ctx).With().Debug("syncer fetching all atxs referenced by block", blk.ID())

	// As block with empty or Golden ATXID is considered syntactically invalid, explicit check is not needed here.
	atxs := []types.ATXID{blk.ATXID}

	if blk.ActiveSet != nil {
		if len(*blk.ActiveSet) > 0 {
			atxs = append(atxs, *blk.ActiveSet...)
		} else {
			return errZeroActiveSet
		}
	} else {
		if blk.RefBlock == nil {
			return errNoActiveSet
		}
	}
	_, err := s.atxQueue.HandleAtxs(ctx, atxs)
	s.WithContext(ctx).With().Debug("syncer done fetching all atxs referenced by block", blk.ID(), log.Err(err))
	return err
}

func (s *Syncer) fetchBlockDataForValidation(ctx context.Context, blk *types.Block) error {
	if blk.RefBlock != nil {
		err := s.fetchRefBlock(ctx, blk)
		if err != nil {
			return err
		}
	}
	return s.fetchAllReferencedAtxs(ctx, blk)
}

func (s *Syncer) blockSyntacticValidation(ctx context.Context, block *types.Block) ([]*types.Transaction, []*types.ActivationTx, error) {
	// A block whose associated ATX is the GoldenATXID or the EmptyATXID - either of these - is syntactically invalid.
	if block.ATXID == *types.EmptyATXID || block.ATXID == s.GoldenATXID {
		return nil, nil, errInvalidATXID
	}

	// validate unique tx atx
	if err := s.fetchBlockDataForValidation(ctx, block); err != nil {
		return nil, nil, err
	}

	if err := s.fastValidation(block); err != nil {
		return nil, nil, err
	}

	// data availability
	txs, atxs, err := s.dataAvailability(ctx, block)
	if err != nil {
		return nil, nil, fmt.Errorf("DataAvailabilty failed for block %v err: %v", block.ID(), err)
	}

	// validate block's view
	valid := s.validateBlockView(ctx, block)
	if valid == false {
		return nil, nil, fmt.Errorf("block %v not syntacticly valid", block.ID())
	}

	return txs, atxs, nil
}

func combineBlockDiffs(blk *types.Block) []types.BlockID {
	return append(append(blk.ForDiff, blk.AgainstDiff...), blk.NeutralDiff...)
}

func (s *Syncer) validateBlockView(ctx context.Context, blk *types.Block) bool {
	ch := make(chan bool, 1)
	defer close(ch)
	foo := func(ctx context.Context, res bool) error {
		s.WithContext(ctx).With().Info("view validated",
			blk.ID(),
			log.Bool("result", res),
			blk.LayerIndex)
		ch <- res
		return nil
	}
	if res, err := s.blockQueue.addDependencies(ctx, blk.ID(), combineBlockDiffs(blk), foo); err != nil {
		s.Error(fmt.Sprintf("block %v not syntactically valid", blk.ID()), err)
		return false
	} else if res == false {
		s.WithContext(ctx).With().Debug("no missing blocks in view",
			blk.ID(),
			blk.LayerIndex)
		return true
	}

	return <-ch
}

func (s *Syncer) fetchAtx(ctx context.Context, ID types.ATXID) (*types.ActivationTx, error) {
	s.WithContext(ctx).With().Debug("attempting to fetch atx", ID)
	atxs, err := s.atxQueue.HandleAtxs(ctx, []types.ATXID{ID})
	if err != nil {
		return nil, err
	}
	if len(atxs) == 0 {
		return nil, fmt.Errorf("ATX %v not fetched", ID.ShortString())
	}
	s.WithContext(ctx).With().Debug("done fetching atx", ID)
	return atxs[0], nil
}

// FetchAtx fetches an ATX from remote peer
func (s *Syncer) FetchAtx(ctx context.Context, ID types.ATXID) error {
	_, e := s.fetchAtx(ctx, ID)
	return e
}

// FetchAtxReferences fetches positioning and prev atxs from peers if they are not found in db
func (s *Syncer) FetchAtxReferences(ctx context.Context, atx *types.ActivationTx) error {
	logger := s.WithContext(ctx).WithFields(atx.ID())
	if atx.PositioningATX != s.GoldenATXID {
		logger.With().Info("going to fetch pos atx", log.FieldNamed("pos_atx_id", atx.PositioningATX))
		_, err := s.fetchAtx(ctx, atx.PositioningATX)
		if err != nil {
			return err
		}
	}

	if atx.PrevATXID != *types.EmptyATXID {
		logger.With().Info("going to fetch prev atx", log.FieldNamed("prev_atx_id", atx.PrevATXID))
		_, err := s.fetchAtx(ctx, atx.PrevATXID)
		if err != nil {
			return err
		}
	}
	logger.Info("done fetching references for atx")

	return nil
}

func (s *Syncer) fetchBlock(ctx context.Context, ID types.BlockID) bool {
	ch := make(chan bool, 1)
	defer close(ch)
	foo := func(ctx context.Context, res bool) error {
		s.WithContext(ctx).With().Info("single block fetched",
			ID,
			log.Bool("result", res))
		ch <- res
		return nil
	}
	id := types.CalcHash32(append(ID.Bytes(), []byte(strconv.Itoa(rand.Int()))...))
	if res, err := s.blockQueue.addDependencies(ctx, id, []types.BlockID{ID}, foo); err != nil {
		s.Error(fmt.Sprintf("block %v not syntactically valid", ID), err)
		return false
	} else if res == false {
		// block already found
		return true
	}

	return <-ch
}

// FetchBlock fetches a single block from peers
func (s *Syncer) FetchBlock(ctx context.Context, ID types.BlockID) error {
	if !s.fetchBlock(ctx, ID) {
		return fmt.Errorf("error in FetchBlock")
	}
	return nil
}

func (s *Syncer) dataAvailability(ctx context.Context, blk *types.Block) ([]*types.Transaction, []*types.ActivationTx, error) {
	s.WithContext(ctx).With().Debug("checking data availability for block", blk.ID())
	wg := sync.WaitGroup{}
	wg.Add(1)
	var txres []*types.Transaction
	var txerr error

	if len(blk.TxIDs) > 0 {
		txres, txerr = s.txQueue.HandleTxs(ctx, blk.TxIDs)
	}

	var atxres []*types.ActivationTx

	if txerr != nil {
		return nil, nil, fmt.Errorf("failed fetching block %v transactions %v", blk.ID(), txerr)
	}

	s.WithContext(ctx).With().Debug("done checking data availability for block", blk.ID())
	return txres, atxres, nil
}

// GetTxs fetches txs from peers if necessary
func (s *Syncer) GetTxs(ctx context.Context, IDs []types.TransactionID) error {
	_, err := s.txQueue.HandleTxs(ctx, IDs)
	return err
}

func (s *Syncer) fetchLayerBlockIds(ctx context.Context, m map[types.Hash32][]p2ppeers.Peer, lyr types.LayerID) ([]types.BlockID, error) {
	logger := s.WithContext(ctx).WithFields(lyr)
	// send request to different users according to returned hashes
	idSet := make(map[types.BlockID]struct{}, s.LayerSize)
	ids := make([]types.BlockID, 0, s.LayerSize)
	for h, peers := range m {
		logger := logger.WithFields(h)
		logger.With().Debug("attempting to fetch block ids from peers", log.Int("num_peers", len(peers)))
	NextHash:
		for _, peer := range peers {
			logger := logger.WithFields(log.FieldNamed("peer_id", peer))
			logger.Debug("send request")
			ch, err := layerIdsReqFactory(lyr)(ctx, s, peer)
			if err != nil {
				return nil, err
			}

			timeout := time.After(s.Configuration.RequestTimeout)
			select {
			case <-s.GetExit():
				logger.Debug("worker received interrupt")
				return nil, fmt.Errorf("received interrupt")
			case <-timeout:
				logger.Error("layer ids request timed out")
				continue
			case v := <-ch:
				if v != nil {
					blockIds := v.([]types.BlockID)
					logger.With().Debug("peer responded to layer ids request",
						log.Int("num_ids", len(blockIds)))
					// peer returned set with bad hash ask next peer
					if res := types.CalcBlocksHash32(blockIds, nil); h != res {
						logger.Warning("layer ids hash does not match request")
					}

					for _, bid := range blockIds {
						if _, exists := idSet[bid]; !exists {
							logger.With().Debug("got data from peer on new block",
								log.FieldNamed("new_block_id", bid))
							idSet[bid] = struct{}{}
							ids = append(ids, bid)
						}
					}

					// fetch next block hash
					break NextHash
				} else {
					logger.Debug("got nil response from peer")
				}
			}
		}
	}

	logger.With().Info("successfully fetched block ids for layer", log.Int("count", len(ids)))
	return ids, nil
}

func (s *Syncer) fetchEpochAtxs(ctx context.Context, m map[types.Hash32][]p2ppeers.Peer, epoch types.EpochID) ([]types.ATXID, error) {
	logger := s.WithContext(ctx)
	// send request to different users according to returned hashes
	idSet := make(map[types.ATXID]struct{}, s.LayerSize)
	ids := make([]types.ATXID, 0, s.LayerSize)
	for h, peers := range m {
	NextHash:
		for _, peer := range peers {
			logger.With().Debug("send request", log.String("peer", peer.String()))
			ch, err := getEpochAtxIds(ctx, epoch, s, peer)
			if err != nil {
				return nil, err
			}

			timeout := time.After(s.Configuration.RequestTimeout)
			select {
			case <-s.GetExit():
				logger.Debug("worker received interrupt")
				return nil, fmt.Errorf("received interrupt")
			case <-timeout:
				logger.With().Error("epoch atxs request timed out", log.String("peer", peer.String()))
				continue
			case v := <-ch:
				if v != nil {
					logger.With().Debug("peer responded to epoch atx ids request",
						log.String("peer", peer.String()))
					// peer returned set with bad hash ask next peer
					res := types.CalcATXIdsHash32(v.([]types.ATXID), nil)

					if h != res {
						logger.With().Warning("epoch atx ids hash does not match request",
							log.String("peer", peer.String()))
					}

					for _, bid := range v.([]types.ATXID) {
						if _, exists := idSet[bid]; !exists {
							idSet[bid] = struct{}{}
							ids = append(ids, bid)
						}
					}
					// fetch for next hash
					break NextHash
				}
			}
		}
	}

	if len(ids) == 0 {
		logger.Info("could not get atx ids from any peer")
	}

	return ids, nil
}

type peerHashPair struct {
	peer p2ppeers.Peer
	hash types.Hash32
}

func (s *Syncer) fetchLayerHashes(ctx context.Context, lyr types.LayerID) (map[types.Hash32][]p2ppeers.Peer, error) {
	// get layer hash from each peer
	wrk := newPeersWorker(ctx, s, s.GetPeers(), &sync.Once{}, hashReqFactory(lyr))
	go wrk.Work(ctx)
	m := make(map[types.Hash32][]p2ppeers.Peer)
	layerHasBlocks := false
	for out := range wrk.output {
		pair, ok := out.(*peerHashPair)
		if pair != nil && ok { // do nothing on close channel
			if pair.hash != emptyLayer {
				layerHasBlocks = true
				m[pair.hash] = append(m[pair.hash], pair.peer)
			}
		}
	}

	if !layerHasBlocks {
		s.With().Info("layer has no blocks", lyr)
		return nil, errNoBlocksInLayer
	}

	if len(m) == 0 {
		return nil, errors.New("could not get layer hashes from any peer")
	}
	s.With().Info("layer has blocks", lyr)
	return m, nil
}

func (s *Syncer) fetchEpochAtxHashes(ctx context.Context, ep types.EpochID) (map[types.Hash32][]p2ppeers.Peer, error) {
	// get layer hash from each peer
	wrk := newPeersWorker(ctx, s, s.GetPeers(), &sync.Once{}, atxHashReqFactory(ep))
	go wrk.Work(ctx)
	m := make(map[types.Hash32][]p2ppeers.Peer)
	layerHasBlocks := false
	for out := range wrk.output {
		pair, ok := out.(*peerHashPair)
		if pair != nil && ok { // do nothing on close channel
			if pair.hash != emptyLayer {
				layerHasBlocks = true
				m[pair.hash] = append(m[pair.hash], pair.peer)
			}

		}
	}

	if !layerHasBlocks {
		s.With().Info("epoch has no atxs", ep)
		return nil, errNoBlocksInLayer
	}

	if len(m) == 0 {
		return nil, errors.New("could not get epoch hashes from any peer")
	}
	s.With().Info("epoch has atxs", ep)
	return m, nil
}

func fetchWithFactory(ctx context.Context, wrk worker) chan interface{} {
	// each worker goroutine tries to fetch a block iteratively from each peer
	go wrk.Work(ctx)
	for i := 0; int32(i) < atomic.LoadInt32(wrk.workCount)-1; i++ {
		go wrk.Clone().Work(ctx)
	}
	return wrk.output
}

// FetchPoetProof fetches a poet proof from network peers
func (s *Syncer) FetchPoetProof(ctx context.Context, poetProofRef []byte) error {
	if !s.poetDb.HasProof(poetProofRef) {
		out := <-fetchWithFactory(ctx, newNeighborhoodWorker(ctx, s, 1, poetReqFactory(poetProofRef)))
		if out == nil {
			return fmt.Errorf("could not find PoET proof with any neighbor")
		}
		proofMessage := out.(types.PoetProofMessage)
		err := s.poetDb.ValidateAndStore(&proofMessage)
		if err != nil {
			return err
		}
	}
	return nil
}

// GetPoetProof fetches a poet proof from network peers
func (s *Syncer) GetPoetProof(ctx context.Context, hash types.Hash32) error {
	poetProofRef := hash.Bytes()
	if !s.poetDb.HasProof(poetProofRef) {
		out := <-fetchWithFactory(ctx, newNeighborhoodWorker(ctx, s, 1, poetReqFactory(poetProofRef)))
		if out == nil {
			return fmt.Errorf("could not find PoET proof with any neighbor")
		}
		proofMessage := out.(types.PoetProofMessage)
		err := s.poetDb.ValidateAndStore(&proofMessage)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Syncer) atxCheckLocal(ctx context.Context, atxIds []types.Hash32) (map[types.Hash32]item, map[types.Hash32]item, []types.Hash32) {
	// look in pool
	unprocessedItems := make(map[types.Hash32]item, len(atxIds))
	missingInPool := make([]types.ATXID, 0, len(atxIds))
	for _, t := range atxIds {
		id := types.ATXID(t)
		if x, err := s.atxDb.GetFullAtx(id); err == nil {
			atx := x
			s.WithContext(ctx).With().Debug("found atx in atx db", id)
			unprocessedItems[id.Hash32()] = atx
		} else {
			s.WithContext(ctx).With().Debug("atx not in atx pool", id)
			missingInPool = append(missingInPool, id)
		}
	}
	// look in db
	dbAtxs, missing := s.GetATXs(ctx, missingInPool)

	dbItems := make(map[types.Hash32]item, len(dbAtxs))
	for i, k := range dbAtxs {
		dbItems[i.Hash32()] = k
	}

	missingItems := make([]types.Hash32, 0, len(missing))
	for _, i := range missing {
		missingItems = append(missingItems, i.Hash32())
	}

	return unprocessedItems, dbItems, missingItems
}

func (s *Syncer) txCheckLocal(ctx context.Context, txIds []types.Hash32) (map[types.Hash32]item, map[types.Hash32]item, []types.Hash32) {
	// look in pool
	unprocessedItems := make(map[types.Hash32]item)
	missingInPool := make([]types.TransactionID, 0)
	for _, t := range txIds {
		id := types.TransactionID(t)
		if tx, err := s.txpool.Get(id); err == nil {
			s.WithContext(ctx).With().Debug("found tx in tx pool", id)
			unprocessedItems[id.Hash32()] = tx
		} else {
			s.WithContext(ctx).With().Debug("tx not in tx pool", id)
			missingInPool = append(missingInPool, id)
		}
	}
	// look in db
	dbTxs, missing := s.GetTransactions(missingInPool)

	dbItems := make(map[types.Hash32]item, len(dbTxs))
	for _, k := range dbTxs {
		dbItems[k.Hash32()] = k
	}

	missingItems := make([]types.Hash32, 0, len(missing))
	for i := range missing {
		missingItems = append(missingItems, i.Hash32())
	}

	return unprocessedItems, dbItems, missingItems
}

func (s *Syncer) blockCheckLocal(ctx context.Context, blockIds []types.Hash32) (map[types.Hash32]item, map[types.Hash32]item, []types.Hash32) {
	// look in pool
	dbItems := make(map[types.Hash32]item)
	for _, id := range blockIds {
		res, err := s.GetBlock(types.BlockID(id.ToHash20()))
		if err != nil {
			s.WithContext(ctx).With().Debug("get block failed", log.String("id", id.ShortString()))
			continue
		}
		dbItems[id] = res
	}

	return nil, dbItems, nil
}

func (s *Syncer) getAndValidateLayer(id types.LayerID) error {
	s.validatingLayerMutex.Lock()
	s.validatingLayer = id
	defer func() {
		s.validatingLayer = validatingLayerNone
		s.validatingLayerMutex.Unlock()
	}()

	lyr, err := s.GetLayer(id)
	if err != nil {
		return err
	}

	s.Log.With().Info("getAndValidateLayer",
		id.Field(),
		log.String("blocks", fmt.Sprint(types.BlockIDs(lyr.Blocks()))))

	s.ValidateLayer(lyr) // wait for layer validation
	return nil
}

func (s *Syncer) getValidatingLayer() types.LayerID {
	return s.validatingLayer
}
