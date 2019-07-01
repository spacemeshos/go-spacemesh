package sync

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
)

type validationQueue struct {
	log.Log
	queue         chan types.BlockID
	callbacks     map[types.BlockID]func() error
	depMap        map[types.BlockID]map[types.BlockID]struct{}
	reverseDepMap map[types.BlockID][]types.BlockID
}

func NewValidationQueue(lg log.Log) *validationQueue {
	vq := &validationQueue{
		queue:         make(chan types.BlockID, 100),
		depMap:        make(map[types.BlockID]map[types.BlockID]struct{}),
		reverseDepMap: make(map[types.BlockID][]types.BlockID),
		callbacks:     make(map[types.BlockID]func() error),
		Log:           lg,
	}

	return vq
}

func (vq *validationQueue) Done() {
	vq.Info("done")
	close(vq.queue)
}

func (vq *validationQueue) traverse(s *Syncer) []types.BlockID {
	output := s.fetchWithFactory(NewBlockWorker(s, s.Concurrency, BlockReqFactory(), vq.queue))

	for out := range output {
		block := out.(*types.Block)
		s.Info("Validating view Validate %v", block.ID())
		if err := s.Validate(block); err != nil {
			s.Error(fmt.Sprintf("failed derefrencing block data %v %v", block.ID(), err))
			continue
		}

		vq.callbacks[block.ID()] = func() error {
			//data availability
			txs, atxs, err := s.DataAvailabilty(block)
			if err != nil {
				return errors.New(fmt.Sprintf("data availabilty failed for block %v", block.ID()))
			}

			if err := s.AddBlockWithTxs(block, txs, atxs); err != nil {
				return errors.New(fmt.Sprintf("failed to add block %v to database %v", block.ID(), err))
			}
			return nil
		}

		if vq.checkDependencies(&block.BlockHeader, s.GetBlock) {
			s.Info("dependencies done for %v", block.ID())
			s.Info("%v left in dependencies map ", len(vq.reverseDepMap))
			if len(vq.reverseDepMap) == 0 {
				vq.Done()
				return nil
			}
		}
	}

	return vq.getMissingBlocks()
}

func (vq *validationQueue) checkDependencies(bHeader *types.BlockHeader, checkDatabase func(id types.BlockID) (*types.Block, error)) bool {
	var dependencys map[types.BlockID]struct{}
	for _, block := range bHeader.ViewEdges {

		//if unknown block

		//	check queue
		if _, ok := vq.reverseDepMap[block]; !ok {

			//	check database
			if _, err := checkDatabase(block); err != nil {

				//add to queue
				vq.Info("add %v to validation queue", block)
				vq.queue <- block

				//init dependency set
				dependencys = make(map[types.BlockID]struct{})
				dependencys[block] = struct{}{}
			}
		}
	}

	if len(dependencys) == 0 {

		//remove this block from all dependencies lists
		for _, b := range vq.reverseDepMap[bHeader.ID()] {

			//we can update recursively all dependencies or just wait for the queue to finish processing
			delete(vq.depMap[b], bHeader.ID())
			if len(vq.depMap[b]) == 0 {
				delete(vq.depMap, b)
				callback := vq.callbacks[b]
				callback()
			}
		}

		//remove this block from reverse dependency map
		vq.Info("remove %v from dependency map", bHeader.ID())
		delete(vq.reverseDepMap, bHeader.ID())
		vq.Info(" %v blocks in dependency map", len(vq.reverseDepMap))
		return true
	}

	vq.depMap[bHeader.ID()] = dependencys
	return false
}

func (vq *validationQueue) getMissingBlocks() []types.BlockID {
	missingBlocks := make([]types.BlockID, 0, len(vq.reverseDepMap))
	for k := range vq.reverseDepMap {
		missingBlocks = append(missingBlocks, k)
	}
	return missingBlocks
}
