package sync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/layerfetcher"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	p2pconf "github.com/spacemeshos/go-spacemesh/p2p/config"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
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

// LayerFetch definer interface for fetching data for layers
type LayerFetch interface {
	PollLayer(ctx context.Context, layer types.LayerID) chan layerfetcher.LayerPromiseResult
	GetAtxs(ctx context.Context, IDs []types.ATXID) error
	GetEpochATXs(ctx context.Context, id types.EpochID) error
	GetBlocks(ctx context.Context, IDs []types.BlockID) error
	FetchBlock(ctx context.Context, ID types.BlockID) error
	GetPoetProof(ctx context.Context, id types.Hash32) error
	GetInputVector(id types.LayerID) error
	Start()
	Close()
}

type peers interface {
	GetPeers() []p2ppeers.Peer
	Close()
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
	if *s == 2 {
		return "done"
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

	fetcher LayerFetch
}

// NewSync fires a sync every sm.SyncInterval or on force space from outside
func NewSync(ctx context.Context, srv service.Service, layers *mesh.Mesh, txpool txMemPool, atxDB atxDB, bv blockEligibilityValidator, poetdb poetDb, conf Configuration, clock ticker, layerFetch LayerFetch, logger log.Log) *Syncer {

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
		fetcher:                   layerFetch,
	}

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
	s.WithContext(ctx).Info("sync state w: %v, g:%v, current: %v latest: %v, verified: %v",
		s.weaklySynced(s.GetCurrentLayer()),
		s.getGossipBufferingStatus(),
		s.GetCurrentLayer(),
		s.LatestLayer(),
		s.ProcessedLayer())
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
		logger.Debug("start syncer")
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
			go s.synchronize(ctx)
		case <-s.syncTimer.C:
			go s.synchronize(ctx)
		}
	}
}

func (s *Syncer) synchronize(ctx context.Context) {
	logger := s.WithContext(ctx)

	// only one concurrent synchronize
	if s.syncLock.TryLock() == false {
		logger.Info("sync is already running, giving up")
		return
	}
	// release synchronize lock
	defer s.syncLock.Unlock()

	// in a steady state (ProcessedLayer() is GetCurrentLayer() or GetCurrentLayer()-1),
	// we don't get into this loop at all.
	for s.GetCurrentLayer() > 0 && s.ProcessedLayer() < s.GetCurrentLayer()-1 {
		curr := s.GetCurrentLayer()
		latest := s.LatestLayer()
		verified := s.ProcessedLayer()
		logger.Info("in sync loop, current: %v, latest: %v, verified: %v, gossip: %v", curr, latest, verified, s.getGossipBufferingStatus())

		if len(s.net.GetPeers()) == 0 {
			logger.Error("cannot sync - no peers")
			return
		}

		logger.Info("trying to sync to layer %v", curr-1)
		if err := s.syncToLayer(ctx, curr-1, true); err != nil {
			return
		}
	}
	curr := s.GetCurrentLayer()
	verified := s.ProcessedLayer()
	if verified >= curr-1 {
		if s.getGossipBufferingStatus() == pending {
			logger.Info("weakly synced, current: %v, latest: %v, verified: %v", curr, s.LatestLayer(), verified)
			// signal hare to start listening to gossip
			s.setGossipBufferingStatus(inProgress2)
		}
		logger.Info("trying to sync to current layer, current: %v, latest: %v, verified: %v", curr, s.LatestLayer(), verified)
		s.syncToLayer(ctx, curr, s.shouldValidateCurrentLayer())
	}

	s.WithContext(ctx).Info("node is synced, current: %v, latest: %v, verified: %v",
		s.GetCurrentLayer(), s.LatestLayer(), s.ProcessedLayer())
	// fully-synced, make sure we listen to p2p
	s.setGossipBufferingStatus(done)
}

func (s *Syncer) shouldValidateCurrentLayer() bool {
	latest := s.LatestLayer()
	curr := s.GetCurrentLayer()
	return latest == curr && time.Now().Sub(s.LayerToTime(latest)) > s.ValidationDelta
}

