package layerfetcher

/*
import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

var (
	errZeroActiveSet   = errors.New("block declares empty active set")
	errNoActiveSet     = errors.New("block does not declare active set")
)

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
}*/
