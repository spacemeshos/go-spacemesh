package sync

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
	"sync/atomic"
	"time"
)

type BlockValidator interface {
	BlockEligible(block *types.BlockHeader) (bool, error)
}

type TxValidator interface {
	TxValid(tx types.SerializableTransaction) bool
}

type Configuration struct {
	SyncInterval   time.Duration
	Concurrency    int //number of workers for sync method
	LayerSize      int
	RequestTimeout time.Duration
}

type Syncer struct {
	p2p.Peers
	*mesh.Mesh
	BlockValidator //todo should not be here
	TxValidator
	Configuration
	log.Log
	*server.MessageServer
	currentLayer      types.LayerID
	SyncLock          uint32
	startLock         uint32
	forceSync         chan bool
	clock             timesync.LayerTimer
	exit              chan struct{}
	currentLayerMutex sync.RWMutex
}

func (s *Syncer) ForceSync() {
	s.forceSync <- true
}

func (s *Syncer) Close() {
	s.Peers.Close()
	close(s.forceSync)
	close(s.exit)
}

const (
	IDLE         uint32             = 0
	RUNNING      uint32             = 1
	BLOCK        server.MessageType = 1
	LAYER_HASH   server.MessageType = 2
	LAYER_IDS    server.MessageType = 3
	TX           server.MessageType = 4
	syncProtocol                    = "/sync/1.0/"
)

func (s *Syncer) IsSynced() bool {
	return s.VerifiedLayer() == s.maxSyncLayer()
}

func (s *Syncer) Start() {
	if atomic.CompareAndSwapUint32(&s.startLock, 0, 1) {
		go s.run()
		s.forceSync <- true
		return
	}
}

//fires a sync every sm.syncInterval or on force space from outside
func (s *Syncer) run() {
	syncRoutine := func() {
		if atomic.CompareAndSwapUint32(&s.SyncLock, IDLE, RUNNING) {
			s.Synchronise()
			atomic.StoreUint32(&s.SyncLock, IDLE)
		}
	}
	for {
		select {
		case <-s.exit:
			s.Debug("Work stoped")
			return
		case <-s.forceSync:
			go syncRoutine()
		case layer := <-s.clock:
			s.currentLayerMutex.Lock()
			s.currentLayer = layer
			s.currentLayerMutex.Unlock()
			s.Debug("sync got tick for layer %v", layer)
			go syncRoutine()
		}
	}
}

//fires a sync every sm.syncInterval or on force space from outside
func NewSync(srv service.Service, layers *mesh.Mesh, bv BlockValidator, tv TxValidator, conf Configuration, clock timesync.LayerTimer, logger log.Log) *Syncer {
	s := Syncer{
		BlockValidator: bv,
		TxValidator:    tv,
		Configuration:  conf,
		Log:            logger,
		Mesh:           layers,
		Peers:          p2p.NewPeers(srv, logger.WithName("peers")),
		MessageServer:  server.NewMsgServer(srv.(server.Service), syncProtocol, conf.RequestTimeout, make(chan service.DirectMessage, config.ConfigValues.BufferSize), logger.WithName("srv")),
		SyncLock:       0,
		startLock:      0,
		forceSync:      make(chan bool),
		clock:          clock,
		exit:           make(chan struct{}),
	}

	s.RegisterBytesMsgHandler(LAYER_HASH, newLayerHashRequestHandler(layers, logger))
	s.RegisterBytesMsgHandler(BLOCK, newMiniBlockRequestHandler(layers, logger))
	s.RegisterBytesMsgHandler(LAYER_IDS, newLayerBlockIdsRequestHandler(layers, logger))
	s.RegisterBytesMsgHandler(TX, newTxsRequestHandler(layers, logger))
	return &s
}

func (s *Syncer) maxSyncLayer() types.LayerID {
	defer s.currentLayerMutex.RUnlock()
	s.currentLayerMutex.RLock()
	return s.currentLayer
}

func (s *Syncer) Synchronise() {
	for currentSyncLayer := s.VerifiedLayer() + 1; currentSyncLayer < s.maxSyncLayer(); currentSyncLayer++ {
		s.Info("syncing layer %v to layer %v current consensus layer is %d", s.VerifiedLayer(), currentSyncLayer, s.currentLayer)
		lyr, err := s.GetLayer(types.LayerID(currentSyncLayer))
		if err != nil {
			s.Info("layer %v is not in the database", currentSyncLayer)
			if lyr, err = s.getLayerFromNeighbors(currentSyncLayer); err != nil {
				s.Info("could not get layer %v from neighbors %v", currentSyncLayer, err)
				return
			}
		}
		s.ValidateLayer(lyr)
	}
}

