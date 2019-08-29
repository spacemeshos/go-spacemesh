// This file was automatically generated by genny.
// Any changes will be lost if this file is regenerated.
// see https://github.com/cheekybits/genny

package miner

import (
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type TxMemPool struct {
	mu    sync.RWMutex
	txMap map[types.TransactionId]*types.AddressableSignedTransaction
}

func NewTxMemPool() *TxMemPool {
	return &TxMemPool{sync.RWMutex{}, make(map[types.TransactionId]*types.AddressableSignedTransaction)}
}

func (mem *TxMemPool) Get(id types.TransactionId) (types.AddressableSignedTransaction, error) {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	val, ok := mem.txMap[id]
	if !ok {
		return *new(types.AddressableSignedTransaction), fmt.Errorf("Cannot find in mempool")
	}
	return *val, nil
}

func (mem *TxMemPool) GetAllItems() []types.AddressableSignedTransaction {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	var txList []types.AddressableSignedTransaction
	for _, k := range mem.txMap {
		txList = append(txList, *k)
	}
	return txList
}

func (mem *TxMemPool) Put(id types.TransactionId, item *types.AddressableSignedTransaction) {
	mem.mu.Lock()
	mem.txMap[id] = item
	mem.mu.Unlock()
}

func (mem *TxMemPool) Invalidate(id types.TransactionId) {
	mem.mu.Lock()
	defer mem.mu.Unlock()
	delete(mem.txMap, id)
}
