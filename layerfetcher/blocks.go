package layerfetcher

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

var (
	errZeroActiveSet   = errors.New("block declares empty active set")
	errNoActiveSet     = errors.New("block does not declare active set")
)

func (l *Logic) blockSyntacticValidation(block *types.Block) error {
	// if there is a reference block - first validate it
	if block.RefBlock != nil {
		err := l.GetBlock(*block.RefBlock)
		if err != nil {
			return err
		}
	}

	// fast validation checks if there are no duplicate ATX in active set and no duplicate TXs as well
	if err := l.blockValidator.fastValidation(block); err != nil {
		return err
	}

	// try fetch referenced ATXs
	err := l.fetchAllReferencedAtxs(block)
	if err != nil {
		return err
	}

	// get the TXs
	if len(block.TxIDs) > 0 {
		err := l.GetTxs(block.TxIDs)
		if err != nil {
			return err
		}
	}


	err = l.GetBlocks(block.ViewEdges)
	if err != nil {
		return err
	}

	// validate block's votes
	//if valid, err := validateVotes(block, s.ForBlockInView, s.Hdist, s.Log); valid == false || err != nil {
	if err := l.blockValidator.validateVotes(block); err != nil {
		return fmt.Errorf("validate votes failed for block %v, %v", block.ID(), err)
	}

	return nil
}

func (l *Logic) BlockReceiveFunc(blockId types.Hash32, data []byte) error{
	var blk types.Block
	err := types.BytesToInterface(data, &blk)
	if err != nil {
		return err
	}

	err = l.blockSyntacticValidation(&blk)
	if err != nil {
		return err
	}

	//todo: should I maybe called gossip handling function here?
	return nil
}

func (l *Logic) fetchRefBlock(block *types.Block) error {
	if block.RefBlock == nil {
		return fmt.Errorf("called fetch ref block with nil ref block %v", block.ID())
	}
	_, err := l.mesh.GetBlock(*block.RefBlock)
	if err != nil {
		l.log.Info("fetching block %v", *block.RefBlock)
		err := l.GetBlock(*block.RefBlock)

		if err != nil {
			return err
		}
	}
	return nil
}

func (l *Logic) fetchAllReferencedAtxs(blk *types.Block) error {
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
	err := l.GetAtxs(atxs)
	return err
}
