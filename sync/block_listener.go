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
	layersPerEpoch       uint16
}

type TickProvider interface {
	Subscribe() timesync.LayerTimer
}

func (bl *BlockListener) Close() {
	close(bl.exit)
	bl.Info("block listener closing, waiting for gorutines")
	bl.Syncer.Close()
	bl.Info("block listener closed")
}

func (bl *BlockListener) Start() {
	if atomic.CompareAndSwapUint32(&bl.startLock, 0, 1) {
		go bl.ListenToGossipBlocks()
	}
}

func NewBlockListener(net service.Service, sync *Syncer, concurrency int, layersPerEpoch uint16, logger log.Log) *BlockListener {
	bl := BlockListener{
		Syncer:               sync,
		Log:                  logger,
		semaphore:            make(chan struct{}, concurrency),
		exit:                 make(chan struct{}),
		receivedGossipBlocks: net.RegisterGossipProtocol(config.NewBlockProtocol),
		layersPerEpoch:       layersPerEpoch,
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
				bl.With().Info("ignoring gossip blocks - not synced yet")
				break
			}
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

			if bl.HandleNewBlock(&blk) {
				data.ReportValidation(config.NewBlockProtocol)
			}

		}
	}
}

func (bl *BlockListener) HandleNewBlock(blk *types.Block) bool {

	atxstring := ""
	for _, atx := range blk.AtxIds {
		atxstring += atx.ShortId() + ", "
	}
	bl.Log.With().Info("got new block", log.BlockId(uint64(blk.Id)), log.LayerId(uint64(blk.LayerIndex)), log.EpochId(uint64(bl.currentLayer.GetEpoch(bl.layersPerEpoch))), log.Int("txs", len(blk.TxIds)), log.Int("atxs", len(blk.AtxIds)), log.String("atx_list", atxstring))
	//check if known

	if _, err := bl.GetBlock(blk.Id); err == nil {
		bl.With().Info("we already know this block", log.BlockId(uint64(blk.ID())))
		return true
	}

	bl.Log.With().Info("finished database check. running syntactic validation ", log.BlockId(uint64(blk.ID())))

	txs, atxs, err := bl.BlockSyntacticValidation(blk)
	if err != nil {
		bl.With().Error("failed to validate block", log.BlockId(uint64(blk.ID())), log.Err(err))
		return false
	}

	bl.Log.With().Info("finished syntactic validation adding block with txs", log.BlockId(uint64(blk.ID())))
	go func() {
		if err := bl.AddBlockWithTxs(blk, txs, atxs); err != nil {
			bl.Log.With().Error("failed to add block to database", log.BlockId(uint64(blk.ID())), log.Err(err))
			return
		}
	}()
	bl.With().Info("added block to database", log.BlockId(uint64(blk.ID())))
	return true
}
