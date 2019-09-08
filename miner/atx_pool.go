package miner

import (
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type AtxMemPool struct {
	mu    sync.RWMutex
	txMap map[types.AtxId]*types.ActivationTx
}

func NewAtxMemPool() *AtxMemPool {
	return &AtxMemPool{sync.RWMutex{}, make(map[types.AtxId]*types.ActivationTx)}
}

func (mem *AtxMemPool) Get(id types.AtxId) (types.ActivationTx, error) {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	val, ok := mem.txMap[id]
	if !ok {
		return *new(types.ActivationTx), fmt.Errorf("cannot find ATX in mempool")
	}
	return *val, nil
}

func (mem *AtxMemPool) GetAllItems() []types.ActivationTx {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	var txList []types.ActivationTx
	for _, k := range mem.txMap {
		txList = append(txList, *k)
	}
	return txList
}

func (mem *AtxMemPool) Put(id types.AtxId, item *types.ActivationTx) {
	mem.mu.Lock()
	mem.txMap[id] = item
	mem.mu.Unlock()
}

func (mem *AtxMemPool) Invalidate(id types.AtxId) {
	mem.mu.Lock()
	defer mem.mu.Unlock()
	delete(mem.txMap, id)
}
