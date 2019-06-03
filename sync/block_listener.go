package sync

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
	"sync/atomic"
	"time"
)

type MessageServer server.MessageServer

const NewBlockProtocol = "newBlock"

type BlockListener struct {
	*server.MessageServer
	*Syncer
	BlockValidator
	log.Log
	wg                   sync.WaitGroup
	bufferSize           int
	semaphore            chan struct{}
	unknownQueue         chan types.BlockID //todo consider benefits of changing to stack
	receivedGossipBlocks chan service.GossipMessage
	startLock            uint32
	timeout              time.Duration
	exit                 chan struct{}
}

type TickProvider interface {
	Subscribe() timesync.LayerTimer
}

func (bl *BlockListener) Close() {
	close(bl.exit)
	bl.Debug("block listener closing, waiting for gorutines")
	bl.wg.Wait()
}

func (bl *BlockListener) Start() {
	if atomic.CompareAndSwapUint32(&bl.startLock, 0, 1) {
		go bl.run()
		go bl.ListenToGossipBlocks()
	}
}

func (bl *BlockListener) OnNewBlock(b *types.Block) {
	bl.addUnknownToQueue(b)
}

func NewBlockListener(net service.Service, bv BlockValidator, sync *Syncer, concurrency int, logger log.Log) *BlockListener {
	bl := BlockListener{
		BlockValidator:       bv,
		Syncer:               sync,
		Log:                  logger,
		semaphore:            make(chan struct{}, concurrency),
		unknownQueue:         make(chan types.BlockID, 200), //todo tune buffer size + get buffer from config
		exit:                 make(chan struct{}),
		receivedGossipBlocks: net.RegisterGossipProtocol(NewBlockProtocol),
	}
	return &bl
}

func (bl *BlockListener) ListenToGossipBlocks() {
	for {
		select {
		case <-bl.exit:
			bl.Log.Info("listening  stopped")
			return
		case data := <-bl.receivedGossipBlocks:
			bl.wg.Add(1)
			go func() {
				bl.handleBlock(data)
				bl.wg.Done()
			}()
		}
	}
}

func (bl *BlockListener) handleBlock(data service.GossipMessage) {
	if data == nil {
		bl.Error("got empty message while listening to gossip blocks")
		return
	}
	blk := &types.Block{}
	err := types.BytesToInterface(data.Bytes(), blk)
	if err != nil {
		bl.Error("received invalid block %v", data.Bytes())
		return
	}
	bl.Log.With().Info("got new block", log.Uint64("id", uint64(blk.Id)), log.Int("txs", len(blk.Txs)), log.Int("atxs", len(blk.ATXs)))
	eligible, err := bl.BlockEligible(&blk.BlockHeader)
	if err != nil {
		bl.Error("block eligible check failed %v", blk.ID())
		return
	}
	if !eligible {
		bl.Error("block not eligible, %v", blk.ID())
		return
	}
	if err := bl.AddBlock(blk); err != nil {
		bl.Info("Block already received %v", blk.ID())
		return
	}
	bl.Info("added block to database %v", blk.ID())
	data.ReportValidation(NewBlockProtocol)
	bl.addUnknownToQueue(blk)
}

func (bl *BlockListener) run() {
	for {
		select {
		case <-bl.exit:
			bl.Log.Info("Work stopped")
			return
		case id := <-bl.unknownQueue:
			bl.Log.Debug("fetch block ", id, "buffer is at ", len(bl.unknownQueue)/cap(bl.unknownQueue), " capacity")
			bl.semaphore <- struct{}{}
			go func() {
				bl.fetchFullBlocks([]types.BlockID{id})
				<-bl.semaphore
			}()
		}
	}
}

func (bl *BlockListener) addUnknownToQueue(b *types.Block) {
	for _, block := range b.ViewEdges {
		//if unknown block
		if _, err := bl.GetBlock(block); err != nil {
			bl.unknownQueue <- block
		}
	}
}
