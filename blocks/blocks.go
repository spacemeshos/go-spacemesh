package blocks

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	mesh2 "github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

const NewBlockProtocol = "newBlock"

var (
	errDupTx = errors.New("duplicate TransactionID in block")

	errDupAtx          = errors.New("duplicate ATXID in block")
	errTooManyAtxs     = errors.New("too many atxs in blocks")
	errNoBlocksInLayer = errors.New("layer has no blocks")
	errNoActiveSet     = errors.New("block does not declare active set")
	errZeroActiveSet   = errors.New("block declares empty active set")
)

type forBlockInView func(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) (bool, error)) error

type BlockHandler struct {
	log.Log
	traverse  forBlockInView
	depth     int
	mesh      mesh2.Mesh
	validator BlockEligibilityValidator
}

func (bh BlockHandler) validateVotes(blk *types.Block) error {
	view := map[types.BlockID]struct{}{}
	for _, b := range blk.ViewEdges {
		view[b] = struct{}{}
	}

	vote := map[types.BlockID]struct{}{}
	for _, b := range blk.BlockVotes {
		vote[b] = struct{}{}
	}

	traverse := func(b *types.Block) (stop bool, err error) {
		if _, ok := vote[b.ID()]; ok {
			delete(vote, b.ID())
		}
		return len(vote) == 0, nil
	}

	// traverse only through the last Hdist layers
	lowestLayer := blk.LayerIndex - types.LayerID(bh.depth)
	if blk.LayerIndex < types.LayerID(bh.depth) {
		lowestLayer = 0
	}
	err := bh.traverse(view, lowestLayer, traverse)
	if err == nil && len(vote) > 0 {
		return fmt.Errorf("voting on blocks out of view (or out of Hdist), %v %s", vote, err)
	}

	return err
}

func (bh *BlockHandler) HandleBlock(data service.GossipMessage, sync service.Fetcher) {
	err := bh.HandleBlockData(data.Bytes(), sync)
	if err != nil {
		bh.Error("%v", err)
		return
	}
	data.ReportValidation(NewBlockProtocol)
}

func (bh *BlockHandler) HandleBlockData(data []byte, sync service.Fetcher) error {
	var blk types.Block
	err := types.BytesToInterface(data, &blk)
	if err != nil {
		bh.Log.Error("received invalid block %v", data, err)

	}

	// set the block id when received
	blk.Initialize()

	activeSet := 0
	if blk.ActiveSet != nil {
		activeSet = len(*blk.ActiveSet)
	}

	refBlock := ""
	if blk.RefBlock != nil {
		refBlock = blk.RefBlock.String()
	}
	bh.Log.With().Info("got new block",
		blk.ID(),
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
	if _, err := bh.mesh.GetBlock(blk.ID()); err == nil {
		//data.ReportValidation(config.NewBlockProtocol)
		bh.With().Info("we already know this block", blk.ID())
		return nil
	}

	err = bh.blockSyntacticValidation(&blk, sync)
	if err != nil {
		bh.With().Error("failed to validate block", blk.ID(), log.Err(err))
		return fmt.Errorf("failed to validate block %v", err)
	}
	//data.ReportValidation(config.NewBlockProtocol)
	if err := bh.mesh.AddBlockWithTxs(&blk); err != nil {
		bh.With().Error("failed to add block to database", blk.ID(), log.Err(err))
		// we return nil here so that the block will still be propagated
		return nil
	}

	if blk.Layer() <= bh.mesh.ProcessedLayer() { //|| blk.Layer() == bh.mesh.getValidatingLayer() {
		bh.mesh.HandleLateBlock(&blk)
	}
	return nil
}

func (bh BlockHandler) blockSyntacticValidation(block *types.Block, syncer service.Fetcher) error {
	// if there is a reference block - first validate it
	if block.RefBlock != nil {
		err := syncer.GetBlock(*block.RefBlock)
		if err != nil {
			return err
		}
	}

	// fast validation checks if there are no duplicate ATX in active set and no duplicate TXs as well
	if err := bh.fastValidation(block); err != nil {
		return err
	}

	// try fetch referenced ATXs
	err := bh.fetchAllReferencedAtxs(block, syncer)
	if err != nil {
		return err
	}

	// get the TXs
	if len(block.TxIDs) > 0 {
		err := syncer.GetTxs(block.TxIDs)
		if err != nil {
			return err
		}
	}

	err = syncer.GetBlocks(block.ViewEdges)
	if err != nil {
		return err
	}

	// validate block's votes
	//if valid, err := validateVotes(block, s.ForBlockInView, s.Hdist, s.Log); valid == false || err != nil {
	if err := bh.validateVotes(block); err != nil {
		return fmt.Errorf("validate votes failed for block %v, %v", block.ID(), err)
	}

	return nil
}

func (bh *BlockHandler) fetchAllReferencedAtxs(blk *types.Block, syncer service.Fetcher) error {
	var atxs []types.ATXID

	atxs = append(atxs, blk.ATXID)

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
	err := syncer.GetAtxs(atxs)
	return err
}

func (bh *BlockHandler) fastValidation(block *types.Block) error {
	// block eligibility
	if eligible, err := bh.validator.BlockSignedAndEligible(block); err != nil || !eligible {
		return fmt.Errorf("block eligibiliy check failed - err %v", err)
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
