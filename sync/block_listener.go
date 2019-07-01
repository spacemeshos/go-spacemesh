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
	bl.Info("block listener closing, waiting for gorutines")
	bl.wg.Wait()
	bl.Syncer.Close()
	bl.Info("block listener closed")
}

func (bl *BlockListener) Start() {
	if atomic.CompareAndSwapUint32(&bl.startLock, 0, 1) {
		go bl.ListenToGossipBlocks()
	}
}

func NewBlockListener(net service.Service, sync *Syncer, concurrency int, logger log.Log) *BlockListener {
	bl := BlockListener{
		Syncer:               sync,
		Log:                  logger,
		semaphore:            make(chan struct{}, concurrency),
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
			if !bl.IsSynced() {
				bl.Info("ignoring gossip blocks - not synced yet")
				break
			}
			bl.wg.Add(1)
			go func() {
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

				if bl.handleBlock(&blk) {
					data.ReportValidation(config.NewBlockProtocol)
				}
				bl.wg.Done()
			}()

		}
	}
}

func (bl *BlockListener) handleBlock(blk *types.Block) bool {

	bl.Log.With().Info("got new block", log.Uint64("id", uint64(blk.Id)), log.Int("txs", len(blk.TxIds)), log.Int("atxs", len(blk.AtxIds)))
	if err := bl.SyncAndValidate(blk); err != nil {
		bl.Error("handleBlock %v failed ", blk.ID(), err)
		return false
	}
	bl.Info("added block %v to database", blk.ID())
	return true
}
