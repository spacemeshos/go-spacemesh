package sync

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"sync"
	"time"
)

//BlockListener Listens to blocks propagated in gossip
type BlockListener struct {
	*Syncer
	blockEligibilityValidator
	log.Log
	wg                   sync.WaitGroup
	bufferSize           int
	semaphore            chan struct{}
	receivedGossipBlocks chan service.GossipMessage
	startLock            types.TryMutex
	timeout              time.Duration
	exit                 chan struct{}
}

//Close closes all running goroutines
func (bl *BlockListener) Close() {
	close(bl.exit)
	bl.Info("block listener closing, waiting for gorutines")
	bl.wg.Wait()
	bl.Syncer.Close()
	bl.Info("block listener closed")
}

//Start starts the main listening goroutine
func (bl *BlockListener) Start() {
	if bl.startLock.TryLock() {
		go bl.listenToGossipBlocks()
	}
}

//NewBlockListener creates a new instance of BlockListener
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

func (bl *BlockListener) listenToGossipBlocks() {
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

	activeSet := 0
	if blk.ActiveSet != nil {
		activeSet = len(*blk.ActiveSet)
	}

	refBlock := ""
	if blk.RefBlock != nil {
		refBlock = blk.RefBlock.String()
	}
	bl.Log.With().Info("got new block",
		blk.ID(),
		blk.LayerIndex,
		blk.LayerIndex.GetEpoch(),
		log.String("sender_id", blk.MinerID().ShortString()),
		log.Int("tx_count", len(blk.TxIDs)),
		log.Int("atx_count", len(blk.ATXIDs)),
		log.Int("view_edges", len(blk.ViewEdges)),
		log.Int("vote_count", len(blk.BlockVotes)),
		blk.ATXID,
		log.Uint32("eligibility_counter", blk.EligibilityProof.J),
		log.String("ref_block", refBlock),
		log.Int("active_set", activeSet),
	)
	//check if known
	if _, err := bl.GetBlock(blk.ID()); err == nil {
		bl.With().Info("we already know this block", log.BlockID(blk.ID().String()))
		return
	}
	txs, atxs, err := bl.blockSyntacticValidation(&blk)
	if err != nil {
		bl.With().Error("failed to validate block", log.BlockID(blk.ID().String()), log.Err(err))
		return
	}
	data.ReportValidation(config.NewBlockProtocol)
	if err := bl.AddBlockWithTxs(&blk, txs, atxs); err != nil {
		bl.With().Error("failed to add block to database", log.BlockID(blk.ID().String()), log.Err(err))
		return
	}

	if blk.Layer() <= bl.ProcessedLayer() || blk.Layer() == bl.getValidatingLayer() {
		bl.Syncer.HandleLateBlock(&blk)
	}
	return
}
