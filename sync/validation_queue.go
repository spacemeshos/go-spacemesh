package sync

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"reflect"
	"sync"
)

type syncer interface {
	AddBlockWithTxs(blk *types.Block, txs []*types.Transaction, atxs []*types.ActivationTx) error
	GetBlock(id types.BlockID) (*types.Block, error)
	ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) (bool, error)) error
	HandleLateBlock(bl *types.Block)
	ProcessedLayer() types.LayerID
	dataAvailability(blk *types.Block) ([]*types.Transaction, []*types.ActivationTx, error)
	getValidatingLayer() types.LayerID
	fastValidation(block *types.Block) error
	blockCheckLocal(blockIds []types.Hash32) (map[types.Hash32]item, map[types.Hash32]item, []types.Hash32)
}

type blockQueue struct {
	syncer
	fetchQueue
	Configuration
	callbacks     map[interface{}]func(res bool) error
	depMap        map[interface{}]map[types.Hash32]struct{}
	reverseDepMap map[types.Hash32][]interface{}
}

func newValidationQueue(srvr networker, conf Configuration, sy syncer) *blockQueue {
	vq := &blockQueue{
		fetchQueue: fetchQueue{
			Log:                 srvr.WithName("blockFetchQueue"),
			workerInfra:         srvr,
			checkLocal:          sy.blockCheckLocal,
			batchRequestFactory: blockFetchReqFactory,
			Mutex:               &sync.Mutex{},
			pending:             make(map[types.Hash32][]chan bool),
			queue:               make(chan []types.Hash32, 1000),
			name:                "Block",
		},
		Configuration: conf,
		depMap:        make(map[interface{}]map[types.Hash32]struct{}),
		reverseDepMap: make(map[types.Hash32][]interface{}),
		callbacks:     make(map[interface{}]func(res bool) error),
		syncer:        sy,
	}
	vq.handleFetch = vq.handleBlocks
	go vq.work()

	return vq
}

func (vq *blockQueue) inQueue(id types.Hash32) bool {
	_, ok := vq.reverseDepMap[id]
	if ok {
		return true
	}
	return false
}

// handles all fetched blocks
// this handler is passed to fetchQueue which is responsible for triggering the call
func (vq *blockQueue) handleBlocks(bjb fetchJob) {
	mp := map[types.Hash32]*types.Block{}
	for _, item := range bjb.items {
		tmp := item.(*types.Block)
		mp[item.Hash32()] = tmp
	}

	for _, id := range bjb.ids {
		b, ok := mp[id]
		if !ok {
			vq.updateDependencies(id, false)
			vq.Error("could not retrieve a block in view %v", id.ShortString())
			continue
		}

		go vq.handleBlock(id, b)
	}

}

func (vq *blockQueue) handleBlock(id types.Hash32, block *types.Block) {
	vq.Info("start handle block %s miner id %s", block.ID(), block.MinerID().ShortString())
	if err := vq.fastValidation(block); err != nil {
		vq.Error("block validation failed", log.BlockID(block.ID().String()), log.Err(err))
		vq.updateDependencies(id, false)
		return
	}
	vq.Info("finish fast validation block %v", block.ID())
	vq.handleBlockDependencies(block)
}

// handles new block dependencies
// if there are unknown blocks in the view they are added to the fetch queue
func (vq *blockQueue) handleBlockDependencies(blk *types.Block) {
	vq.Debug("handle dependencies Block %v", blk.ID())
	res, err := vq.addDependencies(blk.ID(), blk.ViewEdges, vq.finishBlockCallback(blk))

	if err != nil {
		vq.updateDependencies(blk.Hash32(), false)
		vq.Error(fmt.Sprintf("failed to add pending for Block %v %v", blk.ID(), err))
		return
	}

	if res == false {
		vq.Debug("pending done for %v", blk.ID())
		vq.updateDependencies(blk.Hash32(), true)
	}
	vq.Debug("added %v dependencies to queue", blk.ID())
}

