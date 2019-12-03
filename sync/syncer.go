package sync

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	p2pconf "github.com/spacemeshos/go-spacemesh/p2p/config"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"sync"
	"sync/atomic"
	"time"
)

type ForBlockInView func(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) (bool, error)) error

type TxMemPool interface {
	Get(id types.TransactionId) (*types.Transaction, error)
	Put(id types.TransactionId, item *types.Transaction)
}

type AtxMemPool interface {
	Get(id types.AtxId) (*types.ActivationTx, error)
	Put(atx *types.ActivationTx)
}

type PoetDb interface {
	HasProof(proofRef []byte) bool
	ValidateAndStore(proofMessage *types.PoetProofMessage) error
	GetProofMessage(proofRef []byte) ([]byte, error)
}

type BlockValidator interface {
	BlockSignedAndEligible(block *types.Block) (bool, error)
}

type EligibilityValidator interface {
	BlockSignedAndEligible(block *types.Block) (bool, error)
}

type TxValidator interface {
	AddressExists(addr types.Address) bool
}

type TickProvider interface {
	Subscribe() timesync.LayerTimer
	Unsubscribe(timer timesync.LayerTimer)
	GetCurrentLayer() types.LayerID
}

type Configuration struct {
	LayersPerEpoch uint16
	Concurrency    int //number of workers for sync method
	LayerSize      int
	RequestTimeout time.Duration
	AtxsLimit      int
	Hdist          int
}

type LayerValidator interface {
	ValidatedLayer() types.LayerID
	ValidateLayer(lyr *types.Layer)
}

const (
	ValidationCacheSize = 1000
)

var (
	errDupTx       = errors.New("duplicate TransactionId in block")
	errDupAtx      = errors.New("duplicate AtxId in block")
	errTooManyAtxs = errors.New("too many atxs in blocks")
)

type Status int

const (
	Pending    Status = 0
	InProgress Status = 1
	Done       Status = 2
)

type LayerProvider interface {
	GetLayer(index types.LayerID) (*types.Layer, error)
}

type Syncer struct {
	Configuration
	log.Log
	*mesh.Mesh
	EligibilityValidator
	*workerInfra
	TickProvider
	poetDb            PoetDb
	txpool            TxMemPool
	atxpool           AtxMemPool
	lValidator        LayerValidator
	currentLayer      types.LayerID
	SyncLock          uint32
	startLock         uint32
	forceSync         chan bool
	LayerCh           timesync.LayerTimer
	exit              chan struct{}
	currentLayerMutex sync.RWMutex
	syncRoutineWg     sync.WaitGroup
	gossipLock        sync.RWMutex
	gossipSynced      Status

	//todo fetch server
	blockQueue *blockQueue
	txQueue    *txQueue
	atxQueue   *atxQueue
}

func (s *Syncer) ForceSync() {
	s.forceSync <- true
}

func (s *Syncer) Close() {
	s.Info("Closing syncer")
	close(s.exit)
	close(s.forceSync)
	s.Peers.Close()
	// TODO: broadly implement a better mechanism for shutdown
	time.Sleep(5 * time.Millisecond) // "ensures" no more sync routines can be created, ok for now
	s.syncRoutineWg.Wait()           // must be called after we ensure no more sync routines can be created
	s.blockQueue.Close()
	s.atxQueue.Close()
	s.txQueue.Close()
	s.MessageServer.Close()
}

const (
	IDLE         uint32             = 0
	RUNNING      uint32             = 1
	BLOCK        server.MessageType = 1
	LAYER_HASH   server.MessageType = 2
	LAYER_IDS    server.MessageType = 3
	TX           server.MessageType = 4
	ATX          server.MessageType = 5
	POET         server.MessageType = 6
	syncProtocol                    = "/sync/1.0/"
)

func (s *Syncer) weaklySynced() bool {
	// equivalent to s.LatestLayer() >= s.lastTickedLayer()-1
	// means we have data from the previous layer
	return s.LatestLayer()+1 >= s.lastTickedLayer()
}

func (s *Syncer) getGossipBufferingStatus() Status {
	s.gossipLock.RLock()
	b := s.gossipSynced
	s.gossipLock.RUnlock()
	return b
}

