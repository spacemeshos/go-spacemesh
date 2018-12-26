package sync

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sync/pb"
	"gopkg.in/op/go-logging.v1"
	"sync/atomic"
	"time"
)

type BlockValidator interface {
	ValidateBlock(block *mesh.Block) bool
}

type Configuration struct {
	hdist          uint32 //dist of consensus layers from newst layer
	syncInterval   time.Duration
	concurrency    int //number of workers for sync method
	layerSize      int
	requestTimeout time.Duration
}

type Syncer struct {
	peers     Peers
	log       logging.Logger
	layers    mesh.Mesh
	bv        BlockValidator //todo should not be here
	config    Configuration
	msgServer *server.MessageServer
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
	IDLE       uint32             = 0
	RUNNING    uint32             = 1
	BLOCK      server.MessageType = 1
	LAYER_HASH server.MessageType = 2
	LAYER_IDS  server.MessageType = 3
	protocol                      = "/sync/fblock/1.0/"
)

func (s *Syncer) IsSynced() bool {
	return s.layers.LatestIrreversible() == s.maxSyncLayer()
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

//fires a sync every sm.config.syncInterval or on force space from outside
func NewSync(srv server.Service, layers mesh.Mesh, bv BlockValidator, conf Configuration, log logging.Logger) *Syncer {
	s := Syncer{
		layers:    layers,
		peers:     NewPeers(srv),
		msgServer: server.NewMsgServer(srv, protocol, conf.requestTimeout-time.Millisecond*30),
		bv:        bv,
		config:    conf,
		SyncLock:  0,
		startLock: 0,
		forceSync: make(chan bool),
		log:       log,
		exit:      make(chan struct{}),
	}

	s.msgServer.RegisterMsgHandler(LAYER_HASH, s.layerHashRequestHandler)
	s.msgServer.RegisterMsgHandler(BLOCK, s.blockRequestHandler)
	s.msgServer.RegisterMsgHandler(LAYER_IDS, s.layerIdsRequestHandler)

	return &s
}

//fires a sync every sm.config.syncInterval or on force space from outside
func (s *Syncer) run() {
	syncTicker := time.NewTicker(s.config.syncInterval)

	for {
		doSync := false
		select {
		case <-s.exit:
			s.log.Debug("run stopped")
			return
		case doSync = <-s.forceSync:
		case <-syncTicker.C:
			doSync = true
		}
		if doSync {
			go func() {
				if atomic.CompareAndSwapUint32(&s.SyncLock, IDLE, RUNNING) {
					s.log.Debug("do sync")
					s.Synchronise()
					atomic.StoreUint32(&s.SyncLock, IDLE)
				}
			}()
		}
	}
}

func (s *Syncer) maxSyncLayer() uint32 {
	if uint32(s.layers.LatestKnownLayer()) < s.config.hdist {
		return 0
	}

	return s.layers.LatestKnownLayer() - s.config.hdist
}

func (s *Syncer) Synchronise() {
	for i := s.layers.LatestIrreversible(); i < s.maxSyncLayer(); i++ {
		s.log.Debug(" synchronise start iteration")
		blockIds, err := s.getLayerBlockIDs(mesh.LayerID(i + 1)) //returns a set of all known blocks in the mesh
		if err != nil || len(blockIds) == 0 {
			s.log.Error(" could not get layer block ids: ", err)
			s.log.Debug(" synchronise failed, local layer index is ", s.layers.LatestIrreversible())
			return
		}

		output := make(chan *mesh.Block)
		// each worker goroutine tries to fetch a block iteratively from each peer
		count := int32(s.config.concurrency)

		for i := 0; i < s.config.concurrency; i++ {
			go func() {
				for id := range blockIds {
					for _, p := range s.peers.GetPeers() {
						if bCh, err := s.sendBlockRequest(p, mesh.BlockID(id)); err == nil {
							select {
							case b := <-bCh:
								if b != nil && s.bv.ValidateBlock(b) { //some validation testing
									output <- b
								}
							case <-time.After(s.config.requestTimeout):
								s.log.Debug("timeout for block ", id)
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
			s.log.Debug("add block to layer", block)
			blocks = append(blocks, block)
		}
		s.log.Debug("add layer ", i, " number of blocks found ", len(blocks))
		s.layers.AddLayer(mesh.NewExistingLayer(mesh.LayerID(i), blocks))
	}
	s.log.Debug("synchronise done, local layer index is ", s.layers.LatestIrreversible())
}

type peerHashPair struct {
	peer Peer
	hash []byte
}

func (s *Syncer) sendBlockRequest(peer Peer, id mesh.BlockID) (<-chan *mesh.Block, error) {
	s.log.Debug("send block request Peer: ", peer, " id: ", id)
	data := &pb.FetchBlockReq{Id: uint32(id)}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	ch := make(chan *mesh.Block) //todo close channel
	foo := func(msg []byte) {
		if msg == nil {
			s.log.Error("block response from ", peer, " was nil")
			return
		}
		s.log.Debug("handle block response")
		data := &pb.FetchBlockResp{}
		if err := proto.Unmarshal(msg, data); err != nil {
			s.log.Error("could not unmarshal block data ", err)
			return
		}
		ch <- mesh.NewExistingBlock(mesh.BlockID(data.Block.GetId()), mesh.LayerID(data.Block.GetLayer()), nil) //todo switch to real block proto and add data
	}

	return ch, s.msgServer.SendRequest(BLOCK, payload, peer, foo)
}

func (s *Syncer) getLayerBlockIDs(index mesh.LayerID) (chan mesh.BlockID, error) {

	m, err := s.getLayerHashes(index)
	if err != nil {
		s.log.Error("could not get LayerHashes for layer: ", index, err)
		return nil, err
	}
	return s.getIdsforHash(m, index)
}

func (s *Syncer) getIdsforHash(m map[string]Peer, index mesh.LayerID) (chan mesh.BlockID, error) {
	reqCounter := 0
	ch := make(chan []uint32)
	//defer close(ch todo close channel gracefully
	for _, v := range m {
		_, err := s.sendLayerIDsRequest(v, index, ch)
		if err != nil {
			s.log.Error("could not fetch layer ", index, " block ids from peer ", v) // todo recover from this
			return nil, err
		} else {
			reqCounter++
		}
	}

	idSet := make(map[mesh.BlockID]bool, s.config.layerSize) //change uint32 to BlockId
	timeout := time.After(s.config.requestTimeout)
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
				s.log.Error("not all peers responded to hash request")
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

func (s *Syncer) getLayerHashes(index mesh.LayerID) (map[string]Peer, error) {
	m := make(map[string]Peer, 20) //todo need to get this from p2p service
	peers := s.peers.GetPeers()
	// request hash from all
	ch := make(chan peerHashPair)
	//defer close(ch) //todo close channel gracefully
	for _, p := range peers {
		_, err := s.sendLayerHashRequest(p, index, ch, int32(len(peers)))
		if err != nil {
			s.log.Error("could not get layer ", index, " hash from peer ", p)
			continue
		}
	}

	timeout := time.After(s.config.requestTimeout)
	resCounter := len(peers)
	for resCounter > 0 {
		select {
		// Got a timeout! fail with a timeout error
		case pair := <-ch:
			m[string(pair.hash)] = pair.peer
			resCounter--
		case <-timeout:
			if len(m) > 0 {
				s.log.Error("not all peers responded to hash request")
				return m, nil
			}
			return nil, errors.New("no peers responded to hash request")
		}
	}
	return m, nil
}

func (s *Syncer) sendLayerHashRequest(peer Peer, layer mesh.LayerID, ch chan peerHashPair, count int32) (<-chan peerHashPair, error) {
	s.log.Debug("send Layer hash request Peer: ", peer, " layer: ", layer)
	var payload []byte
	payload, err := proto.Marshal(&pb.LayerHashReq{Layer: uint32(layer)})
	if err != nil {
		s.log.Error("could not marshall layer hash request")
		return nil, err
	}

	foo := func(msg []byte) {
		if msg == nil {
			s.log.Error("layer hash no response from ", peer.String())
			return
		}

		res := &pb.LayerHashResp{}
		if err = proto.Unmarshal(msg, res); err != nil {
			s.log.Error("could not unmarshal layer hash response ", err)
			return
		}
		ch <- peerHashPair{peer: peer, hash: res.Hash}
	}

	s.log.Debug("send async request")
	return ch, s.msgServer.SendRequest(LAYER_HASH, payload, peer, foo)
}

func (s *Syncer) sendLayerIDsRequest(peer Peer, idx mesh.LayerID, ch chan []uint32) (chan []uint32, error) {
	s.log.Debug("send Layer ids request Peer: ", peer, " layer: ", idx)
	data := &pb.LayerIdsReq{Layer: uint32(idx)}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	foo := func(msg []byte) {
		if msg == nil {
			s.log.Error("layer hash response was nil from ", peer.String())
			return
		}
		data := &pb.LayerIdsResp{}
		if err := proto.Unmarshal(msg, data); err != nil {
			s.log.Error("could not unmarshal layer ids response")
			return
		}
		ch <- data.Ids
	}

	return ch, s.msgServer.SendRequest(LAYER_IDS, payload, peer, foo)
}

func (s *Syncer) blockRequestHandler(msg []byte) []byte {
	s.log.Debug("handle block request")
	req := &pb.FetchBlockReq{}
	if err := proto.Unmarshal(msg, req); err != nil {
		return nil
	}

	block, err := s.layers.GetBlock(mesh.BlockID(req.Id))
	if err != nil {
		s.log.Error("Error handling Block request message, with BlockID: %d and err: %v", req.Id, err)
		return nil
	}

	payload, err := proto.Marshal(&pb.FetchBlockResp{Id: uint32(block.ID()), Block: &pb.Block{Id: uint32(block.ID()), Layer: uint32(block.Layer())}})
	if err != nil {
		s.log.Error("Error marshaling response message (FetchBlockResp), with BlockID: %d, LayerID: %d and err:", block.ID(), block.Layer(), err)
		return nil
	}

	s.log.Debug("return block ", block)

	return payload
}

func (s *Syncer) layerHashRequestHandler(msg []byte) []byte {
	req := &pb.LayerHashReq{}

	err := proto.Unmarshal(msg, req)
	if err != nil {
		return nil
	}

	layer, err := s.layers.GetLayer(mesh.LayerID(req.Layer))
	if err != nil {
		s.log.Error("Error handling layer ", req.Layer, " request message with error:", err)
		return nil
	}

	payload, err := proto.Marshal(&pb.LayerHashResp{Hash: layer.Hash()})
	if err != nil {
		s.log.Error("Error marshaling response message (LayerHashResp) with error:", err)
		return nil
	}

	return payload
}

func (s *Syncer) layerIdsRequestHandler(msg []byte) []byte {
	req := &pb.LayerIdsReq{}
	if err := proto.Unmarshal(msg, req); err != nil {
		return nil
	}

	layer, err := s.layers.GetLayer(mesh.LayerID(req.Layer))
	if err != nil {
		s.log.Error("Error handling layer ids request message with LayerID: %d and error: %s", req.Layer, err.Error())
		return nil
	}

	blocks := layer.Blocks()

	ids := make([]uint32, 0, len(blocks))

	for _, b := range blocks {
		ids = append(ids, uint32(b.ID()))
	}

	payload, err := proto.Marshal(&pb.LayerIdsResp{Ids: ids})
	if err != nil {
		s.log.Error("Error marshaling response message, with blocks IDs: %v and error:", ids, err)
		return nil
	}

	return payload
}
