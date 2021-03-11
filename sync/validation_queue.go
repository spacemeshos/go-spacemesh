package sync

import (
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"reflect"
	"sync"
)

type syncer interface {
	AddBlockWithTxs(blk *types.Block) error
	GetBlock(id types.BlockID) (*types.Block, error)
	ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) (bool, error)) error
	HandleLateBlock(bl *types.Block)
	ProcessedLayer() types.LayerID
	dataAvailability(context.Context, *types.Block) ([]*types.Transaction, []*types.ActivationTx, error)
	getValidatingLayer() types.LayerID
	fastValidation(*types.Block) error
	blockCheckLocal(ctx context.Context, blockIds []types.Hash32) (map[types.Hash32]item, map[types.Hash32]item, []types.Hash32)
	fetchBlockDataForValidation(context.Context, *types.Block) error
}

type blockQueue struct {
	syncer
	fetchQueue
	Configuration
	callbacks     map[interface{}]func(ctx context.Context, res bool) error
	depMap        map[interface{}]map[types.Hash32]struct{}
	reverseDepMap map[types.Hash32][]interface{}
}

func newValidationQueue(ctx context.Context, srvr networker, conf Configuration, sy syncer) *blockQueue {
	vq := &blockQueue{
		fetchQueue: fetchQueue{
			Log:                 srvr.WithName("blockFetchQueue"),
			workerInfra:         srvr,
			checkLocal:          sy.blockCheckLocal,
			batchRequestFactory: blockFetchReqFactory,
			Mutex:               &sync.Mutex{},
			pending:             make(map[types.Hash32][]chan bool),
			queue:               make(chan fetchRequest, 1000),
			name:                "Block",
			requestTimeout:      conf.RequestTimeout,
		},
		Configuration: conf,
		depMap:        make(map[interface{}]map[types.Hash32]struct{}),
		reverseDepMap: make(map[types.Hash32][]interface{}),
		callbacks:     make(map[interface{}]func(ctx2 context.Context, res bool) error),
		syncer:        sy,
	}
	vq.handleFetch = vq.handleBlocks
	go vq.work(ctx)

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
func (vq *blockQueue) handleBlocks(ctx context.Context, bjb fetchJob) {
	mp := map[types.Hash32]*types.Block{}
	for _, item := range bjb.items {
		tmp := item.(*types.Block)
		mp[item.Hash32()] = tmp
	}

	for _, id := range bjb.ids {
		b, ok := mp[id]
		if !ok {
			vq.updateDependencies(ctx, id, false)
			vq.WithContext(ctx).With().Error("could not retrieve a block in view", id)
			continue
		}

		vq.WithContext(ctx).Info("starting handling blocks")
		go vq.handleBlock(log.WithNewRequestID(ctx, b.ID()), id, b)
	}
}

func (vq *blockQueue) handleBlock(ctx context.Context, id types.Hash32, block *types.Block) {
	logger := vq.WithContext(ctx).WithFields(block.ID())
	logger.With().Info("start handling block", block.Fields()...)
	if err := vq.fetchBlockDataForValidation(ctx, block); err != nil {
		logger.With().Error("fetching block data failed", log.Err(err))
		vq.updateDependencies(ctx, id, false)
		return
	}
	if err := vq.fastValidation(block); err != nil {
		logger.With().Error("block fast validation failed", log.Err(err))
		vq.updateDependencies(ctx, id, false)
		return
	}
	logger.Info("finished block fast validation")
	vq.handleBlockDependencies(ctx, block)
}

// handles new block dependencies
// if there are unknown blocks in the view they are added to the fetch queue
func (vq *blockQueue) handleBlockDependencies(ctx context.Context, blk *types.Block) {
	logger := vq.WithContext(ctx).WithFields(blk.ID())
	logger.Debug("handle block dependencies")
	res, err := vq.addDependencies(ctx, blk.ID(), blk.ViewEdges, vq.finishBlockCallback(blk))

	if err != nil {
		vq.updateDependencies(ctx, blk.Hash32(), false)
		logger.With().Error("failed to add dependencies", log.Err(err))
		return
	}

	if res == false {
		logger.With().Debug("pending done", blk.ID())
		vq.updateDependencies(ctx, blk.Hash32(), true)
	}
	logger.Debug("added dependencies to queue")
}

func (vq *blockQueue) finishBlockCallback(block *types.Block) func(ctx context.Context, res bool) error {
	return func(ctx context.Context, res bool) error {
		logger := vq.WithContext(ctx).WithFields(block.ID())
		if !res {
			logger.Info("finished block, invalid")
			return nil
		}

		// data availability
		_, _, err := vq.dataAvailability(ctx, block)
		if err != nil {
			return fmt.Errorf("DataAvailabilty failed for block: %v errmsg: %v", block.ID().String(), err)
		}

		// validate block's votes
		if valid, err := validateVotes(block, vq.ForBlockInView, vq.Hdist); valid == false || err != nil {
			return fmt.Errorf("validate votes failed for block: %s errmsg: %s", block.ID().String(), err)
		}

		err = vq.AddBlockWithTxs(block)

		if err != nil && err != mesh.ErrAlreadyExist {
			return err
		}

		// run late block through tortoise only if its new to us
		if (block.Layer() <= vq.ProcessedLayer() || block.Layer() == vq.getValidatingLayer()) && err != mesh.ErrAlreadyExist {
			vq.HandleLateBlock(block)
		}

		logger.Info("finished block, valid")
		return nil
	}
}

// removes all dependencies for a block
func (vq *blockQueue) updateDependencies(ctx context.Context, block types.Hash32, valid bool) {
	vq.WithContext(ctx).With().Debug("invalidate block", block)
	vq.Lock()
	//clean after block
	delete(vq.depMap, block)
	delete(vq.callbacks, block)
	vq.Unlock()

	doneQueue := make([]types.Hash32, 0, len(vq.depMap))
	doneQueue = vq.removefromDepMaps(ctx, block, valid, doneQueue)
	for {
		if len(doneQueue) == 0 {
			break
		}
		block = doneQueue[0]
		doneQueue = doneQueue[1:]
		doneQueue = vq.removefromDepMaps(ctx, block, valid, doneQueue)
	}
}

// removes block from dependencies maps and calls the blocks callback\
// dependencies can be of type block/layer
// for block jobs we need to return  a list of finished blocks
func (vq *blockQueue) removefromDepMaps(ctx context.Context, block types.Hash32, valid bool, doneBlocks []types.Hash32) []types.Hash32 {
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
				if err := callback(ctx, valid); err != nil {
					vq.Error("%v callback failed: %v", dep, err)
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

func (vq *blockQueue) addDependencies(ctx context.Context, jobID interface{}, blks []types.BlockID, finishCallback func(ctx context.Context, res bool) error) (bool, error) {
	defer vq.shutdownRecover()
	logger := vq.WithContext(ctx).WithFields(log.String("job_id", fmt.Sprint(jobID)))

	vq.Lock()
	if _, ok := vq.callbacks[jobID]; ok {
		vq.Unlock()
		return false, fmt.Errorf("job %s already exsits", jobID)
	}

	dependencies := make(map[types.Hash32]struct{})
	idsToPush := make([]types.Hash32, 0, len(blks))
	for _, id := range blks {
		bid := id.AsHash32()
		if vq.inQueue(bid) {
			vq.reverseDepMap[bid] = append(vq.reverseDepMap[bid], jobID)
			logger.With().Debug("adding already queued block to pending map", id)
			dependencies[bid] = struct{}{}
		} else {
			//	check database
			if _, err := vq.GetBlock(id); err != nil {
				// add unknown block to queue
				vq.reverseDepMap[bid] = append(vq.reverseDepMap[bid], jobID)
				logger.With().Debug("adding unknown block to pending map", id)
				dependencies[bid] = struct{}{}
				idsToPush = append(idsToPush, id.AsHash32())
			}
		}
	}

	// if no missing dependencies return
	if len(dependencies) == 0 {
		vq.Unlock()
		return false, finishCallback(ctx, true)
	}

	// add callback to job
	vq.callbacks[jobID] = finishCallback

	// add dependencies to job
	vq.depMap[jobID] = dependencies

	// addToPending needs the mutex so we must release before
	vq.Unlock()
	if len(idsToPush) > 0 {
		logger.With().Debug("adding dependencies to pending queue",
			log.Int("count", len(idsToPush)))
		vq.addToPending(ctx, idsToPush)
	}

	logger.With().Debug("finished adding dependencies",
		log.Int("count", len(dependencies)))
	return true, nil
}
