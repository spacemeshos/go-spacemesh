package sync

import (
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
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
	hdist          uint32
	pNum           int
	syncInterval   time.Duration
	concurrency    int
	requestTimeout time.Duration
}

type Syncer struct {
	peers     Peers
	layers    mesh.Mesh
	sv        BlockValidator //todo should not be here
	config    Configuration
	p         *p2p.Protocol
	SyncLock  uint32
	startLock uint32
	forceSync chan bool
	exit      chan interface{}
}

func (s *Syncer) ForceSync() {
	s.forceSync <- true
}

func (s *Syncer) Close() {
	close(s.forceSync)
	close(s.exit)
}

const (
	IDLE       uint32          = 0
	RUNNING    uint32          = 1
	BLOCK      p2p.MessageType = 1
	LAYER_HASH p2p.MessageType = 2
	LAYER_IDS  p2p.MessageType = 3

	protocol = "/sync/fblock/1.0/"
)

func (s *Syncer) IsSynced() bool {
	return s.layers.LocalLayerCount() == s.maxSyncLayer()
}

func (s *Syncer) Stop() {
	s.exit <- true
}

func (s *Syncer) Start() {
	if atomic.CompareAndSwapUint32(&s.startLock, 0, 1) {
		go s.run()
		return
	}
}

func NewSync(peers Peers, layers mesh.Mesh, bv BlockValidator, conf Configuration) *Syncer {
	s := Syncer{peers, layers, bv, conf, p2p.NewProtocol(peers, protocol, time.Second*5), 0, 0, make(chan bool), make(chan interface{})}
	s.p.RegisterMsgHandler(LAYER_HASH, s.LayerHashRequestHandler)
	s.p.RegisterMsgHandler(BLOCK, s.BlockRequestHandler)
	s.p.RegisterMsgHandler(LAYER_IDS, s.LayerIdsRequestHandler)
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
			fmt.Println("timer !!!")
			doSync = true
		default:
			doSync = false
		}
		if doSync {
			go func() {
				if atomic.CompareAndSwapUint32(&s.SyncLock, IDLE, RUNNING) {
					fmt.Println("do sync")
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
		blockIds := s.GetLayerBlockIDs(i) //returns a set of all known blocks in the mesh
		ids := make(chan uint32, len(blockIds))
		output := make(chan Block)

		//bufferd chan for workers  to read from

		for _, id := range blockIds {
			ids <- id
		}

		close(ids)

		// each worker goroutine tries to fetch a block iteratively from each peer
		for i := 0; i < s.config.concurrency; i++ {
			go func() {
				for id := range ids {
					for _, p := range s.peers.GetPeers() {
						if bCh, err := s.SendBlockRequest(p, id); err == nil {
							b := <-bCh
							if s.sv.ValidateBlock(b) { //some validation testing
								output <- b
								break
							}
						}

					}
				}
				close(output)
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

func (s *Syncer) GetLayerBlockIDs(index uint32) []uint32 {

	m := make(map[string]Peer, 20)
	peers := s.peers.GetPeers()
	// request hash from all

	for _, p := range peers {

		hash, err := s.SendLayerHashRequest(p, index)
		if err != nil {
			log.Debug("could not get layer ", index, " hash from peer")
			continue
		}
		m[string(hash)] = p
	}

	var res []uint32
	for _, v := range m {
		blocksCh, err := s.SendLayerIDsRequest(v, index)
		blocks := <-blocksCh
		if err == nil {
			res = append(res, blocks...)
		}
	}

	return res
}

func (s *Syncer) SendBlockRequest(peer Peer, id uint32) (chan Block, error) {
	log.Debug("send block request Peer: ", "id: ", id)
	data := &pb.FetchBlockReq{Id: id}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	ch := make(chan Block)
	foo := func(msg []byte) {
		log.Debug("handle block response")
		data := &pb.FetchBlockResp{}
		err := proto.Unmarshal(msg, data)
		if err != nil {
			log.Error("could not unmarshal block data")
		}
		ch <- data.Block
	}

	return ch, s.p.SendAsyncRequest(BLOCK, payload, peer, foo)
}

func (s *Syncer) SendLayerHashRequest(peer Peer, layer uint32) ([]byte, error) {
	log.Debug("send Layer hash request Peer: ", "layer: ", layer)

	data := &pb.LayerHashReq{Layer: layer}
	payload, err := proto.Marshal(data)
	if err != nil {
		log.Error("could not marshall layer hash request")
		return nil, err
	}

	msg, err := s.p.SendRequest(LAYER_HASH, payload, peer, s.config.requestTimeout)
	if err != nil {
		log.Error("could not send layer hash request ", err)
		return nil, err
	}

	res := &pb.LayerHashResp{}
	err = proto.Unmarshal(msg.([]byte), res)
	if err != nil {
		log.Error("could not unmarshal layer hash response ", err)
		return nil, err
	}

	return res.Hash, nil
}

func (s *Syncer) SendLayerIDsRequest(peer Peer, idx uint32) (chan []uint32, error) {

	data := &pb.LayerIdsReq{Layer: idx}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	ch := make(chan []uint32)
	foo := func(msg []byte) {
		data := &pb.LayerIdsResp{}
		err := proto.Unmarshal(msg, data)
		if err != nil {
			log.Error("could not unmarshal layer ids response")
		}
		ch <- data.Ids
	}

	return ch, s.p.SendAsyncRequest(LAYER_IDS, payload, peer, foo)
}

func (s *Syncer) BlockRequestHandler(msg []byte) []byte {
	log.Debug("handle block request")
	req := &pb.FetchBlockReq{}
	err := proto.Unmarshal(msg, req)
	if err != nil {
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

func (s *Syncer) LayerHashRequestHandler(msg []byte) []byte {
	req := &pb.LayerHashReq{}
	err := proto.Unmarshal(msg, req)
	if err != nil {
		return nil
	}

	layer, err := s.layers.GetLayer(int(req.Layer))
	if err != nil {
		log.Error("Error handling layer request message, err:", err) //todo describe err
		return nil
	}

	payload, err := proto.Marshal(&pb.LayerHashResp{Hash: layer.Hash()})
	if err != nil {
		log.Error("Error handling request message, err:", err) //todo describe err
		return nil
	}

	return payload
}

func (s *Syncer) LayerIdsRequestHandler(msg []byte) []byte {
	req := &pb.LayerIdsReq{}
	err := proto.Unmarshal(msg, req)
	if err != nil {
		return nil
	}

	layer, err := s.layers.GetLayer(int(req.Layer))
	if err != nil {
		log.Debug("Error handling layer ids request message, err:", err) //todo describe err
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
