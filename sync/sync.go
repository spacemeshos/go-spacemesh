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

type Layer interface {
	Index() int
	Blocks() []Block
	Hash() string
}

type BlockValidator interface {
	ValidateBlock(block Block) bool
}

type Configuration struct {
	hdist        int
	pNum         int
	syncInterval time.Duration
	concurrency  int
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
	exit      chan bool
}

// i dont like this becuse no one outside can see that this is blocking
func (s *Syncer) ForceSync() {
	s.forceSync <- true
}

func (s *Syncer) Close() {
	s.exit <- true
	close(s.forceSync)
	close(s.exit)
}

type Status int

const (
	RUNNING Status = 1
	IDLE    Status = 0
)

func (s *Syncer) Status() Status {
	return Status(atomic.LoadUint32(&s.SyncLock))
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
	s := Syncer{peers, layers, bv, conf, p2p.NewProtocol(peers, protocol, time.Second*5), 0, 0, make(chan bool), make(chan bool)}

	s.p.RegisterMsgHandler(layerHash, s.LayerHashRequestHandler)
	s.p.RegisterMsgHandler(blockMsg, s.BlockRequestHandler)

	return &s
}

//fires a sync every sm.config.syncInterval or on force space from outside
func (s *Syncer) run() {
	syncTicker := time.NewTicker(s.config.syncInterval)
	for {
		doSync := false
		select {
		case <-syncTicker.C:
			doSync = true
		case doSync = <-s.forceSync:
		case <-s.exit:
			fmt.Println("run stoped")
			return
		default:
			doSync = false
		}
		if doSync {
			go func() {
				if atomic.CompareAndSwapUint32(&s.SyncLock, 0, 1) {
					s.Synchronise()
					atomic.StoreUint32(&s.SyncLock, 0)
					return
				}
			}()
		}
	}
}

func (s *Syncer) Synchronise() {
	for i := s.layers.LocalLayerCount(); ; i++ {
		blockIds := s.GetLayerBlockIDs(i) //returns a set of all known blocks in the mesh
		ids := make(chan uint32)
		output := make(chan Block)

		// each worker goroutine tries to fetch a block iteratively from each peer
		for i := 0; i < s.config.concurrency; i++ {
			go func() {
				for id := range ids {
					for _, p := range s.peers.GetPeers() {
						b, err := s.getBlockById_Blocking(p, id)
						if err != nil {
							continue
						} else if s.sv.ValidateBlock(b) { //some validation testing
							output <- b
						}
					}
				}
			}()
		}

		//feed the workers with ids
		go func() {
			for _, id := range blockIds {
				ids <- id
			}
		}()

		close(ids) //todo check that gorutins stop

		blocks := make([]*mesh.Block, 0, len(blockIds))
		for block := range output {
			blocks = append(blocks, mesh.NewExistingBlock(block.GetId(), block.GetLayer(), nil))
		}

		s.layers.AddLayer(mesh.NewExistingLayer(i, blocks))
	}
}

func (s *Syncer) GetLayerBlockIDs(index uint32) []uint32 {

	m := make(map[string]Peer, 20)
	peers := s.peers.GetPeers()
	// request hash from all
	for _, p := range peers {
		hash, err := s.getLayerHash_Blocking(p, index) // get hash from peer
		if err != nil {
			//todo handle err
		}
		m[string(hash.Hash)] = p
	}

	var res []uint32
	for k, v := range m {
		blocks, err := s.RequestLayerIDs(v, index, k)
		if err == nil {
			res = append(res, blocks...)
		}
	}

	return res
}

type Block interface {
	GetLayer() uint32
	GetId() uint32
}

func (s *Syncer) getBlockById_Blocking(peer Peer, id uint32) (Block, error) {
	b, err := s.SendBlockRequest(peer, id)
	if err != nil {
		//err handling
	}
	return <-b, nil
}

func (s *Syncer) getBlockById(peer Peer, id uint32) (chan Block, error) {
	b, err := s.SendBlockRequest(peer, id)
	if err != nil {
		//err handling
	}
	return b, nil
}

func (s *Syncer) getLayerHash_Blocking(peer Peer, id uint32) (*pb.LayerHashResp, error) {

	b, err := s.SendLayerHashRequest(peer, id)
	if err != nil {
		//err handling
	}
	return <-b, nil
}

func (s *Syncer) getLayerHash(peer Peer, id uint32) (chan *pb.LayerHashResp, error) {
	b, err := s.SendLayerHashRequest(peer, id)
	if err != nil {
		//err handling
	}
	return b, nil
}

const protocol = "/sync/fblock/1.0/"
const blockMsg = 1
const layerHash = 2

func (s Syncer) SendBlockRequest(peer Peer, id uint32) (chan Block, error) {

	data := &pb.FetchBlockReq{
		Id:    id,
		Layer: uint32(1),
	}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}
	foo, ch := NewBlockResponseHandler()

	return ch, s.p.SendAsyncRequest(blockMsg, payload, peer.String(), foo)
}

func (s Syncer) SendLayerHashRequest(peer Peer, layer uint32) (chan *pb.LayerHashResp, error) {

	data := &pb.LayerHashReq{Layer: layer}
	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	foo, ch := NewLayerHashResHandler()
	return ch, s.p.SendAsyncRequest(layerHash, payload, peer.String(), foo)
}

func NewBlockResponseHandler() (func(msg []byte), chan Block) {
	ch := make(chan Block)
	foo := func(msg []byte) {
		data := &pb.FetchBlockResp{}
		err := proto.Unmarshal(msg, data)
		if err != nil {
			fmt.Println("some error")
		}
		ch <- data
	}
	return foo, ch
}

func NewLayerHashResHandler() (func(msg []byte), chan *pb.LayerHashResp) {
	ch := make(chan *pb.LayerHashResp)
	foo := func(msg []byte) {
		data := &pb.LayerHashResp{}
		err := proto.Unmarshal(msg, data)
		if err != nil {
			fmt.Println("some error")
		}
		ch <- data
	}
	return foo, ch
}

func (s *Syncer) BlockRequestHandler(msg []byte) []byte {
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

	payload, err := proto.Marshal(&pb.FetchBlockResp{Layer: block.Layer(), Id: block.Id()})
	if err != nil {
		log.Error("Error handling request message, err:", err) //todo describe err
		return nil
	}

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

	payload, err := proto.Marshal(&pb.LayerHashResp{Hash: []byte(layer.Hash())})
	if err != nil {
		log.Error("Error handling request message, err:", err) //todo describe err
		return nil
	}

	return payload
}
func (s *Syncer) RequestLayerIDs(peer Peer, i uint32, s2 string) ([]uint32, error) {
	//todo implement
	return []uint32{1}, nil
}
