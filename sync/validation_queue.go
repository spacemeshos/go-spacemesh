package sync

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"reflect"
	"sync"
)

type ValidationInfra interface {
	DataAvailabilty(blk *types.Block) ([]*types.AddressableSignedTransaction, []*types.ActivationTx, error)
	AddBlockWithTxs(blk *types.Block, txs []*types.AddressableSignedTransaction, atxs []*types.ActivationTx) error
	GetBlock(id types.BlockID) (*types.Block, error)
	ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) (bool, error)) error
	fastValidation(block *types.Block) error
	log.Logger
}

type blockQueue struct {
	Configuration
	ValidationInfra
	fetchQueue
	callbacks     map[interface{}]func(res bool) error
	depMap        map[interface{}]map[types.BlockID]struct{}
	reverseDepMap map[types.BlockID][]interface{}
	visited       map[types.BlockID]struct{}
}

func NewValidationQueue(srvr WorkerInfra, conf Configuration, msh ValidationInfra, checkLocal CheckLocalFunc, lg log.Log) *blockQueue {
	vq := &blockQueue{
		fetchQueue: fetchQueue{
			Log:                 srvr.WithName("atxFetchQueue"),
			workerInfra:         srvr,
			checkLocal:          checkLocal,
			BatchRequestFactory: BlockFetchReqFactory,
			Mutex:               &sync.Mutex{},
			pending:             make(map[types.Hash32][]chan bool),
			queue:               make(chan []types.Hash32, 1000),
		},
		Configuration:   conf,
		visited:         make(map[types.BlockID]struct{}),
		depMap:          make(map[interface{}]map[types.BlockID]struct{}),
		reverseDepMap:   make(map[types.BlockID][]interface{}),
		callbacks:       make(map[interface{}]func(res bool) error),
		ValidationInfra: msh,
	}
	vq.handleFetch = vq.handleBlock
	go vq.work()

	return vq
}

func (vq *blockQueue) inQueue(id types.BlockID) bool {
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

func (vq *blockQueue) handleBlock(bjb fetchJob) {
	//todo movem to batch block request
	bid := types.BlockID(util.BytesToUint64(bjb.ids[0].Bytes()))
	if bjb.items == nil {
		//no items fetched
		vq.updateDependencies(bid, false)
		vq.Error(fmt.Sprintf("could not retrieve a block in view "))
		//return
	}
	block := bjb.items[0].(*types.Block)
	//todo hack remove after batch block request is implemented in request and handler
	vq.Info("fetched  %v", bid)
	vq.visited[bid] = struct{}{}
	if err := vq.fastValidation(block); err != nil {
		vq.Error("ValidationQueue: block validation failed", log.BlockId(uint64(block.ID())), log.Err(err))
		vq.updateDependencies(bid, false)
		//return
	}

	vq.handleBlockDependencies(block)
	//todo better deadlock solution
}

func (vq *blockQueue) handleBlockDependencies(blk *types.Block) {
	vq.Info("Validating view Block %v", blk.ID())
	res, err := vq.addDependencies(blk.ID(), blk.ViewEdges, vq.finishBlockCallback(blk))
	if err != nil {
		vq.updateDependencies(blk.ID(), false)
		vq.Error(fmt.Sprintf("failed to add pending for Block %v %v", blk.ID(), err))
		//continue
	}
	if res == false {
		vq.Info("pending done for %v", blk.ID())
		vq.updateDependencies(blk.ID(), true)
	}
}

func (vq *blockQueue) finishBlockCallback(block *types.Block) func(res bool) error {
	return func(res bool) error {
		if !res {
			vq.Info("finished block %v block invalid", block.ID())
			return nil
		}

		//data availability
		txs, atxs, err := vq.DataAvailabilty(block)
		if err != nil {
			return fmt.Errorf("DataAvailabilty failed for block %v err: %v", block, err)
		}

		//validate block's votes
		if valid := validateVotes(block, vq.ForBlockInView, vq.Hdist); valid == false {
			return errors.New(fmt.Sprintf("validate votes failed for block %v", block.ID()))
		}

		if err := vq.AddBlockWithTxs(block, txs, atxs); err != nil && err != mesh.ErrAlreadyExist {
			return err
		}

		return nil
	}
}

func (vq *blockQueue) updateDependencies(block types.BlockID, valid bool) {
	vq.Lock()
	defer vq.Unlock()
	var doneBlocks []types.BlockID
	doneQueue := make([]types.BlockID, 0, len(vq.depMap))
	doneQueue = vq.removefromDepMaps(block, valid, doneQueue)
	for {
		if len(doneQueue) == 0 {
			break
		}
		block = doneQueue[0]
		doneQueue = doneQueue[1:]
		doneBlocks = append(doneBlocks, block)
		doneQueue = vq.removefromDepMaps(block, valid, doneQueue)
	}
}

func (vq *blockQueue) removefromDepMaps(block types.BlockID, valid bool, doneBlocks []types.BlockID) []types.BlockID {
	//clean after block
	delete(vq.depMap, block)
	delete(vq.callbacks, block)
	delete(vq.visited, block)

	for _, dep := range vq.reverseDepMap[block] {
		delete(vq.depMap[dep], block)
		if len(vq.depMap[dep]) == 0 {
			delete(vq.depMap, dep)
			vq.Info("run callback for %v, %v", dep, reflect.TypeOf(dep))
			if callback, ok := vq.callbacks[dep]; ok {
				if err := callback(valid); err != nil {
					vq.Error(" %v callback Failed", dep)
					continue
				}
				delete(vq.callbacks, dep)
				switch id := dep.(type) {
				case types.BlockID:
					doneBlocks = append(doneBlocks, id)
				}
			}
		}
	}
	delete(vq.reverseDepMap, block)
	return doneBlocks
}

func (vq *blockQueue) addDependencies(jobId interface{}, blks []types.BlockID, finishCallback func(res bool) error) (bool, error) {
	vq.Lock()
	vq.callbacks[jobId] = finishCallback
	dependencys := make(map[types.BlockID]struct{})
	idsToPush := make([]types.Hash32, 0, len(blks))
	for _, id := range blks {
		if vq.inQueue(id) {
			vq.reverseDepMap[id] = append(vq.reverseDepMap[id], jobId)
			vq.Info("add block %v to %v pending map", id, jobId)
			dependencys[id] = struct{}{}
		} else {
			//	check database
			if _, err := vq.GetBlock(id); err != nil {
				//unknown block add to queue
				vq.reverseDepMap[id] = append(vq.reverseDepMap[id], jobId)
				vq.Info("add block %v to %v pending map", id, jobId)
				dependencys[id] = struct{}{}
				idsToPush = append(idsToPush, id.AsHash32())
			}
		}
	}
	vq.Unlock()

	for _, id := range idsToPush {
		vq.addToPending([]types.Hash32{id})
	}

	//todo better this is a little hacky
	if len(dependencys) == 0 {
		return false, finishCallback(true)
	}

	vq.depMap[jobId] = dependencys
	return true, nil
}
