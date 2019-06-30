package sync

import (
	"github.com/spacemeshos/go-spacemesh/common/prque"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
)

type PQueue struct {
	*sync.Cond
	*prque.Prque
}

func NewPQueue() *PQueue {
	return &PQueue{Cond: sync.NewCond(new(sync.Mutex)), Prque: prque.New(nil)}
}

// Pop (waits until anything available)
func (h *PQueue) Pop() int {
	h.L.Lock()
	defer h.L.Unlock()
	for h.Size() == 0 {
		h.Wait()
	}
	return h.Pop()
}

func (h *PQueue) Push(x types.BlockID, lyr types.LayerID) {
	defer h.Signal()
	h.L.Lock()
	defer h.L.Unlock()
	h.Prque.Push(x, int64(lyr))
}

type validationQueue struct {
	*PQueue
	depMap        map[types.BlockID]map[types.BlockID]struct{}
	reverseDepMap map[types.BlockID][]types.BlockID
}

func NewValidationQueue(blk *types.Block) *validationQueue {
	vq := &validationQueue{PQueue: NewPQueue(),
		depMap:        make(map[types.BlockID]map[types.BlockID]struct{}),
		reverseDepMap: make(map[types.BlockID][]types.BlockID)}
	vq.Push(blk.ID(), blk.Layer())
	return vq
}

func (vq *validationQueue) updateDependencies(bHeader *types.BlockHeader, checkDatabase func(id types.BlockID) (*types.Block, error)) bool {
	var dependencys map[types.BlockID]struct{}
	for _, block := range bHeader.ViewEdges {
		//if unknown block
		if _, err := checkDatabase(block); err != nil {
			//add to queue
			vq.Push(block, bHeader.Layer())
			//init dependency set
			dependencys = make(map[types.BlockID]struct{})
			dependencys[block] = struct{}{}
		}
	}

	if len(dependencys) == 0 {
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
