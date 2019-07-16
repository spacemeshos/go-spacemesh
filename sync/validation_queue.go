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
	visited       map[types.BlockID]struct{}
}

func NewValidationQueue(lg log.Log) *validationQueue {
	vq := &validationQueue{
		queue:         make(chan types.BlockID, 100),
		visited:       make(map[types.BlockID]struct{}),
		depMap:        make(map[types.BlockID]map[types.BlockID]struct{}),
		reverseDepMap: make(map[types.BlockID][]types.BlockID),
		callbacks:     make(map[types.BlockID]func() error),
		Log:           lg,
	}

	return vq
}

func (vq *validationQueue) inQueue(id types.BlockID) bool {
	_, ok := vq.reverseDepMap[id]
	if ok {
		return true
	}

	_, ok = vq.visited[id]
	if ok {
		return true
	}
	return false
}

func (vq *validationQueue) done() {
	vq.Info("done")
	close(vq.queue)
}

func (vq *validationQueue) traverse(s *Syncer, blk *types.BlockHeader) error {

	blocklog := vq.Log.WithFields(log.Uint64("block_id", uint64(blk.Id)))
	blocklog.Info("starting to traverse block view")

	if vq.addDependencies(blk, s.GetBlock) == false {
		return nil
	}

	vq.callbacks[blk.ID()] = func() error {
		return nil
	}

	blocklog.Info("starting fetch factory")

	output := s.fetchWithFactory(NewBlockWorker(s, s.Concurrency, BlockReqFactory(), vq.queue))
	for out := range output {
		block, ok := out.(*types.Block)
		if !ok || block == nil {
			return errors.New(fmt.Sprintf("could not retrieve a block in %v view ", blk.ID()))
		}

		vq.visited[block.ID()] = struct{}{}
		blocklog.Info("Validating view Block")
		if err := s.confirmBlockValidity(block); err != nil {
			return err
		}

		blocklog.Info("done confirming validity adding finish callbacks")

		vq.callbacks[block.ID()] = vq.finishBlockCallback(s, block)

		blocklog.Info("Starting to add dependencies")

		if vq.addDependencies(&block.BlockHeader, s.GetBlock) == false {
			if err := vq.addToDatabase(block.ID()); err != nil {
				return err
			}

			blocklog.Info("dependencies done")
			doneBlocks := vq.updateDependencies(block.ID())
			for _, bid := range doneBlocks {
				if err := vq.addToDatabase(bid); err != nil {
					return errors.New(fmt.Sprintf("could not finalize block %v validation %v", blk.ID(), err))
				}
			}

			blocklog.Info(" %v blocks in dependency map", len(vq.depMap))
		}

		if len(vq.reverseDepMap) == 0 {
			vq.done()
			return nil
		}
	}

	blocklog.Info("done traversing")

	return nil
}

func (vq *validationQueue) finishBlockCallback(s *Syncer, block *types.Block) func() error {
	return func() error {
		//data availability
		txs, atxs, err := s.DataAvailabilty(block)
		if err != nil {
			return err
		}

		if err := s.AddBlockWithTxs(block, txs, atxs); err != nil {
			return err
		}

		return nil
	}
}

func (vq *validationQueue) updateDependencies(block types.BlockID) []types.BlockID {
	delete(vq.depMap, block)
	var blocks []types.BlockID
	doneQueue := make([]types.BlockID, 0, len(vq.depMap))
	doneQueue = vq.removefromDepMaps(block, doneQueue)
	for {
		if len(doneQueue) == 0 {
			return blocks
		}
		block = doneQueue[0]
		doneQueue = doneQueue[1:]
		blocks = append(blocks, block)
		doneQueue = vq.removefromDepMaps(block, doneQueue)
	}
	return nil
}

func (vq *validationQueue) removefromDepMaps(block types.BlockID, queue []types.BlockID) []types.BlockID {
	for _, b := range vq.reverseDepMap[block] {
		delete(vq.depMap[b], block)
		if len(vq.depMap[b]) == 0 {
			delete(vq.depMap, b)
			queue = append(queue, b)
		}
	}
	delete(vq.reverseDepMap, block)
	return queue
}

func (vq *validationQueue) addDependencies(blk *types.BlockHeader, checkDatabase func(id types.BlockID) (*types.Block, error)) bool {
	dependencys := make(map[types.BlockID]struct{})
	for _, id := range blk.ViewEdges {
		if vq.inQueue(id) {
			vq.reverseDepMap[id] = append(vq.reverseDepMap[id], blk.ID())
			vq.Info("add block %v to %v dependencies map", id, blk.ID())
			dependencys[id] = struct{}{}
		} else {
			//	check database
			if _, err := checkDatabase(id); err != nil {
				//unknown block add to queue
				vq.queue <- id
				vq.reverseDepMap[id] = append(vq.reverseDepMap[id], blk.ID())
				vq.Info("add block %v to %v dependencies map", id, blk.ID())
				dependencys[id] = struct{}{}
			}
		}
	}

	vq.depMap[blk.ID()] = dependencys
	return len(dependencys) > 0
}

func (vq *validationQueue) addToDatabase(id types.BlockID) error {
	if callback, ok := vq.callbacks[id]; ok {
		if err := callback(); err != nil {
			return err
		}
	}
	return nil
}

func (vq *validationQueue) getMissingBlocks() []types.BlockID {
	missingBlocks := make([]types.BlockID, 0, len(vq.reverseDepMap))
	for k := range vq.reverseDepMap {
		missingBlocks = append(missingBlocks, k)
	}
	return missingBlocks
}
