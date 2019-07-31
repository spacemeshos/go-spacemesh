package sync

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
)

type dataAvailabilityFunc = func(blk *types.Block) ([]*types.AddressableSignedTransaction, []*types.ActivationTx, error)
type addBlockFunc func(blk *types.Block, txs []*types.AddressableSignedTransaction, atxs []*types.ActivationTx) error
type checkLocalFunc func(id types.BlockID) (*types.Block, error)

type validationQueue struct {
	log.Log
	queue            chan types.BlockID
	callbacks        map[types.BlockID]func(res bool) error
	depMap           map[types.BlockID]map[types.BlockID]struct{}
	reverseDepMap    map[types.BlockID][]types.BlockID
	visited          map[types.BlockID]struct{}
	addBlock         addBlockFunc
	checkLocal       checkLocalFunc
	dataAvailability dataAvailabilityFunc
}

func NewValidationQueue(davail dataAvailabilityFunc, addBlock addBlockFunc, checkLocal checkLocalFunc, lg log.Log) *validationQueue {
	vq := &validationQueue{
		queue:            make(chan types.BlockID, 100),
		visited:          make(map[types.BlockID]struct{}),
		depMap:           make(map[types.BlockID]map[types.BlockID]struct{}),
		reverseDepMap:    make(map[types.BlockID][]types.BlockID),
		callbacks:        make(map[types.BlockID]func(res bool) error),
		dataAvailability: davail,
		addBlock:         addBlock,
		checkLocal:       checkLocal,
		Log:              lg,
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

func (vq *validationQueue) traverse(s *Syncer) error {

	output := s.fetchWithFactory(NewBlockWorker(s, s.Concurrency, BlockReqFactory(), vq.queue))
	for out := range output {
		jb, ok := out.(blockJob)
		if !ok || jb.block == nil {
			if callback, callOk := vq.callbacks[jb.id]; callOk {
				callback(false)
			}

			vq.updateDependencies(jb.id, false)
			vq.Error(fmt.Sprintf("could not retrieve a block in view "))
			continue
		}

		vq.Info("fetched  %v", jb.block.ID())

		vq.visited[jb.block.ID()] = struct{}{}
		if eligable, err := s.BlockSignedAndEligible(jb.block); err != nil || !eligable {
			vq.updateDependencies(jb.id, false)
			vq.Error(fmt.Sprintf("Block %v eligiblety check failed %v", jb.block.ID(), err))
			continue
		}

		s.Info("Validating view Block %v", jb.block.ID())

		if vq.addDependencies(jb.block, vq.finishBlockCallback(jb.block)) == false {
			if err := vq.runCallback(jb.id, true); err != nil {
				return err
			}

			s.Info("dependencies done for %v", jb.block.ID())
			vq.updateDependencies(jb.id, true)
			vq.Info(" %v blocks in dependency map", len(vq.depMap))
		}
	}

	return nil
}

func (vq *validationQueue) finishBlockCallback(block *types.Block) func(res bool) error {
	return func(res bool) error {
		if !res {
			vq.Info("finished block %v block invalid", block.ID())
			return nil
		}

		//data availability
		txs, atxs, err := vq.dataAvailability(block)
		if err != nil {
			return err
		}

		if err := vq.addBlock(block, txs, atxs); err != nil {
			return err
		}

		return nil
	}
}

func (vq *validationQueue) updateDependencies(block types.BlockID, valid bool) {
	delete(vq.depMap, block)
	var doneBlocks []types.BlockID
	doneQueue := make([]types.BlockID, 0, len(vq.depMap))
	doneQueue = vq.removefromDepMaps(block, doneQueue)
	for {
		if len(doneQueue) == 0 {
			break
		}
		block = doneQueue[0]
		doneQueue = doneQueue[1:]
		doneBlocks = append(doneBlocks, block)
		doneQueue = vq.removefromDepMaps(block, doneQueue)
	}

	for _, bid := range doneBlocks {
		if err := vq.runCallback(bid, valid); err != nil {
			vq.Error(fmt.Sprintf("could not finalize block validation %v", err))
		}
	}

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

func (vq *validationQueue) addDependencies(blk *types.Block, finishBlockCallback func(res bool) error) bool {
	vq.callbacks[blk.ID()] = finishBlockCallback
	dependencys := make(map[types.BlockID]struct{})
	for _, id := range blk.ViewEdges {
		if vq.inQueue(id) {
			vq.reverseDepMap[id] = append(vq.reverseDepMap[id], blk.ID())
			vq.Info("add block %v to %v dependencies map", id, blk.ID())
			dependencys[id] = struct{}{}
		} else {
			//	check database
			if _, err := vq.checkLocal(id); err != nil {
				//unknown block add to queue
				vq.queue <- id
				vq.reverseDepMap[id] = append(vq.reverseDepMap[id], blk.ID())
				vq.Info("add block %v to %v dependencies map", id, blk.ID())
				dependencys[id] = struct{}{}
			}
		}
	}

	//todo better this is a little hacky
	if len(dependencys) == 0 {
		vq.runCallback(blk.ID(), true)
		return false
	}

	vq.depMap[blk.ID()] = dependencys

	return true
}

func (vq *validationQueue) runCallback(id types.BlockID, valid bool) error {
	if callback, ok := vq.callbacks[id]; ok {

		if err := callback(valid); err != nil {
			return err
		}

		delete(vq.callbacks, id)
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