//todo add handling of failure
func (s *Syncer) getLayerFromNeighbors(currenSyncLayer types.LayerID) (*types.Layer, error) {
	blockIds, err := s.getLayerBlockIDs(types.LayerID(currenSyncLayer))
	if err != nil {
		s.Error("could not get layer block ids %v", currenSyncLayer, err)
		return nil, err
	}
	output := FetchBlocks(s, blockIds)
	blocksArr := make([]*types.Block, 0, len(blockIds))
	for out := range output {
		mb := out.(*types.MiniBlock)
		foundTxs, missing := s.GetTransactions(mb.TxIds)
		fetchedTxs, err := s.fetchTxs(missing)
		if err != nil {
			s.Error("could not get all txs for block %v", mb.ID(), err)
			//todo handle error
			return nil, err
		}

		txs := make([]*types.SerializableTransaction, len(mb.TxIds))

		for _, t := range mb.TxIds {
			if tx, ok := foundTxs[t]; ok {
				txs = append(txs, tx)
			} else {
				txs = append(txs, fetchedTxs[t])
			}
		}

		block := &types.Block{BlockHeader: mb.BlockHeader, Txs: txs, ATXs: mb.ATXs}
		s.Debug("add block to layer %v", block)
		s.AddBlock(block)
		blocksArr = append(blocksArr, block)

	}

	return types.NewExistingLayer(types.LayerID(currenSyncLayer), blocksArr), nil
}

type peerHashPair struct {
	peer p2p.Peer
	hash []byte
}

func (s *Syncer) getLayerBlockIDs(index types.LayerID) (chan types.BlockID, error) {

	m, err := s.fetchLayerHashes(index)

	if err != nil {
		s.Error("could not get LayerHashes for layer: %v", index)
		return nil, err
	}
	return s.fetchLayerBlockIds(m, index)
}

func keysAsChan(idSet map[types.BlockID]bool) chan types.BlockID {
	res := make(chan types.BlockID, len(idSet))
	defer close(res)
	for id := range idSet {
		res <- id
	}
	return res
}

func (s *Syncer) fetchLayerBlockIds(m map[string]p2p.Peer, lyr types.LayerID) (chan types.BlockID, error) {
	// each worker goroutine tries to fetch a block iteratively from each peer
	//todo make sure
	peers := s.GetPeers()
	if len(peers) == 0 {
		return nil, errors.New("no peers")
	}

	mu := &sync.Once{}
	numOfpeers := len(peers)
	output := make(chan interface{}, numOfpeers)
	count := int32(numOfpeers)

	for _, peer := range peers {
		wrk := NewWorker(s.MessageServer, s.RequestTimeout, peer, mu, &count, output, LayerIdsReqFactory(lyr))
		go wrk.Work()
	}

	idSet := make(map[types.BlockID]bool, s.LayerSize)
	out := <-output
	for _, bid := range out.([]types.BlockID) {
		if _, exists := idSet[bid]; !exists {
			idSet[bid] = true
		}
	}

	if len(m) == 0 {
		return nil, errors.New("could not get layer hashes id from any peer")
	}

	return keysAsChan(idSet), nil
}

func (s *Syncer) fetchLayerHashes(lyr types.LayerID) (map[string]p2p.Peer, error) {
	// each worker goroutine tries to fetch a block iteratively from each peer
	peers := s.GetPeers()
	if len(peers) == 0 {
		return nil, errors.New("no peers")
	}
	mu := &sync.Once{}
	numOfpeers := len(peers)
	output := make(chan interface{}, numOfpeers)
	count := int32(numOfpeers)
	for _, peer := range peers {
		wrk := NewWorker(s.MessageServer, s.RequestTimeout, peer, mu, &count, output, HashReqFactory(lyr))
		go wrk.Work()
	}

	m := make(map[string]p2p.Peer, 20) //todo need to get this from p2p service
	for out := range output {
		pair := out.(*peerHashPair)
		if pair == nil { //do nothing on close channel
			continue
		}
		m[string(pair.hash)] = pair.peer
	}

	if len(m) == 0 {
		return nil, errors.New("could not get layer hashes from any peer")
	}

	return m, nil
}

func FetchBlocks(s *Syncer, blockIds chan types.BlockID) chan interface{} {
	// each worker goroutine tries to fetch a block iteratively from each peer
	num := common.Min(s.Concurrency, len(blockIds))
	count := int32(num)
	output := make(chan interface{})
	retry := make(chan types.BlockID, len(blockIds))
	mu := &sync.Once{}
	for j := 0; j < num; j++ {
		wrk := NewBlockWorker(s, blockIds, retry, output, mu, &count)
		go wrk.Work()
	}
	return output
}

//todo error handelig
func (s *Syncer) fetchTxs(txids []types.TransactionId) (map[types.TransactionId]*types.SerializableTransaction, error) {
	if len(txids) == 0 {
		return nil, nil
	}
	num := common.Min(s.Concurrency, len(txids))
	output := make(chan interface{})
	retry := make(chan types.TransactionId, len(txids))
	mu := &sync.Once{}
	count := int32(num)
	for j := 0; j < s.Concurrency; j++ {
		wrk := NewTxWorker(s, txids, retry, output, mu, &count)
		go wrk.Work()
	}

	txs := make(map[types.TransactionId]*types.SerializableTransaction)
	for out := range output {
		tx := out.(*types.SerializableTransaction)
		txs[types.GetTransactionId(tx)] = tx
	}
	return txs, nil
}
