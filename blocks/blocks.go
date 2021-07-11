package blocks

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"time"
)

// NewBlockProtocol is the protocol indicator for gossip blocks
const NewBlockProtocol = "newBlock"

var (
	errDupTx         = errors.New("duplicate TransactionID in block")
	errDupAtx        = errors.New("duplicate ATXID in block")
	errNoActiveSet   = errors.New("block does not declare active set")
	errZeroActiveSet = errors.New("block declares empty active set")
)

type forBlockInView func(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) (bool, error)) error

type mesh interface {
	GetBlock(types.BlockID) (*types.Block, error)
	AddBlockWithTxs(context.Context, *types.Block) error
	ProcessedLayer() types.LayerID
	HandleLateBlock(*types.Block)
	ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) (bool, error)) error
}

type blockValidator interface {
	BlockSignedAndEligible(block *types.Block) (bool, error)
}

// BlockHandler is the struct responsible for storing meta data needed to process blocks from gossip
type BlockHandler struct {
	log.Log
	traverse    forBlockInView
	depth       int
	mesh        mesh
	validator   blockValidator
	goldenATXID types.ATXID
}

// Config defines configuration for block handler
type Config struct {
	Depth       int
	GoldenATXID types.ATXID
}

// NewBlockHandler creates new BlockHandler
func NewBlockHandler(cfg Config, m mesh, v blockValidator, lg log.Log) *BlockHandler {
	return &BlockHandler{
		Log:         lg,
		traverse:    m.ForBlockInView,
		depth:       cfg.Depth,
		mesh:        m,
		validator:   v,
		goldenATXID: cfg.GoldenATXID,
	}
}

// HandleBlock handles blocks from gossip
func (bh *BlockHandler) HandleBlock(ctx context.Context, data service.GossipMessage, fetcher service.Fetcher) {
	// restore the request ID and add context
	if data.RequestID() != "" {
		ctx = log.WithRequestID(ctx, data.RequestID())
	} else {
		ctx = log.WithNewRequestID(ctx)
		bh.WithContext(ctx).Warning("got block from gossip with no requestId, generated new id")
	}

	if err := bh.HandleBlockData(ctx, data.Bytes(), fetcher); err != nil {
		bh.WithContext(ctx).With().Error("error handling block data", log.Err(err))
		return
	}
	data.ReportValidation(ctx, NewBlockProtocol)
}

// HandleBlockData handles blocks from gossip and sync
func (bh *BlockHandler) HandleBlockData(ctx context.Context, data []byte, fetcher service.Fetcher) error {
	logger := bh.WithContext(ctx)
	logger.Info("handling data for new block")
	start := time.Now()

	var blk types.Block
	if err := types.BytesToInterface(data, &blk); err != nil {
		logger.With().Error("received invalid block", log.Err(err))
	}

	// set the block id when received
	blk.Initialize()
	logger = logger.WithFields(blk.ID(), blk.Layer())

	// check if known
	if _, err := bh.mesh.GetBlock(blk.ID()); err == nil {
		logger.Info("we already know this block")
		return nil
	}
	logger.With().Info("got new block", blk.Fields()...)

	if err := bh.blockSyntacticValidation(ctx, &blk, fetcher); err != nil {
		logger.With().Error("failed to validate block", log.Err(err))
		return fmt.Errorf("failed to validate block %v", err)
	}

	if err := bh.mesh.AddBlockWithTxs(ctx, &blk); err != nil {
		logger.With().Error("failed to add block to database", log.Err(err))
		// we return nil here so that the block will still be propagated
		return nil
	}

	if blk.Layer() <= bh.mesh.ProcessedLayer() { //|| blk.Layer() == bh.mesh.getValidatingLayer() {
		logger.With().Error("block is late",
			log.FieldNamed("processed_layer", bh.mesh.ProcessedLayer()),
			log.FieldNamed("miner_id", blk.MinerID()))
		bh.mesh.HandleLateBlock(&blk)
	}

	logger.With().Info("time to process block", log.Duration("duration", time.Since(start)))
	return nil
}