func (s *Syncer) syncToLayer(ctx context.Context, toLayer types.LayerID, validate bool) error {
	s.WithContext(ctx).Info("syncing to layer %v", toLayer)
	start := s.ProcessedLayer()
	if start == 0 {
		// s.ProcessedLayer() == 0 can mean either layer 0 is processed or not. this only matters for layer 0
		start = 0
	} else {
		start++
	}
	for i := start; i <= toLayer; i++ {
		// sync the layer data
		var layer *types.Layer
		var err error
		if layer, err = s.syncOneLayer(ctx, i); err != nil {
			s.WithContext(ctx).With().Error("error attempting to sync to layer %v", log.Err(err))
			return err
		}

		if len(layer.Blocks()) == 0 {
			s.WithContext(ctx).Info("setting layer %v to zero-block", i)
			if err := s.SetZeroBlockLayer(i); err != nil {
				s.WithContext(ctx).With().Error("failed to set zero-block for layer %v", i, log.Err(err))
				return err
			}
		}

		// validate layer
		if validate {
			s.WithContext(ctx).Info("validating layer %v", layer.Index())
			if err := s.validateOneLayer(ctx, layer); err != nil {
				return err
			}
		}
	}
	s.WithContext(ctx).Info("successfully synced to layer %v", toLayer)
	return nil
}

func (s *Syncer) syncOneLayer(ctx context.Context, layerID types.LayerID) (*types.Layer, error) {
	logger := s.WithContext(ctx)
	logger.Info("syncing single layer %v, current %v", layerID, s.GetCurrentLayer())

	if s.isClosed() {
		return nil, errors.New("shutdown")
	}

	layer, err := s.getLayerFromNeighbors(ctx, layerID)
	if err != nil {
		logger.With().Info("could not get layer from neighbors", layerID, log.Err(err))
		return nil, err
	}

	s.syncAtxs(ctx, layerID)
	return layer, nil
}

func (s *Syncer) validateOneLayer(ctx context.Context, layer *types.Layer) error {
	if s.isClosed() {
		return errors.New("shutdown")
	}
	s.validatingLayerMutex.Lock()
	s.validatingLayer = layer.Index()
	defer func() {
		s.validatingLayer = validatingLayerNone
		s.validatingLayerMutex.Unlock()
	}()
	s.WithContext(ctx).With().Info("validating layer",
		layer.Index().Field(),
		log.String("blocks", fmt.Sprint(types.BlockIDs(layer.Blocks()))))
	s.ValidateLayer(layer)
	return nil
}

func (s *Syncer) syncAtxs(ctx context.Context, currentSyncLayer types.LayerID) {
	if currentSyncLayer.GetEpoch() == 0 {
		s.With().Info("skipping ATX sync in epoch 0")
		return
	}
	lastLayerOfEpoch := (currentSyncLayer.GetEpoch() + 1).FirstLayer() - 1
	if currentSyncLayer == lastLayerOfEpoch {
		ctx = log.WithNewRequestID(ctx, currentSyncLayer.GetEpoch())
		if err := s.fetcher.GetEpochATXs(ctx, currentSyncLayer.GetEpoch()); err != nil {
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

func (s *Syncer) getLayerFromNeighbors(ctx context.Context, currentSyncLayer types.LayerID) (*types.Layer, error) {
	ch := s.fetcher.PollLayer(ctx, currentSyncLayer)
	res := <-ch
	if res.Err != nil {
		if res.Err == layerfetcher.ErrZeroLayer {
			return types.NewLayer(currentSyncLayer), nil
		}
		return nil, res.Err
	}
	return s.GetLayer(currentSyncLayer)
}

// GetBlocks fetches list of blocks from peers
func (s *Syncer) GetBlocks(ctx context.Context, blockIds []types.BlockID) error {
	return s.fetcher.GetBlocks(ctx, blockIds)
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
		return s.fetcher.FetchBlock(ctx, *block.RefBlock)
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
	return s.fetcher.GetAtxs(ctx, atxs)
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

// FetchBlock fetches a single block from peers
func (s *Syncer) FetchBlock(ctx context.Context, ID types.BlockID) error {
	if err := s.fetcher.FetchBlock(ctx, ID); err != nil {
		return err
	}
	return nil
}

// GetPoetProof fetches a poet proof from network peers
func (s *Syncer) GetPoetProof(ctx context.Context, hash types.Hash32) error {
	return s.fetcher.GetPoetProof(ctx, hash)
}

// GetInputVector fetches a inputvector for layer from network peers
func (s *Syncer) GetInputVector(id types.LayerID) error {
	return s.fetcher.GetInputVector(id)
}

func (s *Syncer) getValidatingLayer() types.LayerID {
	return s.validatingLayer
}