func (vq *blockQueue) finishBlockCallback(block *types.Block) func(res bool) error {
	return func(res bool) error {
		if !res {
			vq.Info("finished block %v block, invalid", block.ID())
			return nil
		}

		//data availability
		txs, atxs, err := vq.dataAvailability(block)
		if err != nil {
			return fmt.Errorf("DataAvailabilty failed for block %v err: %v", block.ID(), err)
		}

		//validate block's votes
		if valid, err := validateVotes(block, vq.ForBlockInView, vq.Hdist, vq.Log); valid == false || err != nil {
			return fmt.Errorf("validate votes failed for block %s %s", block.ID(), err)
		}

		err = vq.AddBlockWithTxs(block, txs, atxs)

		if err != nil && err != mesh.ErrAlreadyExist {
			return err
		}

		//run late block through tortoise only if its new to us
		if (block.Layer() <= vq.ProcessedLayer() || block.Layer() == vq.getValidatingLayer()) && err != mesh.ErrAlreadyExist {
			vq.HandleLateBlock(block)
		}

		vq.Info("finished block %v, valid", block.ID())
		return nil
	}
}

// removes all dependencies for are block
func (vq *blockQueue) updateDependencies(block types.Hash32, valid bool) {
	vq.Debug("invalidate %v", block.ShortString())
	vq.Lock()
	//clean after block
	delete(vq.depMap, block)
	delete(vq.callbacks, block)
	vq.Unlock()

	doneQueue := make([]types.Hash32, 0, len(vq.depMap))
	doneQueue = vq.removefromDepMaps(block, valid, doneQueue)
	for {
		if len(doneQueue) == 0 {
			break
		}
		block = doneQueue[0]
		doneQueue = doneQueue[1:]
		doneQueue = vq.removefromDepMaps(block, valid, doneQueue)
	}
}

// removes block from dependencies maps and calls the blocks callback\
// dependencies can be of type block/layer
// for block jobs we need to return  a list of finished blocks
func (vq *blockQueue) removefromDepMaps(block types.Hash32, valid bool, doneBlocks []types.Hash32) []types.Hash32 {
	vq.fetchQueue.invalidate(block, valid)
	vq.Lock()
	defer vq.Unlock()
	for _, dep := range vq.reverseDepMap[block] {
		delete(vq.depMap[dep], block)
		if len(vq.depMap[dep]) == 0 {
			delete(vq.depMap, dep)
			vq.Debug("run callback for %v, %v", dep, reflect.TypeOf(dep))
			if callback, ok := vq.callbacks[dep]; ok {
				delete(vq.callbacks, dep)
				if err := callback(valid); err != nil {
					vq.Error(" %v callback Failed %v", dep, err)
					continue
				}
				switch id := dep.(type) {
				case types.BlockID:
					doneBlocks = append(doneBlocks, id.AsHash32())
				}
			}
		}
	}
	delete(vq.reverseDepMap, block)
	return doneBlocks
}

func (vq *blockQueue) addDependencies(jobID interface{}, blks []types.BlockID, finishCallback func(res bool) error) (bool, error) {

	defer vq.shutdownRecover()

	vq.Lock()
	if _, ok := vq.callbacks[jobID]; ok {
		vq.Unlock()
		return false, fmt.Errorf("job %s already exsits", jobID)
	}

	dependencys := make(map[types.Hash32]struct{})
	idsToPush := make([]types.Hash32, 0, len(blks))
	for _, id := range blks {
		bid := id.AsHash32()
		if vq.inQueue(bid) {
			vq.reverseDepMap[bid] = append(vq.reverseDepMap[bid], jobID)
			vq.Debug("add block %v to %v pending map", id, jobID)
			dependencys[bid] = struct{}{}
		} else {
			//	check database
			if _, err := vq.GetBlock(id); err != nil {
				//unknown block add to queue
				vq.reverseDepMap[bid] = append(vq.reverseDepMap[bid], jobID)
				vq.Debug("add block %v to %v pending map", id, jobID)
				dependencys[bid] = struct{}{}
				idsToPush = append(idsToPush, id.AsHash32())
			}
		}
	}

	//if no missing dependencies return
	if len(dependencys) == 0 {
		vq.Unlock()
		return false, finishCallback(true)
	}

	//add callback to job
	vq.callbacks[jobID] = finishCallback

	//add dependencies to job
	vq.depMap[jobID] = dependencys

	// addToPending needs the mutex so we must release before
	vq.Unlock()
	if len(idsToPush) > 0 {
		vq.Debug("add %v to queue %v", len(idsToPush), jobID)
		vq.addToPending(idsToPush)
	}

	vq.Debug("added %v dependencies to %s", len(dependencys), jobID)
	return true, nil
}
