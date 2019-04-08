package sync

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"sync/atomic"
	"time"
)

type MessageServer server.MessageServer

const BlockProtocol = "/blocks/1.0/"
const NewBlockProtocol = "newBlock"

type BlockListener struct {
	*server.MessageServer
	p2p.Peers
	*mesh.Mesh
	BlockValidator
	log.Log
	bufferSize           int
	semaphore            chan struct{}
	unknownQueue         chan mesh.BlockID //todo consider benefits of changing to stack
	receivedGossipBlocks chan service.GossipMessage
	startLock            uint32
	timeout              time.Duration
	exit                 chan struct{}
	tick                 chan mesh.LayerID
}

type TickProvider interface {
	Subscribe() timesync.LayerTimer
}

func (bl *BlockListener) Close() {
	bl.Peers.Close()
	close(bl.exit)
}

func (bl *BlockListener) Start() {
	if atomic.CompareAndSwapUint32(&bl.startLock, 0, 1) {
		go bl.run()
		go bl.ListenToGossipBlocks()
	}
}

func (bl *BlockListener) OnNewBlock(b *mesh.Block) {
	bl.addUnknownToQueue(b)
}

func NewBlockListener(net service.Service, bv BlockValidator, layers *mesh.Mesh, timeout time.Duration, concurrency int, logger log.Log) *BlockListener {

	bl := BlockListener{
		BlockValidator:       bv,
		Mesh:                 layers,
		Peers:                p2p.NewPeers(net),
		MessageServer:        server.NewMsgServer(net.(server.Service), BlockProtocol, timeout, make(chan service.DirectMessage, config.ConfigValues.BufferSize), logger),
		Log:                  logger,
		semaphore:            make(chan struct{}, concurrency),
		unknownQueue:         make(chan mesh.BlockID, 200), //todo tune buffer size + get buffer from config
		exit:                 make(chan struct{}),
		receivedGossipBlocks: net.RegisterGossipProtocol(NewBlockProtocol),
	}
	bl.RegisterBytesMsgHandler(BLOCK, newBlockRequestHandler(layers, logger))

	return &bl
}

func (bl *BlockListener) ListenToGossipBlocks() {
	for {
		select {
		case <-bl.exit:
			bl.Log.Info("listening  stopped")
			return
		case data := <-bl.receivedGossipBlocks:

			if data == nil {
				bl.Error("got empty message while listening to gossip blocks")
				break
			}

			blk, err := mesh.BytesAsBlock(data.Bytes())
			if err != nil {
				bl.Error("received invalid block %v", data.Bytes()[:7])
				data.ReportValidation(NewBlockProtocol, false)
				break
			}

			bl.Log.With().Info("got new block", log.Uint64("id", uint64(blk.Id)), log.Int("txs", len(blk.Txs)))
			eligible, err := bl.BlockEligible(blk.LayerIndex, blk.MinerID, mesh.BlockEligibilityProof{}) // TODO: use nodeID and eligibilityProof from block
			if err != nil {
				bl.Error("block eligible check failed")
				break
			}
			if !eligible {
				data.ReportValidation(NewBlockProtocol, false)
				bl.Error("block not eligible")
				break
			}

			data.ReportValidation(NewBlockProtocol, true)
			if err := bl.AddBlock(&blk); err != nil {
				bl.Info("Block already received")
				break
			}
			bl.Info("added block to database ")
			bl.addUnknownToQueue(&blk)
		}
	}
}

func (bl *BlockListener) run() {
	for {
		select {
		case <-bl.exit:
			bl.Log.Info("run stopped")
			return
		case id := <-bl.unknownQueue:
			bl.Log.Debug("fetch block ", id, "buffer is at ", len(bl.unknownQueue)/cap(bl.unknownQueue), " capacity")
			bl.semaphore <- struct{}{}
			go func() {
				bl.FetchBlock(id)
				<-bl.semaphore
			}()
		}
	}
}

//todo handle case where no peer knows the block
func (bl *BlockListener) FetchBlock(id mesh.BlockID) {
	for _, p := range bl.GetPeers() {
		if ch, err := sendBlockRequest(bl.MessageServer, p, id, bl.Log); err == nil {
			eligibilityProof := mesh.BlockEligibilityProof{} // TODO: take from the block
			b := <-ch
			if b == nil {
				continue
			}
			eligible, err := bl.BlockEligible(b.LayerIndex, b.MinerID, eligibilityProof)
			if err != nil {
				panic("return error!") // TODO: return error
			}
			if eligible {
				bl.AddBlock(b)
				bl.addUnknownToQueue(b) //add all child blocks to unknown queue
				return
			}
		}
	}
}

func (bl *BlockListener) addUnknownToQueue(b *mesh.Block) {
	for _, block := range b.ViewEdges {
		//if unknown block
		if _, err := bl.GetBlock(block); err != nil {
			bl.unknownQueue <- block
		}
	}
}
