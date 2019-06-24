package miner

import (
	"reflect"
	"sync"
)

func NewMemPool(tp reflect.Type) *MemPool {
	return &MemPool{sync.RWMutex{}, tp, make(map[interface{}]interface{})}
}

type MemPool struct {
	mu    sync.RWMutex
	t     reflect.Type
	txMap map[interface{}]interface{}
}

func (mem *MemPool) Get(id interface{}) interface{} {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	val, ok := mem.txMap[id]
	if !ok {
		return nil
	}
	return val
}

func (mem *MemPool) PopItems(size int) interface{} {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	txList := reflect.MakeSlice(mem.t, 0, MaxTransactionsPerBlock)
	for _, k := range mem.txMap {
		txList = reflect.Append(txList, reflect.ValueOf(k))
	}
	return txList.Interface()
}

func (mem *MemPool) Put(id interface{}, item interface{}) {
	mem.mu.Lock()
	defer mem.mu.Unlock()
	mem.txMap[id] = item
}

func (mem *MemPool) Invalidate(id interface{}) {
	mem.mu.Lock()
	defer mem.mu.Unlock()
	delete(mem.txMap, id)
}
