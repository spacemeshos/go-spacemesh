package sync

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"sync"
	"time"
)

// BlockListener Listens to blocks propagated in gossip
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

// Close closes all running goroutines
func (bl *BlockListener) Close() {
	// this stops us from receiving any new blocks, but we may still already be processing received blocks
	close(bl.exit)
	bl.Info("block listener closing, waiting for goroutines to finish")
	// this waits for processing of received blocks to finish
	bl.wg.Wait()
	bl.Info("block listener closed")
}

// Start starts the main listening goroutine
func (bl *BlockListener) Start(ctx context.Context) {
	if bl.startLock.TryLock() {
		go bl.listenToGossipBlocks(log.WithNewSessionID(ctx))
	}
}

// NewBlockListener creates a new instance of BlockListener
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

func (bl *BlockListener) listenToGossipBlocks(ctx context.Context) {
	bl.wg.Add(1)
	defer bl.wg.Done()
	for {
		select {
		case <-bl.exit:
			bl.Log.Info("stopped listening to gossip blocks")
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
				bl.handleBlock(ctx, data)
				tmr.ObserveDuration()
			}()
		}
	}
}

func (bl *BlockListener) handleBlock(ctx context.Context, data service.GossipMessage) {
	logger := bl.WithContext(ctx)
	var blk types.Block
	if err := types.BytesToInterface(data.Bytes(), &blk); err != nil {
		logger.With().Error("received invalid block", log.Int("data_len", len(data.Bytes())), log.Err(err))
		return
	}

	// set the block id when received
	blk.Initialize()
	logger = logger.WithFields(blk.ID())

	activeSet := 0
	if blk.ActiveSet != nil {
		activeSet = len(*blk.ActiveSet)
	}

	refBlock := ""
	if blk.RefBlock != nil {
		refBlock = blk.RefBlock.String()
	}
	logger.With().Info("got new block",
		blk.LayerIndex,
		blk.LayerIndex.GetEpoch(),
		log.String("sender_id", blk.MinerID().ShortString()),
		log.Int("tx_count", len(blk.TxIDs)),
		//log.Int("atx_count", len(blk.ATXIDs)),
		log.Int("view_edges", len(blk.ViewEdges)),
		log.Int("vote_count", len(blk.BlockVotes)),
		blk.ATXID,
		log.Uint32("eligibility_counter", blk.EligibilityProof.J),
		log.String("ref_block", refBlock),
		log.Int("active_set", activeSet),
	)
	// check if known
	if _, err := bl.GetBlock(blk.ID()); err == nil {
		data.ReportValidation(ctx, config.NewBlockProtocol)
		logger.Info("we already know this block")
		return
	}
	txs, atxs, err := bl.blockSyntacticValidation(ctx, &blk)
	if err != nil {
		logger.With().Error("failed to validate block", log.Err(err))
		return
	}
	data.ReportValidation(ctx, config.NewBlockProtocol)
	if err := bl.AddBlockWithTxs(&blk, txs, atxs); err != nil {
		logger.With().Error("failed to add block to database", log.Err(err))
		return
	}

	if blk.Layer() <= bl.ProcessedLayer() || blk.Layer() == bl.getValidatingLayer() {
		bl.Syncer.HandleLateBlock(&blk)
	}
	return
}
