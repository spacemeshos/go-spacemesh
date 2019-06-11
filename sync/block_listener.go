package sync

import (
	"github.com/spacemeshos/go-spacemesh/config"
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

type BlockListener struct {
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

func NewBlockListener(net service.Service, bv BlockValidator, sync *Syncer, concurrency int, logger log.Log) *BlockListener {
	bl := BlockListener{
		BlockValidator:       bv,
		Syncer:               sync,
		Log:                  logger,
		semaphore:            make(chan struct{}, concurrency),
		unknownQueue:         make(chan types.BlockID, 200), //todo tune buffer size + get buffer from config
		exit:                 make(chan struct{}),
		receivedGossipBlocks: net.RegisterGossipProtocol(config.NewBlockProtocol),
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
			if bl.IsSynced() {
				bl.wg.Add(1)
				go func() {
					bl.handleBlock(data)
					bl.wg.Done()
				}()
			}
		}
	}
}

func (bl *BlockListener) handleBlock(data service.GossipMessage) {
	if data == nil {
		bl.Error("got empty message while listening to gossip blocks")
		return
	}

	var blk types.Block
	err := types.BytesToInterface(data.Bytes(), &blk)
	if err != nil {
		bl.Error("received invalid block %v", data.Bytes(), err)
		return
	}

	bl.Log.With().Info("got new block", log.Uint64("id", uint64(blk.Id)), log.Int("txs", len(blk.TxIds)), log.Int("atxs", len(blk.ATxIds)))
	eligible, err := bl.BlockEligible(&blk.BlockHeader)
	if err != nil {
		bl.Error("block %v eligible check failed ", blk.ID())
		return
	}
	if !eligible {
		bl.Error("block %v not eligible", blk.ID())
		return
	}

	associated, txs, atxs, err := bl.syncMissingContent(&blk.MiniBlock)
	if err != nil {
		bl.Error("handleBlock %v failed ", blk.ID(), err)
		return
	}

	if associated != nil {
		bl.ProcessAtx(associated)
	}

	if err := bl.AddBlockWithTxs(&blk, txs, atxs); err != nil {
		bl.Error("failed adding block %v", blk.ID())
		return
	}

	bl.Info("added block %v to database", blk.ID())
	data.ReportValidation(config.NewBlockProtocol)
	bl.addUnknownToQueue(&blk.BlockHeader)
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

func (bl *BlockListener) addUnknownToQueue(b *types.BlockHeader) {
	for _, block := range b.ViewEdges {
		//if unknown block
		if _, err := bl.GetBlock(block); err != nil {
			bl.unknownQueue <- block
		}
	}
}