func combineBlockDiffs(blk types.Block) []types.BlockID {
	return append(blk.ForDiff, append(blk.AgainstDiff, blk.NeutralDiff...)...)
}

func (bh BlockHandler) blockSyntacticValidation(ctx context.Context, block *types.Block, fetcher service.Fetcher) error {
	// Add layer to context, for logging purposes, since otherwise the context will be lost here below
	if reqID, ok := log.ExtractRequestID(ctx); ok {
		ctx = log.WithRequestID(ctx, reqID, block.Layer())
	}

	bh.WithContext(ctx).With().Debug("syntactically validating block", block.ID())

	// if there is a reference block - first validate it
	if block.RefBlock != nil {
		err := fetcher.FetchBlock(ctx, *block.RefBlock)
		if err != nil {
			return fmt.Errorf("failed to fetch ref block %v e: %v", *block.RefBlock, err)
		}
	}

	// try fetch referenced ATXs
	err := bh.fetchAllReferencedAtxs(ctx, block, fetcher)
	if err != nil {
		return err
	}

	// fast validation checks if there are no duplicate ATX in active set and no duplicate TXs as well
	if err := bh.fastValidation(block); err != nil {
		bh.WithContext(ctx).With().Error("failed fast validation", block.ID(), log.Err(err))
		return err
	}

	// get the TXs
	if len(block.TxIDs) > 0 {
		err := fetcher.GetTxs(ctx, block.TxIDs)
		if err != nil {
			return fmt.Errorf("failed to fetch txs %v e: %v", block.ID(), err)
		}
	}

	// get and validate blocks views using the fetch
	err = fetcher.GetBlocks(ctx, combineBlockDiffs(*block))
	if err != nil {
		return fmt.Errorf("failed to fetch view %v e: %v", block.ID(), err)
	}

	bh.WithContext(ctx).With().Debug("validation done: block is syntactically valid", block.ID())
	return nil
}

func (bh *BlockHandler) fetchAllReferencedAtxs(ctx context.Context, blk *types.Block, fetcher service.Fetcher) error {
	bh.WithContext(ctx).With().Debug("block handler fetching all atxs referenced by block", blk.ID())

	// As block with empty or Golden ATXID is considered syntactically invalid, explicit check is not needed here.
	atxs := []types.ATXID{blk.ATXID}

	if blk.ActiveSet != nil {
		if len(*blk.ActiveSet) > 0 {
			atxs = append(atxs, *blk.ActiveSet...)
		} else {
			return errZeroActiveSet
		}
	} else {
		if blk.RefBlock == nil {
			return errNoActiveSet
		}
	}
	if len(atxs) > 0 {
		return fetcher.GetAtxs(ctx, atxs)
	}
	return nil
}

func (bh *BlockHandler) fastValidation(block *types.Block) error {
	// block eligibility
	if eligible, err := bh.validator.BlockSignedAndEligible(block); err != nil || !eligible {
		return fmt.Errorf("block eligibility check failed - err %v", err)
	}

	// validate unique tx atx
	if err := validateUniqueTxAtx(block); err != nil {
		return err
	}
	return nil

}

func validateUniqueTxAtx(b *types.Block) error {
	// check for duplicate tx id
	mt := make(map[types.TransactionID]struct{}, len(b.TxIDs))
	for _, tx := range b.TxIDs {
		if _, exist := mt[tx]; exist {
			return errDupTx
		}
		mt[tx] = struct{}{}
	}

	// check for duplicate atx id
	if b.ActiveSet != nil {
		ma := make(map[types.ATXID]struct{}, len(*b.ActiveSet))
		for _, atx := range *b.ActiveSet {
			if _, exist := ma[atx]; exist {
				return errDupAtx
			}
			ma[atx] = struct{}{}
		}
	}

	return nil
}
