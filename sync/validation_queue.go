package sync

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/types"
	"reflect"
	"sync"
)

type dataAvailabilityFunc = func(blk *types.Block) ([]*types.AddressableSignedTransaction, []*types.ActivationTx, error)
type addBlockFunc func(blk *types.Block, txs []*types.AddressableSignedTransaction, atxs []*types.ActivationTx) error
type checkLocalFunc func(id types.BlockID) (*types.Block, error)

const (
	Layer JobType = 0
	Block JobType = 1
)

type JobId uint64
type JobType int

type Job struct {
	JobId
	JobType
}

type validationQueue struct {
	log.Log
	*MessageServer
	Configuration
	BlockValidator
	queue            chan interface{}
	mu               sync.Mutex
	callbacks        map[interface{}]func(res bool) error
	depMap           map[interface{}]map[types.BlockID]struct{}
	reverseDepMap    map[types.BlockID][]interface{}
	visited          map[types.BlockID]struct{}
	addBlock         addBlockFunc
	checkLocal       checkLocalFunc
	dataAvailability dataAvailabilityFunc
}

func NewValidationQueue(srvr *MessageServer, conf Configuration, bv BlockValidator, davail dataAvailabilityFunc, addBlock addBlockFunc, checkLocal checkLocalFunc, lg log.Log) *validationQueue {
	vq := &validationQueue{
		Configuration:    conf,
		MessageServer:    srvr,
		BlockValidator:   bv,
		queue:            make(chan interface{}, 2*conf.LayerSize),
		visited:          make(map[types.BlockID]struct{}),
		depMap:           make(map[interface{}]map[types.BlockID]struct{}),
		reverseDepMap:    make(map[types.BlockID][]interface{}),
		callbacks:        make(map[interface{}]func(res bool) error),
		dataAvailability: davail,
		addBlock:         addBlock,
		checkLocal:       checkLocal,
		Log:              lg,
	}

	go vq.work()

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

func (vq *validationQueue) work() error {

	output := fetchWithFactory(NewFetchWorker(vq.MessageServer, vq.Log, vq.Concurrency, BlockReqFactory(), vq.queue))
	for out := range output {
		bjb, ok := out.(fetchJob)
		if !ok {
			vq.Error(fmt.Sprintf("Type conversion err %v", out))
			continue
		}

		bid := bjb.ids.(types.BlockID)

		s.Info("Validating view for block %v", block.ID())
		if bjb.items == nil {
			vq.updateDependencies(bid, false)
			vq.Error(fmt.Sprintf("could not retrieve a block in view "))
			continue
		}

		vq.Info("fetched  %v", bid)
		blk := bjb.items.(*types.Block)
		vq.visited[bid] = struct{}{}
		if eligable, err := vq.BlockSignedAndEligible(blk); err != nil || !eligable {
			vq.updateDependencies(bid, false)
			vq.Error(fmt.Sprintf("Block %v eligiblety check failed %v", bid, err))
			continue
		}

		vq.handleBlockDependencies(blk) //todo better deadlock solution

		vq.Info("next block")
	}

	return nil
}

func (vq *validationQueue) handleBlockDependencies(blk *types.Block) {
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

		if err := vq.addBlock(block, txs, atxs); err != nil && err != mesh.DoubleWrite {
			return err
		}

		return nil
	}
}

func (vq *validationQueue) updateDependencies(block types.BlockID, valid bool) {
	vq.mu.Lock()
	defer vq.mu.Unlock()
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

func (vq *validationQueue) removefromDepMaps(block types.BlockID, valid bool, doneBlocks []types.BlockID) []types.BlockID {
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

func (vq *validationQueue) addDependencies(jobId interface{}, blks []types.BlockID, finishCallback func(res bool) error) (bool, error) {
	vq.mu.Lock()
	vq.callbacks[jobId] = finishCallback
	dependencys := make(map[types.BlockID]struct{})
	idsToPush := make([]types.BlockID, 0, len(blks))
	for _, id := range blks {
		if vq.inQueue(id) {
			vq.reverseDepMap[id] = append(vq.reverseDepMap[id], jobId)
			vq.Info("add block %v to %v pending map", id, jobId)
			dependencys[id] = struct{}{}
		} else {
			//	check database
			if _, err := vq.checkLocal(id); err != nil {
				//unknown block add to queue
				vq.reverseDepMap[id] = append(vq.reverseDepMap[id], jobId)
				vq.Info("add block %v to %v pending map", id, jobId)
				dependencys[id] = struct{}{}
				idsToPush = append(idsToPush, id)
			}
		}
	}
	vq.mu.Unlock()

	for _, id := range idsToPush {
		vq.queue <- id
	}

	//todo better this is a little hacky
	if len(dependencys) == 0 {
		return false, finishCallback(true)
	}

	vq.depMap[jobId] = dependencys
	return true, nil
}
