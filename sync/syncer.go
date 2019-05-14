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
	BlockEligible(block *types.Block) (bool, error)
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
			s.Debug("run stoped")
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
func NewSync(srv service.Service, layers *mesh.Mesh, bv BlockValidator, conf Configuration, clock timesync.LayerTimer, logger log.Log) *Syncer {
	s := Syncer{
		BlockValidator: bv,
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
	s.RegisterBytesMsgHandler(BLOCK, newBlockRequestHandler(layers, logger))
	s.RegisterBytesMsgHandler(LAYER_IDS, newLayerBlockIdsRequestHandler(layers, logger))

	return &s
}

func (s *Syncer) maxSyncLayer() types.LayerID {
	defer s.currentLayerMutex.RUnlock()
	s.currentLayerMutex.RLock()
	return s.currentLayer
}

func (s *Syncer) Synchronise() {
	for currentSyncLayer := s.VerifiedLayer() + 1; currentSyncLayer < s.maxSyncLayer(); currentSyncLayer++ {
		s.Info("syncing layer %v to layer %v current consensus layer is %d", s.VerifiedLayer(), currentSyncLayer, s.maxSyncLayer())
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
	blocksArr := make([]*types.Block, 0, len(blockIds))
	// each worker goroutine tries to fetch a block iteratively from each peer
	output := make(chan *types.Block)
	count := int32(s.Concurrency)
	for j := 0; j < s.Concurrency; j++ {
		go func() {
			for id := range blockIds {
				for _, p := range s.GetPeers() {
					if bCh, err := sendBlockRequest(s.MessageServer, p, types.BlockID(id), s.Log); err == nil {
						timer := newMilliTimer(blockTime)
						block := <-bCh
						elapsed := timer.ObserveDuration()
						if block == nil {
							continue
						}
						s.Info("fetching block %v took %v ", block.ID(), elapsed)
						blockCount.Add(1)
						eligible, err := s.BlockEligible(block)
						if err != nil {
							s.Error("block eligibility check failed: %v", err)
							continue
						}
						if eligible { //some validation testing
							output <- block
							break
						}
					}
				}
			}
			if atomic.AddInt32(&count, -1); atomic.LoadInt32(&count) == 0 { // last one closes the channel
				close(output)
			}
		}()
	}
	for block := range output {
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

	m, err := s.getLayerHashes(index)

	if err != nil {
		s.Error("could not get LayerHashes for layer: %v", index)
		return nil, err
	}
	return s.getIdsForHash(m, index)
}

func (s *Syncer) getIdsForHash(m map[string]p2p.Peer, index types.LayerID) (chan types.BlockID, error) {

	ch := make(chan []types.BlockID, len(m))
	kill := make(chan struct{})
	for _, peer := range m {
		c, err := s.sendLayerBlockIDsRequest(peer, index)
		if err != nil {
			s.Error("could not send layer %v ids ", index, " from peer ", peer) // todo recover from this
			continue
		}
		go func(p p2p.Peer) {
			select {
			case <-kill:
				s.Info("ids request to %v timed out", p)
				return
			case v := <-c:
				if v != nil {
					ch <- v
				}
			}
		}(peer)
	}

	idSet := make(map[types.BlockID]bool, s.LayerSize)

	go func() {
		<-time.After(s.RequestTimeout)
		close(kill)
		close(ch)
	}()

	for _, bid := range <-ch {
		if _, exists := idSet[bid]; !exists {
			idSet[bid] = true
		}
	}

	if len(m) == 0 {
		return nil, errors.New("could not get layer hashes id from any peer")
	}

	return keysAsChan(idSet), nil

}

func keysAsChan(idSet map[types.BlockID]bool) chan types.BlockID {
	res := make(chan types.BlockID, len(idSet))
	defer close(res)
	for id := range idSet {
		res <- id
	}
	return res
}

func (s *Syncer) getLayerHashes(index types.LayerID) (map[string]p2p.Peer, error) {
	m := make(map[string]p2p.Peer, 20) //todo need to get this from p2p service
	peers := s.GetPeers()
	if len(peers) == 0 {
		return nil, errors.New("no peers")
	}
	// request hash from all
	ch := make(chan *peerHashPair, len(peers))
	kill := make(chan struct{})

	for _, peer := range peers {
		c, err := s.sendLayerHashRequest(peer, index)
		if err != nil {
			s.Error("could not get layer ", index, " hash from peer ", peer)
			continue
		}
		//merge channels and close when done
		go func(p p2p.Peer) {
			select {
			case <-kill:
				s.Error("hash request to %v timed out", p)
				return
			case v := <-c:
				if v != nil {
					ch <- v
				}
			}
		}(peer)
	}

	go func() {
		<-time.After(s.RequestTimeout)
		close(kill)
		close(ch)
	}()

	for pair := range ch {
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

func newBlockRequestHandler(layers *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		logger.Debug("handle block request")
		blockid := types.BlockID(common.BytesToUint64(msg))
		blk, err := layers.GetBlock(blockid)
		if err != nil {
			logger.Error("Error handling Block request message, with BlockID: %d and err: %v", blockid, err)
			return nil
		}

		bbytes, err := types.BlockAsBytes(*blk)
		if err != nil {
			logger.Error("Error marshaling response message (FetchBlockResp), with BlockID: %d, LayerID: %d and err:", blk.ID(), blk.Layer(), err)
			return nil
		}

		logger.Debug("return block %v", blk)

		return bbytes
	}
}

func newLayerHashRequestHandler(layers *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		logger.Debug("handle layer hash request")
		lyrid := common.BytesToUint64(msg)
		layer, err := layers.GetLayer(types.LayerID(lyrid))
		if err != nil {
			logger.Error("Error handling layer %d request message with error: %v", lyrid, err)
			return nil
		}
		return layer.Hash()
	}
}

func newLayerBlockIdsRequestHandler(layers *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		logger.Debug("handle blockIds request")
		lyrid := common.BytesToUint64(msg)
		layer, err := layers.GetLayer(types.LayerID(lyrid))
		if err != nil {
			logger.Error("Error handling ids request message with LayerID: %d and error: %s", lyrid, err.Error())
			return nil
		}

		blocks := layer.Blocks()

		ids := make([]types.BlockID, 0, len(blocks))
		for _, b := range blocks {
			ids = append(ids, b.ID())
		}

		idbytes, err := types.BlockIdsAsBytes(ids)
		if err != nil {
			logger.Error("Error marshaling response message, with blocks IDs: %v and error:", ids, err)
			return nil
		}

		return idbytes
	}
}
