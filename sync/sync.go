package sync

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sync/pb"
	"sync/atomic"
	"time"
)

type Block interface {
	GetLayer() uint32
	GetId() uint32
}

type Layer interface {
	Index() int
	Blocks() []Block
	Hash() string
}

type BlockValidator interface {
	ValidateBlock(block Block) bool
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
	layers    mesh.Mesh
	sv        BlockValidator //todo should not be here
	config    Configuration
	p         *server.MessageServer
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

	protocol = "/sync/fblock/1.0/"
)

func (s *Syncer) IsSynced() bool {
	return s.layers.LocalLayerCount() == s.maxSyncLayer()
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

func NewSync(peers Peers, layers mesh.Mesh, bv BlockValidator, conf Configuration) *Syncer {

	s := Syncer{peers,
		layers,
		bv,
		conf,
		server.NewMsgServer(peers, protocol, time.Second*5),
		0,
		0,
		make(chan bool),
		make(chan struct{})}

	s.p.RegisterMsgHandler(LAYER_HASH, s.layerHashRequestHandler)
	s.p.RegisterMsgHandler(BLOCK, s.blockRequestHandler)
	s.p.RegisterMsgHandler(LAYER_IDS, s.layerIdsRequestHandler)

	return &s
}

//fires a sync every sm.config.syncInterval or on force space from outside
func (s *Syncer) run() {
	syncTicker := time.NewTicker(s.config.syncInterval)
	for {
		doSync := false
		select {
		case <-s.exit:
			log.Debug("run stoped")
			return
		case doSync = <-s.forceSync:
		case <-syncTicker.C:
			doSync = true
		default:
			doSync = false
		}
		if doSync {
			go func() {
				if atomic.CompareAndSwapUint32(&s.SyncLock, IDLE, RUNNING) {
					log.Debug("do sync")
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
	for i := s.layers.LocalLayerCount(); i < s.maxSyncLayer(); {

		i++

		blockIds, err := s.getLayerBlockIDs(i) //returns a set of all known blocks in the mesh
		if err != nil {
			log.Error("could not get layer block ids: ", err)
			log.Debug("synchronise failed, local layer index is ", s.layers.LocalLayerCount())
			return
		}

		output := make(chan Block)
		// each worker goroutine tries to fetch a block iteratively from each peer
		count := int32(s.config.concurrency)

		for i := 0; i < s.config.concurrency; i++ {
			go func() {
				for id := range blockIds {
					for _, p := range s.peers.GetPeers() {
						if bCh, err := s.sendBlockRequest(p, id); err == nil {
							b := <-bCh
							if b != nil && s.sv.ValidateBlock(b) { //some validation testing
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
			log.Debug("add block to layer", block)
			blocks = append(blocks, mesh.NewExistingBlock(block.GetId(), block.GetLayer(), nil))
		}

		log.Debug("add layer ", i)

		s.layers.AddLayer(mesh.NewExistingLayer(i, blocks))
	}

	log.Debug("synchronise done, local layer index is ", s.layers.LocalLayerCount())
}

type peerHashPair struct {
	peer Peer
	hash []byte
}

func (s *Syncer) sendBlockRequest(peer Peer, id uint32) (chan Block, error) {
	log.Debug("send block request Peer: ", peer, " id: ", id)
	data := &pb.FetchBlockReq{Id: id}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	ch := make(chan Block)

	foo := func(msg []byte) {
		defer close(ch)
		log.Debug("handle block response")
		data := &pb.FetchBlockResp{}
		if err := proto.Unmarshal(msg, data); err != nil {
			log.Error("could not unmarshal block data")
			return
		}
		ch <- data.Block
	}

	return ch, s.p.SendAsyncRequest(BLOCK, payload, peer, foo)
}

func (s *Syncer) getLayerBlockIDs(index uint32) (chan uint32, error) {

	m, err := s.getLayerHashes(index)

	if err != nil {
		log.Error("could not get LayerHashes for layer: ", index, err)
		return nil, err
	}
	return s.getIdsforHash(m, index)
}

func (s *Syncer) getIdsforHash(m map[string]Peer, index uint32) (chan uint32, error) {
	idSet := make(map[uint32]bool, s.config.layerSize)
	//todo move this to config
	ch := make(chan []uint32)
	reqCounter := 0

	for _, v := range m {
		_, err := s.sendLayerIDsRequest(v, index, ch)
		if err != nil {
			log.Error("could not fetch layer ", index, " block ids from peer ", v) // todo recover from this
			return nil, err
		} else {
			reqCounter++
		}
	}

	timeout := time.After(s.config.requestTimeout)

Loop:
	for {
		select {
		case b := <-ch:
			for _, id := range b {
				if _, exists := idSet[id]; !exists {
					idSet[id] = true
				}
			}
			reqCounter--
			if reqCounter == 0 {
				break Loop
			}
		case <-timeout: // Got a timeout! fail with a timeout error
			if len(idSet) == 0 {
				return nil, errors.New("could not get block ids from any peer")
			}
			break Loop
		}
	}

	res := make(chan uint32, len(idSet))
	defer close(res)
	for id, _ := range idSet {
		res <- id
	}

	return res, nil

}

func (s *Syncer) getLayerHashes(index uint32) (map[string]Peer, error) {
	m := make(map[string]Peer, 20) //todo need to get this from p2p service
	peers := s.peers.GetPeers()
	// request hash from all
	ch := make(chan peerHashPair)
	defer close(ch)
	for _, p := range peers {
		_, err := s.sendLayerHashRequest(p, index, ch)
		if err != nil {
			log.Error("could not get layer ", index, " hash from peer ", p)
			continue
		}
	}

	timeout := time.After(s.config.requestTimeout)
	resCounter := 0
	totalRequests := len(peers)

	for {
		select {
		// Got a timeout! fail with a timeout error
		case pair := <-ch:
			m[string(pair.hash)] = pair.peer
			resCounter++
			if resCounter == totalRequests {
				return m, nil
			}
		case <-timeout:
			if len(m) > 0 {
				return m, nil
			}
			return nil, errors.New("no peers responded to hash request")
		}
	}
}

func (s *Syncer) sendLayerHashRequest(peer Peer, layer uint32, ch chan peerHashPair) (chan peerHashPair, error) {
	log.Debug("send Layer hash request Peer: ", peer, " layer: ", layer)

	data := &pb.LayerHashReq{Layer: layer}
	payload, err := proto.Marshal(data)
	if err != nil {
		log.Error("could not marshall layer hash request")
		return nil, err
	}
	foo := func(msg []byte) {
		res := &pb.LayerHashResp{}
		if msg == nil {
			log.Error("layer hash response was nil from ", peer.String())
			return
		}

		if err = proto.Unmarshal(msg, res); err != nil {
			log.Error("could not unmarshal layer hash response ", err)
			return
		}
		ch <- peerHashPair{peer: peer, hash: res.Hash}
	}
	return ch, s.p.SendAsyncRequest(LAYER_HASH, payload, peer, foo)
}

func (s *Syncer) sendLayerIDsRequest(peer Peer, idx uint32, ch chan []uint32) (chan []uint32, error) {
	log.Debug("send Layer ids request Peer: ", peer, " layer: ", idx)

	data := &pb.LayerIdsReq{Layer: idx}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	foo := func(msg []byte) {
		defer close(ch)
		data := &pb.LayerIdsResp{}
		if err := proto.Unmarshal(msg, data); err != nil {
			log.Error("could not unmarshal layer ids response")
			return
		}
		ch <- data.Ids
	}

	return ch, s.p.SendAsyncRequest(LAYER_IDS, payload, peer, foo)
}

func (s *Syncer) blockRequestHandler(msg []byte) []byte {
	log.Debug("handle block request")
	req := &pb.FetchBlockReq{}
	if err := proto.Unmarshal(msg, req); err != nil {
		return nil
	}

	block, err := s.layers.GetBlock(mesh.BlockID(req.Id))
	if err != nil {
		log.Error("Error handling Block request message, err:", err) //todo describe err
		return nil
	}

	payload, err := proto.Marshal(&pb.FetchBlockResp{Id: block.Id(), Block: &pb.Block{Id: block.Id(), Layer: block.Layer()}})
	if err != nil {
		log.Error("Error handling request message, err:", err) //todo describe err
		return nil
	}

	log.Debug("return block ", block)

	return payload
}

func (s *Syncer) layerHashRequestHandler(msg []byte) []byte {
	req := &pb.LayerHashReq{}
	err := proto.Unmarshal(msg, req)
	if err != nil {
		return nil
	}

	layer, err := s.layers.GetLayer(int(req.Layer))
	if err != nil {
		log.Error("Error handling layer ", req.Layer, " request message, err:", err) //todo describe err
		return nil
	}

	payload, err := proto.Marshal(&pb.LayerHashResp{Hash: layer.Hash()})
	if err != nil {
		log.Error("Error handling request message, err:", err) //todo describe err
		return nil
	}

	return payload
}

func (s *Syncer) layerIdsRequestHandler(msg []byte) []byte {
	req := &pb.LayerIdsReq{}
	if err := proto.Unmarshal(msg, req); err != nil {
		return nil
	}

	layer, err := s.layers.GetLayer(int(req.Layer))
	if err != nil {
		log.Error("Error handling layer ids request message, err:", err) //todo describe err
		return nil
	}

	blocks := layer.Blocks()

	ids := make([]uint32, 0, len(blocks))

	for _, b := range blocks {
		ids = append(ids, b.Id())
	}

	payload, err := proto.Marshal(&pb.LayerIdsResp{Ids: ids})
	if err != nil {
		log.Error("Error handling request message, err:", err) //todo describe err
		return nil
	}

	return payload
}