//api for other modules to check if they should listen to gossip
func (s *Syncer) ListenToGossip() bool {
	return s.getGossipBufferingStatus() != Pending
}

func (s *Syncer) setGossipBufferingStatus(b Status) {
	s.gossipLock.Lock()
	s.gossipSynced = b
	s.gossipLock.Unlock()
}

func (s *Syncer) IsSynced() bool {
	s.Log.Info("latest: %v, maxSynced %v", s.LatestLayer(), s.lastTickedLayer())
	return s.weaklySynced() && s.getGossipBufferingStatus() == Done
}

func (s *Syncer) Start() {
	if atomic.CompareAndSwapUint32(&s.startLock, 0, 1) {
		go s.run()
		s.forceSync <- true
		return
	}
}

func (s *Syncer) getSyncRoutine() func() {
	return func() {
		if atomic.CompareAndSwapUint32(&s.SyncLock, IDLE, RUNNING) {
			s.syncRoutineWg.Add(1)
			s.Synchronise()
			atomic.StoreUint32(&s.SyncLock, IDLE)
		}
	}
}

//fires a sync every sm.syncInterval or on force space from outside
func (s *Syncer) run() {
	syncRoutine := s.getSyncRoutine()
	for {
		select {
		case <-s.exit:
			s.Debug("Work stopped")
			return
		case <-s.forceSync:
			go syncRoutine()
		case layer := <-s.LayerCh:
			s.currentLayerMutex.Lock()
			s.currentLayer = layer
			s.currentLayerMutex.Unlock()
			s.Debug("sync got tick for layer %v", layer)
			go syncRoutine()
		}
	}
}

//fires a sync every sm.syncInterval or on force space from outside
func NewSync(srv service.Service, layers *mesh.Mesh, txpool TxMemPool, atxpool AtxMemPool, bv BlockValidator, poetdb PoetDb, conf Configuration, clock TickProvider, logger log.Log) *Syncer {

	srvr := &workerInfra{
		RequestTimeout: conf.RequestTimeout,
		MessageServer:  server.NewMsgServer(srv.(server.Service), syncProtocol, conf.RequestTimeout, make(chan service.DirectMessage, p2pconf.ConfigValues.BufferSize), logger),
		Peers:          p2p.NewPeers(srv, logger.WithName("peers")),
	}

	s := &Syncer{
		EligibilityValidator: bv,
		Configuration:        conf,
		Log:                  logger,
		Mesh:                 layers,
		workerInfra:          srvr,
		TickProvider:         clock,
		lValidator:           layers,
		SyncLock:             0,
		poetDb:               poetdb,
		txpool:               txpool,
		atxpool:              atxpool,
		startLock:            0,
		forceSync:            make(chan bool),
		currentLayer:         clock.GetCurrentLayer(),
		LayerCh:              clock.Subscribe(),
		exit:                 make(chan struct{}),
		gossipSynced:         Pending,
	}

	s.blockQueue = NewValidationQueue(srvr, s.Configuration, s, s.blockCheckLocal, logger.WithName("validQ"))
	s.txQueue = NewTxQueue(s)
	s.atxQueue = NewAtxQueue(s, s.FetchPoetProof)

	srvr.RegisterBytesMsgHandler(LAYER_HASH, newLayerHashRequestHandler(layers, logger))
	srvr.RegisterBytesMsgHandler(BLOCK, newBlockRequestHandler(layers, logger))
	srvr.RegisterBytesMsgHandler(LAYER_IDS, newLayerBlockIdsRequestHandler(layers, logger))
	srvr.RegisterBytesMsgHandler(TX, newTxsRequestHandler(s, logger))
	srvr.RegisterBytesMsgHandler(ATX, newATxsRequestHandler(s, logger))
	srvr.RegisterBytesMsgHandler(POET, newPoetRequestHandler(s, logger))

	return s
}

func (s *Syncer) lastTickedLayer() types.LayerID {
	s.currentLayerMutex.RLock()
	curr := s.currentLayer
	s.currentLayerMutex.RUnlock()
	return curr
}

