package activation

import (
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// AtxMemPool is a memory store that holds all received ATXs from gossip network by their ids
type AtxMemPool struct {
	mu     sync.RWMutex
	atxMap map[types.ATXID]*types.ActivationTx
}

// NewAtxMemPool creates a struct holding atxs by id
func NewAtxMemPool() *AtxMemPool {
	return &AtxMemPool{atxMap: make(map[types.ATXID]*types.ActivationTx)}
}

// Get retrieves the atx by the provided id id, it returns a reference to the found atx struct or an error if not
func (mem *AtxMemPool) Get(id types.ATXID) (*types.ActivationTx, error) {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	atx, ok := mem.atxMap[id]
	if !ok {
		return nil, fmt.Errorf("cannot find ATX in mempool")
	}
	return atx, nil
}

// GetAllItems creates and returns a list of all items found in cache
func (mem *AtxMemPool) GetAllItems() []*types.ActivationTx {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	var atxList []*types.ActivationTx
	for _, k := range mem.atxMap {
		atxList = append(atxList, k)
	}
	return atxList
}

// Put insets an atx into the mempool
func (mem *AtxMemPool) Put(atx *types.ActivationTx) {
	mem.mu.Lock()
	mem.atxMap[atx.ID()] = atx
	mem.mu.Unlock()
}

// Invalidate removes the provided atx by its id. it does not return error if id is not found
func (mem *AtxMemPool) Invalidate(id types.ATXID) {
	mem.mu.Lock()
	defer mem.mu.Unlock()
	delete(mem.atxMap, id)
}
