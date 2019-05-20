package sync

import (
	"encoding/hex"
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
	for mb := range output {

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
	count := int32(len(peers))
	output := make(chan []types.BlockID, len(peers))
	for _, peer := range peers {
		wrk := NewLayerIdsWorker(s, lyr, peer, output, &count)
		go wrk.Work()
	}

	idSet := make(map[types.BlockID]bool, s.LayerSize)
	for _, bid := range <-output {
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
	count := int32(len(peers))
	output := make(chan *peerHashPair, len(peers))
	for _, peer := range peers {
		wrk := NewHashWorker(s, lyr, peer, output, &count)
		go wrk.Work()
	}

	m := make(map[string]p2p.Peer, 20) //todo need to get this from p2p service
	for pair := range output {
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

func FetchBlocks(s *Syncer, blockIds chan types.BlockID) chan *types.MiniBlock {
	// each worker goroutine tries to fetch a block iteratively from each peer
	num := common.Min(s.Concurrency, len(blockIds))
	count := int32(num)
	output := make(chan *types.MiniBlock)
	retry := make(chan types.BlockID, len(blockIds))
	for j := 0; j < num; j++ {
		wrk := NewBlockWorker(s, blockIds, retry, output, &count)
		go wrk.Work()
	}
	return output
}

//todo error handelig
func (s *Syncer) fetchTxs(txids []types.TransactionId) (map[types.TransactionId]*types.SerializableTransaction, error) {
	if len(txids) == 0 {
		return nil, nil
	}
	count := int32(s.Concurrency)
	output := make(chan *types.SerializableTransaction)
	retry := make(chan types.TransactionId, len(txids))
	for j := 0; j < s.Concurrency; j++ {
		wrk := NewTxWorker(s, txids, retry, output, &count)
		go wrk.Work()
	}

	txs := make(map[types.TransactionId]*types.SerializableTransaction)
	for tx := range output {
		txs[types.GetTransactionId(tx)] = tx
	}
	return txs, nil
}

func (s *Syncer) sendLayerHashRequest(peer p2p.Peer, layer types.LayerID) (chan *peerHashPair, error) {

	s.Info("send layer hash request Peer: %v layer: %v", peer, layer)
	ch := make(chan *peerHashPair, 1)
	foo := func(msg []byte) {
		defer close(ch)
		s.Info("got hash response from %v hash: %v  layer: %d", peer, msg, layer)
		ch <- &peerHashPair{peer: peer, hash: msg}
	}

	return ch, s.SendRequest(LAYER_HASH, layer.ToBytes(), peer, foo)
}

func (s *Syncer) sendLayerBlockIDsRequest(peer p2p.Peer, idx types.LayerID) (chan []types.BlockID, error) {
	s.Debug("send blockIds request Peer: %v layer:  %v", peer, idx)
	ch := make(chan []types.BlockID, 1)
	foo := func(msg []byte) {
		s.Debug("handle blockIds response Peer: %v layer:  %v", peer, idx)
		defer close(ch)
		ids, err := types.BytesToBlockIds(msg)
		if err != nil {
			s.Error("could not unmarshal mesh.LayerIDs response")
			return
		}
		lyrIds := make([]types.BlockID, 0, len(ids))
		for _, id := range ids {
			lyrIds = append(lyrIds, id)
		}

		ch <- lyrIds
	}

	return ch, s.SendRequest(LAYER_IDS, idx.ToBytes(), peer, foo)
}

func sendBlockRequest(msgServ *server.MessageServer, peer p2p.Peer, id types.BlockID, logger log.Log) (chan *types.Block, error) {
	logger.Debug("send block request Peer: %v id: %v", peer, id)
	ch := make(chan *types.Block, 1)
	foo := func(msg []byte) {
		logger.Debug("handle block response Peer: %v id: %v", peer, id)
		defer close(ch)
		logger.Info("handle block response")
		block, err := types.BytesAsBlock(msg)
		if err != nil {
			logger.Error("could not unmarshal block data")
			return
		}
		ch <- &block
	}

	return ch, msgServ.SendRequest(BLOCK, id.ToBytes(), peer, foo)
}

func (s *Syncer) sendMiniBlockRequest(peer p2p.Peer, id types.BlockID) (chan *types.MiniBlock, error) {
	s.Info("send block request Peer: %v id: %v", peer, id)

	ch := make(chan *types.MiniBlock, 1)
	foo := func(msg []byte) {
		defer close(ch)
		s.Info("handle block response")
		block, err := types.BytesAsMiniBlock(msg)
		if err != nil {
			s.Error("could not unmarshal block data")
			return
		}
		ch <- block
	}

	return ch, s.SendRequest(BLOCK, id.ToBytes(), peer, foo)
}

func (s *Syncer) sendTxRequest(peer p2p.Peer, id types.TransactionId) (chan *types.SerializableTransaction, error) {
	s.Info("send tx request to Peer: %v id: %v", peer, hex.EncodeToString(id[:]))
	ch := make(chan *types.SerializableTransaction, 1)
	foo := func(msg []byte) {
		defer close(ch)
		s.Debug("handle tx response %v", hex.EncodeToString(msg))
		tx, err := types.BytesAsTransaction(msg)
		if err != nil {
			s.Error("could not unmarshal tx data %v", err)
			return
		}
		ch <- tx
	}

	return ch, s.SendRequest(TX, id[:], peer, foo)
}
