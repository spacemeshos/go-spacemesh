package sync

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/common/types"
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

	if vq.addDependencies(blk, s.GetBlock) == false {
		return nil
	}

	vq.callbacks[blk.ID()] = func() error {
		return nil
	}

	output := s.fetchWithFactory(NewBlockWorker(s, s.Concurrency, BlockReqFactory(), vq.queue))
	for out := range output {
		block, ok := out.(*types.Block)
		if !ok || block == nil {
			return errors.New(fmt.Sprintf("could not retrieve a block in %v view ", blk.ID()))
		}

		vq.visited[block.ID()] = struct{}{}
		s.Info("Checking eligibility for block %v", block.ID())
		if eligable, err := s.BlockSignedAndEligible(block); err != nil || !eligable {
			return errors.New(fmt.Sprintf("Block %v eligiblety check failed %v", blk.ID(), err))
		}

		vq.callbacks[block.ID()] = vq.finishBlockCallback(s, block)

		s.Info("Validating view for block %v", block.ID())
		if vq.addDependencies(&block.BlockHeader, s.GetBlock) == false {
			if err := vq.addToDatabase(block.ID()); err != nil {
				return err
			}

			s.Info("dependencies done for %v", block.ID())
			doneBlocks := vq.updateDependencies(block.ID())
			for _, bid := range doneBlocks {
				if err := vq.addToDatabase(bid); err != nil {
					return errors.New(fmt.Sprintf("could not finalize block %v validation %v", blk.ID(), err))
				}
			}

			vq.Info(" %v blocks in dependency map", len(vq.depMap))
		}

		if len(vq.reverseDepMap) == 0 {
			vq.done()
			return nil
		}
	}

	return nil
}

func (vq *validationQueue) finishBlockCallback(s *Syncer, block *types.Block) func() error {
	return func() error {
		//data availability
		atxsHash12, atxsRes, atxsSeen, err := s.checkAtxCache(block)
		if err != nil {
			return fmt.Errorf("failed to check ATXs cache - err %v", err)
		}
		if atxsSeen == true && atxsRes == false {
			return fmt.Errorf("invalid atxs according to cache (key %v)", atxsHash12)
		}
		txs, txErr, atxs, atxErr := s.DataAvailability(block, atxsSeen)
		if txErr != nil || atxErr != nil {
			if atxErr != nil {
				// update cache only on failures, positive caching is done after all ATXs are processed
				s.updateAtxCache(atxsHash12, false)
				if atxsSeen {
					// shouldn't happen
					// todo - remove this check once code is tested
					s.Log.Panic("bad cache behavior")
				}
			}
			return fmt.Errorf("txerr %v, atxerr %v", txErr, atxErr)
		}

		//validate block's votes
		if valid := s.validateVotes(block); valid == false {
			return errors.New(fmt.Sprintf("validate votes failed for block %v", block.ID()))
		}

		if err := s.AddBlockWithTxs(block, txs, atxs); err != nil {
			return err
		}
		// should be called after the atxs are processed, since in cases of cache hit we don't process any atx)
		s.updateAtxCache(atxsHash12, true)

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
