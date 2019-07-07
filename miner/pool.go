package miner

import (
	"fmt"
	"github.com/cheekybits/genny/generic"
	"sync"
)

//go:generate genny -in=$GOFILE -out=gen_tx_$GOFILE gen "KeyType=types.AtxId ValueType=types.ActivationTx"
//go:generate genny -in=$GOFILE -out=gen_atx_$GOFILE gen "KeyType=types.TransactionId ValueType=types.AddressableSignedTransaction"

type KeyType generic.Type
type ValueType generic.Type

type KeyTypeMemPool struct {
	mu    sync.RWMutex
	txMap map[KeyType]*ValueType
}

func NewKeyTypeMemPool() *KeyTypeMemPool {
	return &KeyTypeMemPool{sync.RWMutex{}, make(map[KeyType]*ValueType)}
}

func (mem *KeyTypeMemPool) Get(id KeyType) (ValueType, error) {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	val, ok := mem.txMap[id]
	if !ok {
		return *new(ValueType), fmt.Errorf("Cannot find in mempool")
	}
	return *val, nil
}

func (mem *KeyTypeMemPool) PopItems(size int) []ValueType {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	txList := make([]ValueType, 0, MaxTransactionsPerBlock)
	for _, k := range mem.txMap {
		txList = append(txList, *k)
	}
	return txList
}

func (mem *KeyTypeMemPool) Put(id KeyType, item *ValueType) {
	mem.mu.Lock()
	mem.txMap[id] = item
	mem.mu.Unlock()
}

func (mem *KeyTypeMemPool) Invalidate(id KeyType) {
	mem.mu.Lock()
	defer mem.mu.Unlock()
	delete(mem.txMap, id)
}
