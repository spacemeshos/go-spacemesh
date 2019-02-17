package sync

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/sync/pb"
	"sync"
	"sync/atomic"
	"time"
)

type BlockValidator interface {
	BlockEligible(id mesh.LayerID, pubKey string) bool
}

type Configuration struct {
	hdist          uint32 //dist of consensus layers from newst layer
	syncInterval   time.Duration
	concurrency    int //number of workers for sync method
	layerSize      int
	requestTimeout time.Duration
}

type Syncer struct {
	p2p.Peers
	*mesh.Mesh
	BlockValidator //todo should not be here
	Configuration
	log.Log
	*server.MessageServer
	SyncLock  uint32
	startLock uint32
	forceSync chan bool
	exit      chan struct{}
}

func (s *Syncer) ForceSync() {
	s.forceSync <- true
}

func (s *Syncer) Close() {
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

func (s *Syncer) Stop() {
	s.exit <- struct{}{}
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
	syncTicker := time.NewTicker(s.syncInterval)
	for {
		doSync := false
		select {
		case <-s.exit:
			log.Debug("run stoped")
			return
		case doSync = <-s.forceSync:
		case <-syncTicker.C:
			doSync = true
		}
		if doSync {
			go func() {
				if atomic.CompareAndSwapUint32(&s.SyncLock, IDLE, RUNNING) {
					s.Debug("do sync")
					s.Synchronise()
					atomic.StoreUint32(&s.SyncLock, IDLE)
				}
			}()
		}
	}
}

//fires a sync every sm.syncInterval or on force space from outside
func NewSync(srv server.Service, layers *mesh.Mesh, bv BlockValidator, conf Configuration, logger log.Log) *Syncer {
	s := Syncer{
		BlockValidator: bv,
		Configuration:  conf,
		Log:            logger,
		Mesh:           layers,
		Peers:          p2p.NewPeers(srv),
		MessageServer:  server.NewMsgServer(srv, syncProtocol, conf.requestTimeout-time.Millisecond*30, make(chan service.DirectMessage, config.ConfigValues.BufferSize), logger),
		SyncLock:       0,
		startLock:      0,
		forceSync:      make(chan bool),
		exit:           make(chan struct{}),
	}

	s.RegisterMsgHandler(LAYER_HASH, newLayerHashRequestHandler(layers, logger))
	s.RegisterMsgHandler(BLOCK, newBlockRequestHandler(layers, logger))
	s.RegisterMsgHandler(LAYER_IDS, newLayerIdsRequestHandler(layers, logger))

	return &s
}

func (s *Syncer) maxSyncLayer() uint32 {
	if uint32(s.LatestLayer()) < s.hdist {
		return 0
	}

	return s.LatestLayer() - s.hdist
}

func (s *Syncer) Synchronise() {
	log.Info("syncing layer %v to layer %v ", s.LatestReceivedLayer(), s.maxSyncLayer())
	for i := s.LatestReceivedLayer(); i < s.maxSyncLayer(); i++ {
		blockIds, err := s.getLayerBlockIDs(mesh.LayerID(i + 1)) //returns a set of all known blocks in the mesh
		if err != nil {
			log.Error("could not get layer block ids: ", err)
			log.Debug("synchronise failed, local layer index is ", i)
			return
		}

		output := make(chan *mesh.Block)
		// each worker goroutine tries to fetch a block iteratively from each peer
		count := int32(s.concurrency)

		for i := 0; i < s.concurrency; i++ {
			go func() {
				for id := range blockIds {
					for _, p := range s.GetPeers() {
						if bCh, err := sendBlockRequest(s.MessageServer, p, mesh.BlockID(id), s.Log); err == nil {
							if b := <-bCh; b != nil && s.BlockEligible(b.LayerIndex, b.MinerID) { //some validation testing
								s.Debug("received block", b)
								output <- b
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

		blocks := make([]*mesh.Block, 0, len(blockIds))

		for block := range output {
			s.Debug("add block to layer", block)
			blocks = append(blocks, block)
		}

		s.Debug("add layer ", i)

		l := mesh.NewExistingLayer(mesh.LayerID(i+1), blocks)
		err = s.AddLayer(l)
		if err != nil {
			log.Error("cannot insert layer to db because %v", err)
			continue
		}
		go s.ValidateLayer(l)
	}

	s.Debug("synchronise done, local layer index is ", s.VerifiedLayer(), "most recent is ", s.LatestReceivedLayer())
}

type peerHashPair struct {
	peer p2p.Peer
	hash []byte
}

func sendBlockRequest(msgServ *server.MessageServer, peer p2p.Peer, id mesh.BlockID, logger log.Log) (chan *mesh.Block, error) {
	logger.Info("send block request Peer: %v id: %v", peer, id)
	data := &pb.FetchBlockReq{Id: uint32(id)}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}
	ch := make(chan *mesh.Block)
	foo := func(msg []byte) {
		defer close(ch)
		logger.Info("handle block response")
		data := &pb.FetchBlockResp{}
		if err := proto.Unmarshal(msg, data); err != nil {
			logger.Error("could not unmarshal block data")
			return
		}
		mp := make([]mesh.BlockID, 0, len(data.Block.VisibleMesh))
		for _, b := range data.Block.VisibleMesh {
			mp = append(mp, mesh.BlockID(b))
		}

		block := mesh.NewExistingBlock(mesh.BlockID(data.Block.GetId()), mesh.LayerID(data.Block.GetLayer()), nil)
		block.ViewEdges = mp
		ch <- block
	}

	return ch, msgServ.SendRequest(BLOCK, payload, peer, foo)
}

func (s *Syncer) getLayerBlockIDs(index mesh.LayerID) (chan mesh.BlockID, error) {

	m, err := s.getLayerHashes(index)

	if err != nil {
		s.Error("could not get LayerHashes for layer: ", index, err)
		return nil, err
	}
	return s.getIdsForHash(m, index)
}

func (s *Syncer) getIdsForHash(m map[string]p2p.Peer, index mesh.LayerID) (chan mesh.BlockID, error) {
	reqCounter := 0

	ch := make(chan []uint32, len(m))
	wg := sync.WaitGroup{}
	for _, v := range m {
		c, err := s.sendLayerIDsRequest(v, index)
		if err != nil {
			s.Error("could not fetch layer ", index, " block ids from peer ", v) // todo recover from this
			continue
		}
		reqCounter++
		wg.Add(1)
		go func() {
			v := <-c
			ch <- v
			wg.Done()
		}()
	}

	go func() { wg.Wait(); close(ch) }()
	idSet := make(map[mesh.BlockID]bool, s.layerSize) //change uint32 to BlockId
	timeout := time.After(s.requestTimeout)
	for reqCounter > 0 {
		select {
		case b := <-ch:
			for _, id := range b {
				bid := mesh.BlockID(id)
				if _, exists := idSet[bid]; !exists {
					idSet[bid] = true
				}
			}
			reqCounter--
		case <-timeout:
			if len(idSet) > 0 {
				s.Error("not all peers responded to hash request")
				return keysAsChan(idSet), nil
			}
			return nil, errors.New("could not get block ids from any peer")

		}
	}

	return keysAsChan(idSet), nil

}

func keysAsChan(idSet map[mesh.BlockID]bool) chan mesh.BlockID {
	res := make(chan mesh.BlockID, len(idSet))
	defer close(res)
	for id := range idSet {
		res <- id
	}
	return res
}

func (s *Syncer) getLayerHashes(index mesh.LayerID) (map[string]p2p.Peer, error) {
	m := make(map[string]p2p.Peer, 20) //todo need to get this from p2p service
	peers := s.GetPeers()
	if len(peers) == 0 {
		return nil, errors.New("no peers")
	}
	// request hash from all
	wg := sync.WaitGroup{}
	ch := make(chan peerHashPair, len(peers))

	resCounter := len(peers)
	for _, p := range peers {
		c, err := s.sendLayerHashRequest(p, index)
		if err != nil {
			s.Error("could not get layer ", index, " hash from peer ", p)
			continue
		}
		//merge channels and close when done
		wg.Add(1)
		go func() {
			v := <-c
			ch <- v
			wg.Done()
		}()
	}
	go func() { wg.Wait(); close(ch) }()
	timeout := time.After(s.requestTimeout)
	for resCounter > 0 {
		// Got a timeout! fail with a timeout error
		select {
		case pair := <-ch:
			m[string(pair.hash)] = pair.peer
			resCounter--
		case <-timeout:
			if len(m) > 0 {
				s.Error("not all peers responded to hash request")
				return m, nil //todo
			}
			return nil, errors.New("no peers responded to hash request")
		}
	}
	return m, nil
}

func (s *Syncer) sendLayerHashRequest(peer p2p.Peer, layer mesh.LayerID) (chan peerHashPair, error) {
	s.Debug("send Layer hash request Peer: ", peer, " layer: ", layer)
	ch := make(chan peerHashPair)
	data := &pb.LayerHashReq{Layer: uint32(layer)}
	payload, err := proto.Marshal(data)
	if err != nil {
		s.Error("could not marshall layer hash request")
		return nil, err
	}
	foo := func(msg []byte) {
		res := &pb.LayerHashResp{}
		if msg == nil {
			s.Error("layer hash response was nil from ", peer.String())
			return
		}

		if err = proto.Unmarshal(msg, res); err != nil {
			s.Error("could not unmarshal layer hash response ", err)
			return
		}
		ch <- peerHashPair{peer: peer, hash: res.Hash}
	}
	return ch, s.SendRequest(LAYER_HASH, payload, peer, foo)
}

func (s *Syncer) sendLayerIDsRequest(peer p2p.Peer, idx mesh.LayerID) (chan []uint32, error) {
	s.Debug("send Layer ids request Peer: ", peer, " layer: ", idx)

	data := &pb.LayerIdsReq{Layer: uint32(idx)}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}
	ch := make(chan []uint32)
	foo := func(msg []byte) {
		defer close(ch)
		data := &pb.LayerIdsResp{}
		if err := proto.Unmarshal(msg, data); err != nil {
			s.Error("could not unmarshal layer ids response")
			return
		}
		ch <- data.Ids
	}

	return ch, s.SendRequest(LAYER_IDS, payload, peer, foo)
}

func newBlockRequestHandler(layers *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		logger.Debug("handle block request")
		req := &pb.FetchBlockReq{}
		if err := proto.Unmarshal(msg, req); err != nil {
			return nil
		}

		block, err := layers.GetBlock(mesh.BlockID(req.Id))
		if err != nil {
			logger.Error("Error handling Block request message, with BlockID: %d and err: %v", req.Id, err)
			return nil
		}

		vm := make([]uint32, 0, len(block.ViewEdges))
		for _, b := range block.ViewEdges {
			vm = append(vm, uint32(b))
		}

		payload, err := proto.Marshal(&pb.FetchBlockResp{Block: &pb.Block{Id: uint32(block.ID()), Layer: uint32(block.Layer()), VisibleMesh: vm}})
		if err != nil {
			logger.Error("Error marshaling response message (FetchBlockResp), with BlockID: %d, LayerID: %d and err:", block.ID(), block.Layer(), err)
			return nil
		}

		logger.Debug("return block ", block)

		return payload
	}
}

func newLayerHashRequestHandler(layers *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		req := &pb.LayerHashReq{}
		err := proto.Unmarshal(msg, req)
		if err != nil {
			return nil
		}

		layer, err := layers.GetLayer(mesh.LayerID(req.Layer))
		if err != nil {
			logger.Error("Error handling layer ", req.Layer, " request message with error:", err)
			return nil
		}

		payload, err := proto.Marshal(&pb.LayerHashResp{Hash: layer.Hash()})
		if err != nil {
			logger.Error("Error marshaling response message (LayerHashResp) with error:", err)
			return nil
		}

		return payload
	}
}

func newLayerIdsRequestHandler(layers *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		req := &pb.LayerIdsReq{}
		if err := proto.Unmarshal(msg, req); err != nil {
			return nil
		}

		layer, err := layers.GetLayer(mesh.LayerID(req.Layer))
		if err != nil {
			logger.Error("Error handling layer ids request message with LayerID: %d and error: %s", req.Layer, err.Error())
			return nil
		}

		blocks := layer.Blocks()

		ids := make([]uint32, 0, len(blocks))

		for _, b := range blocks {
			ids = append(ids, uint32(b.ID()))
		}

		payload, err := proto.Marshal(&pb.LayerIdsResp{Ids: ids})
		if err != nil {
			logger.Error("Error marshaling response message, with blocks IDs: %v and error:", ids, err)
			return nil
		}

		return payload
	}
}
