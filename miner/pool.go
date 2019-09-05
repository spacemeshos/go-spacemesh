package miner

import (
	"fmt"
	"github.com/cheekybits/genny/generic"
	"sync"
)

//go:generate genny -in=$GOFILE -out=gen_atx_$GOFILE gen "Name=Atx KeyType=types.AtxId ValueType=types.ActivationTx"
//go:generate genny -in=$GOFILE -out=gen_tx_$GOFILE gen "Name=Tx KeyType=types.TransactionId ValueType=types.Transaction"

type KeyType generic.Type
type ValueType generic.Type

type NameMemPool struct {
	mu    sync.RWMutex
	txMap map[KeyType]*ValueType
}

func NewNameMemPool() *NameMemPool {
	return &NameMemPool{sync.RWMutex{}, make(map[KeyType]*ValueType)}
}

func (mem *NameMemPool) Get(id KeyType) (ValueType, error) {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	val, ok := mem.txMap[id]
	if !ok {
		return *new(ValueType), fmt.Errorf("Cannot find in mempool")
	}
	return *val, nil
}

func (mem *NameMemPool) GetAllItems() []ValueType {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	var txList []ValueType
	for _, k := range mem.txMap {
		txList = append(txList, *k)
	}
	return txList
}

func (mem *NameMemPool) Put(id KeyType, item *ValueType) {
	mem.mu.Lock()
	mem.txMap[id] = item
	mem.mu.Unlock()
}

func (mem *NameMemPool) Invalidate(id KeyType) {
	mem.mu.Lock()
	defer mem.mu.Unlock()
	delete(mem.txMap, id)
}