func (s *Syncer) Synchronise() {
	defer s.syncRoutineWg.Done()
	s.Info("start synchronize")
	if s.lastTickedLayer() <= 1 { // skip validation for first layer
		s.With().Info("Not syncing in layer <= 1", log.LayerId(uint64(s.lastTickedLayer())))
		s.setGossipBufferingStatus(Done) // fully-synced, make sure we listen to p2p
		return
	}

	currentSyncLayer := s.lValidator.ValidatedLayer() + 1
	if currentSyncLayer == s.lastTickedLayer() { // only validate if current < lastTicked
		s.With().Info("Already synced for layer", log.Uint64("current_sync_layer", uint64(currentSyncLayer)))
		s.setGossipBufferingStatus(Done) // fully-synced, make sure we listen to p2p
		return
	}

	if s.weaklySynced() { // we have all the data of the prev layers so we can simply validate
		s.With().Info("Node is synced. Going to validate layer", log.LayerId(uint64(currentSyncLayer)))

		lyr, err := s.GetLayer(currentSyncLayer)
		if err != nil {
			s.Panic("failed getting layer even though we are weakly-synced currentLayer=%v lastTicked=%v err=%v ", currentSyncLayer, s.lastTickedLayer(), err)
			return
		}
		s.lValidator.ValidateLayer(lyr) // wait for layer validation
		return
	}

	// node is not synced
	s.handleNotSynced(currentSyncLayer)
}

func (s *Syncer) fastValidation(block *types.Block) error {

	if len(block.AtxIds) > s.AtxsLimit {
		s.Error("Too many atxs in block expected<=%v actual=%v", s.AtxsLimit, len(block.AtxIds))
		return errTooManyAtxs
	}

	// block eligibility
	if eligible, err := s.BlockSignedAndEligible(block); err != nil || !eligible {
		return fmt.Errorf("block eligibiliy check failed - err %v", err)
	}

	// validate unique tx atx
	if err := validateUniqueTxAtx(block); err != nil {
		return err
	}
	return nil

}

func (s *Syncer) handleNotSynced(currentSyncLayer types.LayerID) {
	s.Info("Node is out of sync setting gossip-synced to false and starting sync")
	s.setGossipBufferingStatus(Pending) // don't listen to gossip while not synced

	// first, bring all the data of the prev layers
	// Note: lastTicked() is not constant but updates as ticks are received
	for ; currentSyncLayer < s.lastTickedLayer(); currentSyncLayer++ {
		s.With().Info("syncing layer", log.Uint64("current_sync_layer", uint64(currentSyncLayer)), log.Uint64("last_ticked_layer", uint64(s.lastTickedLayer())))
		lyr, err := s.getLayerFromNeighbors(currentSyncLayer)
		if err != nil {
			s.Info("could not get layer %v from neighbors %v", currentSyncLayer, err)
			return
		}

		s.lValidator.ValidateLayer(lyr) // wait for layer validation
	}

	// Now we are somewhere in the layer (begin, middle, end)
	// fetch what you can from the neighbors
	_, err := s.getLayerFromNeighbors(currentSyncLayer)
	if err != nil {
		s.With().Info("could not get last ticked layer from neighbors", log.LayerId(uint64(currentSyncLayer)), log.Err(err))
	}

	// wait for two ticks to ensure we are fully synced before we open gossip or validate the current layer
	err = s.gossipSyncForOneFullLayer(currentSyncLayer)
	if err != nil {
		s.With().Error("Fatal: failed getting layer from db even though we listened to gossip", log.LayerId(uint64(currentSyncLayer)), log.Err(err))
	}
}

