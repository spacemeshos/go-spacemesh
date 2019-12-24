package miner

import (
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type AtxMemPool struct {
	mu       sync.RWMutex
	atxMap   map[types.AtxId]*types.ActivationTx
	listener func(header *types.ActivationTxHeader)
}

func NewAtxMemPool() *AtxMemPool {
	return &AtxMemPool{atxMap: make(map[types.AtxId]*types.ActivationTx)}
}

func (mem *AtxMemPool) Get(id types.AtxId) (*types.ActivationTx, error) {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	atx, ok := mem.atxMap[id]
	if !ok {
		return nil, fmt.Errorf("cannot find ATX in mempool")
	}
	return atx, nil
}

func (mem *AtxMemPool) GetAllItems() []*types.ActivationTx {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	var atxList []*types.ActivationTx
	for _, k := range mem.atxMap {
		atxList = append(atxList, k)
	}
	return atxList
}

func (mem *AtxMemPool) Put(atx *types.ActivationTx) {
	mem.mu.Lock()
	mem.atxMap[atx.Id()] = atx
	if mem.listener != nil {
		go mem.listener(atx.ActivationTxHeader)
	}
	mem.mu.Unlock()
}

func (mem *AtxMemPool) Invalidate(id types.AtxId) {
	mem.mu.Lock()
	defer mem.mu.Unlock()
	delete(mem.atxMap, id)
}

func (mem *AtxMemPool) SetListener(listener func(header *types.ActivationTxHeader)) {
	mem.mu.Lock()
	defer mem.mu.Unlock()
	mem.listener = listener
}
