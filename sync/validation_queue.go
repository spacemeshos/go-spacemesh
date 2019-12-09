package sync

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"reflect"
	"sync"
)

type ValidationInfra interface {
	DataAvailability(blk *types.Block) ([]*types.Transaction, []*types.ActivationTx, error)
	AddBlockWithTxs(blk *types.Block, txs []*types.Transaction, atxs []*types.ActivationTx) error
	GetBlock(id types.BlockID) (*types.Block, error)
	ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) (bool, error)) error
	fastValidation(block *types.Block) error
	HandleLateBlock(bl *types.Block)
	ValidatedLayer() types.LayerID
}

type blockQueue struct {
	Configuration
	ValidationInfra
	fetchQueue
	callbacks     map[interface{}]func(res bool) error
	depMap        map[interface{}]map[types.Hash32]struct{}
	reverseDepMap map[types.Hash32][]interface{}
}

func NewValidationQueue(srvr WorkerInfra, conf Configuration, msh ValidationInfra, checkLocal CheckLocalFunc, lg log.Log) *blockQueue {
	vq := &blockQueue{
		fetchQueue: fetchQueue{
			Log:                 srvr.WithName("blockFetchQueue"),
			workerInfra:         srvr,
			checkLocal:          checkLocal,
			BatchRequestFactory: BlockFetchReqFactory,
			Mutex:               &sync.Mutex{},
			pending:             make(map[types.Hash32][]chan bool),
			queue:               make(chan []types.Hash32, 1000),
			name:                "Block",
		},
		Configuration:   conf,
		depMap:          make(map[interface{}]map[types.Hash32]struct{}),
		reverseDepMap:   make(map[types.Hash32][]interface{}),
		callbacks:       make(map[interface{}]func(res bool) error),
		ValidationInfra: msh,
	}
	vq.handleFetch = vq.handleBlock
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
func (vq *blockQueue) handleBlock(bjb fetchJob) {
	mp := map[types.Hash32]*types.Block{}
	for _, item := range bjb.items {
		tmp := item.(*types.Block)
		mp[item.Hash32()] = tmp
	}

	for _, id := range bjb.ids {

		block, found := mp[id]
		if !found {
			vq.updateDependencies(id, false)
			vq.Error("could not retrieve a block in view %v", id.ShortString())
			continue
		}

		vq.Info("fetched  %v", block.Id())
		if err := vq.fastValidation(block); err != nil {
			vq.Error("block validation failed", log.BlockId(block.Id().String()), log.Err(err))
			vq.updateDependencies(id, false)
			continue
		}

		vq.handleBlockDependencies(block)
		//todo better deadlock solution
	}

}

// handles new block dependencies
// if there are unknown blocks in the view they are added to the fetch queue
func (vq *blockQueue) handleBlockDependencies(blk *types.Block) {
	vq.Info("handle dependencies Block %v", blk.Id())
	res, err := vq.addDependencies(blk.Id(), blk.ViewEdges, vq.finishBlockCallback(blk))

	if err != nil {
		vq.updateDependencies(blk.Hash32(), false)
		vq.Error(fmt.Sprintf("failed to add pending for Block %v %v", blk.Id(), err))
		return
	}

	if res == false {
		vq.Info("pending done for %v", blk.Id())
		vq.updateDependencies(blk.Hash32(), true)
	}
}

func (vq *blockQueue) finishBlockCallback(block *types.Block) func(res bool) error {
	return func(res bool) error {
		if !res {
			vq.Info("finished block %v block, invalid", block.Id())
			return nil
		}

		//data availability
		txs, atxs, err := vq.DataAvailability(block)
		if err != nil {
			return fmt.Errorf("DataAvailabilty failed for block %v err: %v", block.Id(), err)
		}

		//validate block's votes
		if valid, err := validateVotes(block, vq.ForBlockInView, vq.Hdist, vq.Log); valid == false || err != nil {
			return errors.New(fmt.Sprintf("validate votes failed for block %s %s", block.Id(), err))
		}

		err = vq.AddBlockWithTxs(block, txs, atxs)

		if err != nil && err != mesh.ErrAlreadyExist {
			return err
		}

		//run late block through tortoise only if its new to us
		if block.Layer() <= vq.ValidatedLayer() && err != mesh.ErrAlreadyExist {
			vq.HandleLateBlock(block)
		}

		vq.Info("finished block %v, valid", block.Id())
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

func (vq *blockQueue) addDependencies(jobId interface{}, blks []types.BlockID, finishCallback func(res bool) error) (bool, error) {
	vq.Lock()
	if _, ok := vq.callbacks[jobId]; ok {
		vq.Unlock()
		return false, fmt.Errorf("job %s already exsits", jobId)
	}

	vq.callbacks[jobId] = finishCallback
	dependencys := make(map[types.Hash32]struct{})
	idsToPush := make([]types.Hash32, 0, len(blks))
	for _, id := range blks {
		bid := id.AsHash32()
		if vq.inQueue(bid) {
			vq.reverseDepMap[bid] = append(vq.reverseDepMap[bid], jobId)
			vq.Info("add block %v to %v pending map", id, jobId)
			dependencys[bid] = struct{}{}
		} else {
			//	check database
			if _, err := vq.GetBlock(id); err != nil {
				//unknown block add to queue
				vq.reverseDepMap[bid] = append(vq.reverseDepMap[bid], jobId)
				vq.Info("add block %v to %v pending map", id, jobId)
				dependencys[bid] = struct{}{}
				idsToPush = append(idsToPush, id.AsHash32())
			}
		}
	}

	// addToPending needs the mutex so we must release before
	vq.Unlock()
	if len(idsToPush) > 0 {
		vq.Debug("add %v to queue %v", len(idsToPush), jobId)
		vq.addToPending(idsToPush)
	}

	//lock to protect depMap and callback map
	vq.Lock()
	defer vq.Unlock()
	//todo better this is a little hacky
	if len(dependencys) == 0 {
		delete(vq.callbacks, jobId)
		return false, finishCallback(true)
	}

	vq.depMap[jobId] = dependencys
	return true, nil
}