// Waits two ticks (while weakly-synced) in order to ensure that we listened to gossip for one full layer
// after that we are assumed to have all the data required for validation so we can validate and open gossip
// opening gossip in weakly-synced transition us to fully-synced
func (s *Syncer) gossipSyncForOneFullLayer(currentSyncLayer types.LayerID) error {
	//listen to gossip
	s.setGossipBufferingStatus(InProgress)
	// subscribe and wait for two ticks
	s.Info("waiting for two ticks while p2p is open")
	ch := s.TickProvider.Subscribe()
	<-ch
	<-ch
	s.TickProvider.Unsubscribe(ch) // unsub, we won't be listening on this ch anymore
	s.Info("done waiting for two ticks while listening to p2p")

	// assumed to be weakly synced here
	// just get the layers and validate

	// get & validate first tick
	lyr, err := s.GetLayer(currentSyncLayer)
	if err != nil {
		return err
	}
	s.lValidator.ValidateLayer(lyr)

	// get & validate second tick
	currentSyncLayer++
	lyr, err = s.GetLayer(currentSyncLayer)
	if err != nil {
		return err
	}
	s.lValidator.ValidateLayer(lyr)
	s.Info("Done waiting for ticks and validation. setting gossip true")

	// fully-synced - set gossip -synced to true
	s.setGossipBufferingStatus(Done)

	return nil
}

func (s *Syncer) getLayerFromNeighbors(currenSyncLayer types.LayerID) (*types.Layer, error) {

	//fetch layer hash from each peer
	m, err := s.fetchLayerHashes(currenSyncLayer)
	if err != nil {
		return nil, err
	}

	//fetch ids for each hash
	blockIds, err := s.fetchLayerBlockIds(m, currenSyncLayer)
	if err != nil {
		return nil, err
	}

	blocksArr, err := s.syncLayer(currenSyncLayer, blockIds)
	if len(blocksArr) == 0 || err != nil {
		return nil, fmt.Errorf("could get blocks for layer  %v", currenSyncLayer)
	}

	return types.NewExistingLayer(types.LayerID(currenSyncLayer), blocksArr), nil
}

func (s *Syncer) syncLayer(layerID types.LayerID, blockIds []types.BlockID) ([]*types.Block, error) {
	ch := make(chan bool, 1)
	foo := func(res bool) error {
		s.Info("layer %v done", layerID)
		ch <- res
		return nil
	}

	if res, err := s.blockQueue.addDependencies(layerID, blockIds, foo); res == false {
		return s.LayerBlocks(layerID)
	} else if err != nil {
		return nil, errors.New(fmt.Sprintf("failed adding layer %v blocks to queue %v", layerID, err))
	}

	s.Info("layer %v wait for blocks", layerID)
	if result := <-ch; !result {
		return nil, fmt.Errorf("could not get all blocks for layer  %v", layerID)
	}

	return s.LayerBlocks(layerID)
}

func validateUniqueTxAtx(b *types.Block) error {
	// check for duplicate tx id
	mt := make(map[types.TransactionId]struct{}, len(b.TxIds))
	for _, tx := range b.TxIds {
		if _, exist := mt[tx]; exist {
			return errDupTx
		}
		mt[tx] = struct{}{}
	}

	// check for duplicate atx id
	ma := make(map[types.AtxId]struct{}, len(b.AtxIds))
	for _, atx := range b.AtxIds {
		if _, exist := ma[atx]; exist {
			return errDupAtx
		}
		ma[atx] = struct{}{}
	}

	return nil
}

func (s *Syncer) blockSyntacticValidation(block *types.Block) ([]*types.Transaction, []*types.ActivationTx, error) {
	// validate unique tx atx
	if err := s.fastValidation(block); err != nil {
		return nil, nil, err
	}

	//data availability
	txs, atxs, err := s.DataAvailability(block)
	if err != nil {
		return nil, nil, fmt.Errorf("DataAvailabilty failed for block %v err: %v", block.Id(), err)
	}

	//validate block's view
	valid := s.validateBlockView(block)
	if valid == false {
		return nil, nil, errors.New(fmt.Sprintf("block %v not syntacticly valid", block.Id()))
	}

	//validate block's votes
	if valid, err := validateVotes(block, s.ForBlockInView, s.Hdist, s.Log); valid == false || err != nil {
		return nil, nil, errors.New(fmt.Sprintf("validate votes failed for block %v, %v", block.Id(), err))
	}

	return txs, atxs, nil
}

