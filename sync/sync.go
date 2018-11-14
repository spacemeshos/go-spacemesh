package sync

import (
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/sync/pb"
	"sync/atomic"
	"time"
)

type Status int

const (
	RUNNING Status = 1
	IDLE    Status = 0
)

// an unsynced node can register for participating in  block creation but it needs to be synced while creating the block
// space stops when network.GetLayerBlockIds(i) returns 0 ids
// network.GetLayerBlockIds(i) keeps going while mesh id < t - hdist
// all blocks recived by gossip should be cached as long as mesh id  => t - hdist

type Configuration struct {
	hdist        int
	pNum         int
	syncInterval time.Duration
	concurrency  int
}

type Block interface {
	Id() uint32
	Layer() uint32
}

type Layer interface {
	Index() int
	Blocks() []Block
	Hash() string
}

type Syncer struct {
	peers     Peers
	layers    mesh.Mesh
	sv        BlockValidator //todo should not be here
	config    Configuration
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
	sm := Syncer{peers, layers, bv, conf, 0, 0, make(chan bool), make(chan bool)}
	return &sm
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

type BlockValidator interface {
	CheckSyntacticValidity(block Block) bool
}

func (s *Syncer) Synchronise() {
	for i := s.layers.LocalLayerCount(); ; i++ {

		blockIds := s.GetLayerBlockIDs(int(i)) //returns a set of all known blocks in the mesh
		ids := make(chan *string)
		output := make(chan Block)

		// each worker gorutine tries to fetch a block iteretivly from each peer
		for i := 0; i < s.config.concurrency; i++ {
			go func() {
				for id := range ids {
					for _, p := range s.peers.GetPeers() {
						b, err := s.getBlockById(p, *id)
						if err != nil {
							continue
						} else if s.sv.CheckSyntacticValidity(b) { //cant really do this
							output <- b
						}
					}
				}
			}()
		}

		//feed the workers with ids
		go func() {
			for _, id := range blockIds {
				ids <- &id
			}
		}()

		close(ids) //todo check that gorutins stop

		blocks := make([]Block, len(blockIds))
		for block := range output {
			blocks = append(blocks, block)
		}

		//s.layers.AddLayer(mesh.NewExistingLayer(i, blocks)) //todo fix
	}
}

func (s *Syncer) GetLayerBlockIDs(index int) []string {

	m := make(map[string]Peer, 20)
	peers := s.peers.GetPeers()
	// request hash from all
	for _, p := range peers {
		hash := s.GetLayerHash(p, index) // get hash from peer
		m[hash] = p
	}

	var res []string
	for k, v := range m {
		blocks, err := s.peers.GetLayerBlockIDs(v, index, k)
		if err == nil {
			res = append(res, blocks...)
		}
	}

	return res
}

func (s *Syncer) GetLayerHash(peer Peer, index int) string {
	return ""
}

func (s *Syncer) getBlockById(peer Peer, id string) (Block, error) {
	b, err := s.peers.GetBlockByID(peer, id)
	if err != nil {
		//err handling
	}
	return b, nil
}

func (s *Syncer) BlockRequestHandler(msg []byte) []byte {
	req := &pb.FetchBlockReq{}
	err := proto.Unmarshal(msg, req)
	if err != nil {
		return nil
	}

	block, err := s.layers.GetBlock(mesh.BlockID(req.BlockId))
	if err != nil {
		log.Error("Error handling Block request message, err:", err) //todo describe err
		return nil
	}

	payload, err := proto.Marshal(&pb.FetchBlockResp{Layer: block.Layer(), BlockId: block.Id()})
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
