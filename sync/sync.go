package sync

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sync/pb"
	"sync"
	"sync/atomic"
	"time"
)

type Layer interface {
	Index() int
	Blocks() []*mesh.Block
	Hash() string
}

type BlockValidator interface {
	ValidateBlock(block *mesh.Block) bool
}

type Configuration struct {
	hdist          uint32 //dist of consensus layers from newst layer
	syncInterval   time.Duration
	concurrency    int //number of workers for sync method
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
	s.layers.Close()
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
	return s.layers.LatestIrreversible() == s.maxSyncLayer()
}

func (s *Syncer) Stop() {
	s.exit <- struct{}{}
}

func (s *Syncer) Start() {
	if atomic.CompareAndSwapUint32(&s.startLock, 0, 1) {
		go s.run()
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
	if s.layers.LatestKnownLayer() < s.config.hdist {
		return 0
	}

	return s.layers.LatestKnownLayer() - s.config.hdist
}

func (s *Syncer) Synchronise() {
	for i := s.layers.LatestIrreversible(); i < s.maxSyncLayer(); {
		blockIds := s.getLayerBlockIDs(mesh.LayerID(i)) //returns a set of all known blocks in the mesh
		output := make(chan *mesh.Block)
		// each worker goroutine tries to fetch a block iteratively from each peer
		var wg sync.WaitGroup
		wg.Add(s.config.concurrency)
		for i := 0; i < s.config.concurrency; i++ {
			go func() {
				defer wg.Done()
				for id := range blockIds {
					//check local database
					b, err := s.layers.GetBlock(id)
					if err == nil {
						output <- b
					} else if b := s.getBlockFromPeers(id); b != nil {
						output <- b
					}
				}

			}()
		}

		go func() {
			wg.Wait()
			close(output)
		}()

		blocks := make([]*mesh.Block, 0, len(blockIds))

		for block := range output {
			log.Debug("add block to layer", block)
			blocks = append(blocks, mesh.NewExistingBlock(block.ID(), block.Layer(), nil))
		}

		log.Debug("add layer ", i)

		s.layers.AddLayer(mesh.NewExistingLayer(mesh.LayerID(i), blocks))
		i++
	}

	log.Debug("synchronise done, local layer index is ", s.layers.LatestIrreversible())
}

func (s *Syncer) getBlockFromPeers(id mesh.BlockID) *mesh.Block {
	for _, p := range s.peers.GetPeers() {
		if bCh, err := s.sendBlockRequest(p, id); err == nil {
			b := <-bCh
			if b != nil && s.sv.ValidateBlock(b) { //some validation testing
				return b
			}
		}
	}
	return nil
}

func (s *Syncer) getLayerBlockIDs(index mesh.LayerID) chan mesh.BlockID {

	m := make(map[string]Peer, 20)
	peers := s.peers.GetPeers()
	// request hash from all

	//todo concurrency
	for _, p := range peers {

		hash, err := s.sendLayerHashRequest(p, index)
		if err != nil {
			log.Debug("could not get layer ", index, " hash from peer ", p)
			continue
		}
		m[string(hash)] = p
	}

	idSet := make(map[uint64]bool, 300) //todo move this to config

	//todo concurrency
	for _, v := range m {
		blocksCh, err := s.sendLayerIDsRequest(v, index)
		blocks := <-blocksCh
		if err == nil && blocks != nil {
			for _, b := range blocks {
				if _, exists := idSet[b]; !exists {
					idSet[b] = true
				}
			}
		}
	}

	res := make(chan mesh.BlockID, len(idSet))
	for b := range idSet {
		res <- mesh.BlockID(b)
	}

	close(res)
	return res
}

func newExistingBlock(block *pb.Block) *mesh.Block {
	return mesh.NewExistingBlock(mesh.BlockID(block.Id), mesh.LayerID(block.Layer), nil)
}

func (s *Syncer) sendBlockRequest(peer Peer, id mesh.BlockID) (chan *mesh.Block, error) {
	log.Debug("send block request Peer: ", peer, " id: ", id)
	data := &pb.FetchBlockReq{Id: uint64(id)}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	ch := make(chan *mesh.Block)

	foo := func(msg []byte) {
		defer close(ch)
		log.Debug("handle block response")
		data := &pb.FetchBlockResp{}
		if err := proto.Unmarshal(msg, data); err != nil {
			log.Error("could not unmarshal block data")
			return
		}

		ch <- newExistingBlock(data.Block)
	}

	return ch, s.p.SendAsyncRequest(BLOCK, payload, peer, foo)
}

func (s *Syncer) sendLayerHashRequest(peer Peer, layer mesh.LayerID) ([]byte, error) {
	log.Debug("send Layer hash request Peer: ", peer, " layer: ", layer)

	data := &pb.LayerHashReq{Layer: uint64(layer)}
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

	if err = proto.Unmarshal(msg.([]byte), res); err != nil {
		log.Error("could not unmarshal layer hash response ", err)
		return nil, err
	}

	return res.Hash, nil
}

func (s *Syncer) sendLayerIDsRequest(peer Peer, idx mesh.LayerID) (chan []uint64, error) {
	log.Debug("send Layer ids request Peer: ", peer, " layer: ", idx)

	data := &pb.LayerIdsReq{Layer: uint64(idx)}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	ch := make(chan []uint64)
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

	id := uint64(block.ID())
	payload, err := proto.Marshal(&pb.FetchBlockResp{Id: id, Block: &pb.Block{Id: id, Layer: uint64(block.Layer())}})
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

	layer, err := s.layers.GetLayer(mesh.LayerID(req.Layer))
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

func (s *Syncer) layerIdsRequestHandler(msg []byte) []byte {
	req := &pb.LayerIdsReq{}
	if err := proto.Unmarshal(msg, req); err != nil {
		return nil
	}

	layer, err := s.layers.GetLayer(mesh.LayerID(req.Layer))
	if err != nil {
		log.Debug("Error handling layer ids request message, err:", err) //todo describe err
		return nil
	}

	blocks := layer.Blocks()

	ids := make([]uint64, 0, len(blocks))

	for _, b := range blocks {
		ids = append(ids, uint64(b.ID()))
	}

	payload, err := proto.Marshal(&pb.LayerIdsResp{Ids: ids})
	if err != nil {
		log.Error("Error handling request message, err:", err) //todo describe err
		return nil
	}

	return payload
}
