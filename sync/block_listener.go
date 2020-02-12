package sync

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"sync"
	"sync/atomic"
	"time"
)

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
		receivedGossipBlocks: net.RegisterGossipProtocol(config.NewBlockProtocol, priorityq.High),
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
			if !bl.ListenToGossip() {
				bl.With().Info("ignoring gossip blocks - not synced yet")
				break
			}

			bl.wg.Add(1)
			go func() {
				defer bl.wg.Done()
				if data == nil {
					bl.Error("got empty message while listening to gossip blocks")
					return
				}
				tmr := newMilliTimer(gossipBlockTime)
				bl.handleBlock(data)
				tmr.ObserveDuration()
			}()

		}
	}
}

func (bl *BlockListener) handleBlock(data service.GossipMessage) {
	var blk types.Block
	err := types.BytesToInterface(data.Bytes(), &blk)
	if err != nil {
		bl.Error("received invalid block %v", data.Bytes(), err)
		return
	}

	//set the block id when received
	blk.Initialize()

	bl.Log.With().Info("block received",
		log.BlockId(blk.Id().String()),
		log.LayerId(blk.LayerIndex.Uint64()),
		log.Int("tx_count", len(blk.TxIds)),
		log.Int("atx_count", len(blk.AtxIds)),
		log.Int("view_edges", len(blk.ViewEdges)),
		log.Int("vote_count", len(blk.BlockVotes)),
		log.AtxId(blk.ATXID.Hash32().String()),
		log.Uint32("eligibility_counter", blk.EligibilityProof.J),
	)
	//check if known
	if _, err := bl.GetBlock(blk.Id()); err == nil {
		bl.With().Info("we already know this block", log.BlockId(blk.Id().String()))
		return
	}
	txs, atxs, err := bl.blockSyntacticValidation(&blk)
	if err != nil {
		bl.With().Error("failed to validate block", log.BlockId(blk.Id().String()), log.Err(err))
		return
	}
	data.ReportValidation(config.NewBlockProtocol)
	if err := bl.AddBlockWithTxs(&blk, txs, atxs); err != nil {
		bl.With().Error("failed to add block to database", log.BlockId(blk.Id().String()), log.Err(err))
		return
	}

	if blk.Layer() <= bl.ProcessedLayer() || blk.Layer() == bl.ValidatingLayer() {
		bl.Syncer.HandleLateBlock(&blk)
	}
	return
}