func (s *Syncer) validateBlockView(blk *types.Block) bool {
	ch := make(chan bool, 1)
	defer close(ch)
	foo := func(res bool) error {
		ch <- res
		return nil
	}
	if res, err := s.blockQueue.addDependencies(blk.Id(), blk.ViewEdges, foo); res == false {
		return true
	} else if err != nil {
		s.Error(fmt.Sprintf("block %v not syntactically valid", blk.Id()), err)
		return false
	}

	return <-ch
}

func validateVotes(blk *types.Block, forBlockfunc ForBlockInView, depth int, lg log.Log) (bool, error) {
	view := map[types.BlockID]struct{}{}
	for _, blk := range blk.ViewEdges {
		view[blk] = struct{}{}
	}

	vote := map[types.BlockID]struct{}{}
	for _, blk := range blk.BlockVotes {
		vote[blk] = struct{}{}
	}

	traverse := func(b *types.Block) (stop bool, err error) {
		if _, ok := vote[b.Id()]; ok {
			delete(vote, b.Id())
		}
		return len(vote) == 0, nil
	}

	// traverse only through the last Hdist layers
	lowestLayer := blk.LayerIndex - types.LayerID(depth)
	if blk.LayerIndex < types.LayerID(depth) {
		lowestLayer = 0
	}
	err := forBlockfunc(view, lowestLayer, traverse)
	if err == nil && len(vote) > 0 {
		return false, fmt.Errorf("voting on blocks out of view (or out of Hdist), %v", vote)
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

func (s *Syncer) DataAvailability(blk *types.Block) ([]*types.Transaction, []*types.ActivationTx, error) {

	wg := sync.WaitGroup{}
	wg.Add(2)
	var txres []*types.Transaction
	var txerr error

	go func() {
		if len(blk.TxIds) > 0 {
			txres, txerr = s.txQueue.HandleTxs(blk.TxIds)
		}
		wg.Done()
	}()

	var atxres []*types.ActivationTx
	var atxerr error
	go func() {
		if len(blk.AtxIds) > 0 {
			atxres, atxerr = s.atxQueue.HandleAtxs(blk.AtxIds)
		}
		wg.Done()
	}()

	wg.Wait()

	if txerr != nil {
		return nil, nil, fmt.Errorf("failed fetching block %v transactions %v", blk.Id(), txerr)
	}

	if atxerr != nil {
		return nil, nil, fmt.Errorf("failed fetching block %v activation transactions %v", blk.Id(), atxerr)
	}

	s.Info("fetched all block data %v", blk.Id())
	return txres, atxres, nil
}

func (s *Syncer) fetchLayerBlockIds(m map[types.Hash32][]p2p.Peer, lyr types.LayerID) ([]types.BlockID, error) {
	//send request to different users according to returned hashes
	idSet := make(map[types.BlockID]struct{}, s.LayerSize)
	ids := make([]types.BlockID, 0, s.LayerSize)
	for h, peers := range m {
	NextHash:
		for _, peer := range peers {
			s.Info("send request Peer: %v", peer)
			ch, err := LayerIdsReqFactory(lyr)(s, peer)
			if err != nil {
				return nil, err
			}

			timeout := time.After(s.Configuration.RequestTimeout)
			select {
			case <-timeout:
				s.Error("layer ids request to %v timed out", peer)
				continue
			case v := <-ch:
				if v != nil {
					s.Info("Peer: %v responded to layer ids request", peer)
					//peer returned set with bad hash ask next peer
					res, err := types.CalcBlocksHash32(v.([]types.BlockID))
					if err != nil {
						s.With().Error("Peer: got invalid layer ids", log.String("peer", peer.String()), log.Err(err))
						break
					}

					if h != res {
						s.Warning("Peer: %v layer ids hash does not match request", peer)
					}

					for _, bid := range v.([]types.BlockID) {
						if _, exists := idSet[bid]; !exists {
							idSet[bid] = struct{}{}
							ids = append(ids, bid)
						}
					}
					//fetch for next hash
					break NextHash
				}
			}
		}
	}

	if len(ids) == 0 {
		return nil, errors.New("could not get layer ids from any peer")
	}

	return ids, nil
}

type peerHashPair struct {
	peer p2p.Peer
	hash types.Hash32
}

func (s *Syncer) fetchLayerHashes(lyr types.LayerID) (map[types.Hash32][]p2p.Peer, error) {
	// get layer hash from each peer
	wrk, output := NewPeersWorker(s, s.GetPeers(), &sync.Once{}, HashReqFactory(lyr))
	go wrk.Work()
	m := make(map[types.Hash32][]p2p.Peer)
	for out := range output {
		pair, ok := out.(*peerHashPair)
		if pair != nil && ok { //do nothing on close channel
			m[pair.hash] = append(m[pair.hash], pair.peer)
		}
	}
	if len(m) == 0 {
		return nil, errors.New("could not get layer hashes from any peer")
	}
	return m, nil
}

func fetchWithFactory(wrk worker) chan interface{} {
	// each worker goroutine tries to fetch a block iteratively from each peer
	go wrk.Work()
	for i := 0; int32(i) < *wrk.workCount-1; i++ {
		cloneWrk := wrk.Clone()
		go cloneWrk.Work()
	}

	return wrk.output
}

func (s *Syncer) FetchPoetProof(poetProofRef []byte) error {
	if !s.poetDb.HasProof(poetProofRef) {
		out := <-fetchWithFactory(NewNeighborhoodWorker(s, 1, PoetReqFactory(poetProofRef)))
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

func (s *Syncer) atxCheckLocal(atxIds []types.Hash32) (map[types.Hash32]Item, map[types.Hash32]Item, []types.Hash32) {
	//look in pool
	unprocessedItems := make(map[types.Hash32]Item, len(atxIds))
	missingInPool := make([]types.AtxId, 0, len(atxIds))
	for _, t := range atxIds {
		id := types.AtxId(t)
		if x, err := s.atxpool.Get(id); err == nil {
			atx := x
			s.Debug("found atx, %v in atx pool", id.ShortString())
			unprocessedItems[id.Hash32()] = atx
		} else {
			s.Debug("atx %v not in atx pool", id.ShortString())
			missingInPool = append(missingInPool, id)
		}
	}
	//look in db
	dbAtxs, missing := s.GetATXs(missingInPool)

	dbItems := make(map[types.Hash32]Item, len(dbAtxs))
	for i, k := range dbAtxs {
		dbItems[i.Hash32()] = k
	}

	missingItems := make([]types.Hash32, 0, len(missing))
	for _, i := range missing {
		missingItems = append(missingItems, i.Hash32())
	}

	return unprocessedItems, dbItems, missingItems
}

func (s *Syncer) txCheckLocal(txIds []types.Hash32) (map[types.Hash32]Item, map[types.Hash32]Item, []types.Hash32) {
	//look in pool
	unprocessedItems := make(map[types.Hash32]Item)
	missingInPool := make([]types.TransactionId, 0)
	for _, t := range txIds {
		id := types.TransactionId(t)
		if tx, err := s.txpool.Get(id); err == nil {
			s.Debug("found tx, %v in tx pool", hex.EncodeToString(t[:]))
			unprocessedItems[id.Hash32()] = tx
		} else {
			s.Debug("tx %v not in atx pool", hex.EncodeToString(t[:]))
			missingInPool = append(missingInPool, id)
		}
	}
	//look in db
	dbTxs, missing := s.GetTransactions(missingInPool)

	dbItems := make(map[types.Hash32]Item, len(dbTxs))
	for _, k := range dbTxs {
		dbItems[k.Hash32()] = k
	}

	missingItems := make([]types.Hash32, 0, len(missing))
	for i := range missing {
		missingItems = append(missingItems, i.Hash32())
	}

	return unprocessedItems, dbItems, missingItems
}

func (s *Syncer) blockCheckLocal(blockIds []types.Hash32) (map[types.Hash32]Item, map[types.Hash32]Item, []types.Hash32) {
	//look in pool
	dbItems := make(map[types.Hash32]Item)
	for _, id := range blockIds {
		res, err := s.GetBlock(types.BlockID(id))
		if err != nil {
			s.Debug("get block failed %v", id)
			continue
		}
		dbItems[id] = res
	}

	return nil, dbItems, nil
}
