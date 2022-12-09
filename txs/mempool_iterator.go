package txs

import (
	"container/heap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	minTXGas = uint64(1) // gas required for the most basic transaction
)

type item struct {
	*NanoTX

	// The index is needed by update and is maintained by the heap.Interface methods.
	index int // The index of the item in the heap.
}

type priorityQueue []*item

// Len implements head.Interface.
func (pq priorityQueue) Len() int { return len(pq) }

// Less implements head.Interface.
func (pq priorityQueue) Less(i, j int) bool {
	// We want Pop to give us the highest, not lowest, fee, so we use greater than here.
	return pq[i].Fee() > pq[j].Fee() || (pq[i].Fee() == pq[j].Fee() && pq[i].Received.Before(pq[j].Received))
}

// Swap implements head.Interface.
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

// Push implements head.Interface.
func (pq *priorityQueue) Push(i any) {
	n := len(*pq)
	it := i.(*item)
	it.index = n
	*pq = append(*pq, it)
}

// Pop implements head.Interface.
func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	it := old[n-1]
	old[n-1] = nil // avoid memory leak
	it.index = -1  // for safety
	*pq = old[0 : n-1]
	return it
}

// update modifies the fee and value of an item in the queue.
func (pq *priorityQueue) update(it *item, ntx *NanoTX) {
	it.NanoTX = ntx
	heap.Fix(pq, it.index)
}

// mempoolIterator holds the best transaction from the conservative state mempool.
// not thread-safe.
type mempoolIterator struct {
	logger       log.Log
	gasRemaining uint64
	pq           priorityQueue
	txs          map[types.Address][]*NanoTX
}

// newMempoolIterator builds and returns a mempoolIterator.
func newMempoolIterator(logger log.Log, cs conStateCache, gasLimit uint64) *mempoolIterator {
	txs := cs.GetMempool(logger)
	mi := &mempoolIterator{
		logger:       logger,
		gasRemaining: gasLimit,
		pq:           make(priorityQueue, 0, len(txs)),
		txs:          txs,
	}
	logger.With().Info("received mempool txs", log.Int("num_accounts", len(txs)))
	mi.buildPQ()
	return mi
}

func (mi *mempoolIterator) buildPQ() {
	i := 0
	for addr, ntxs := range mi.txs {
		ntx := ntxs[0]
		it := &item{
			NanoTX: ntx,
			index:  i,
		}
		i++
		mi.pq = append(mi.pq, it)
		mi.logger.With().Debug("adding item to pq",
			ntx.ID,
			ntx.Principal,
			log.Uint64("fee", ntx.Fee()),
			log.Uint64("gas", ntx.MaxGas),
			log.Time("received", ntx.Received))

		if len(ntxs) == 1 {
			delete(mi.txs, addr)
		} else {
			mi.txs[addr] = ntxs[1:]
		}
	}
	heap.Init(&mi.pq)
}

func (mi *mempoolIterator) getNext(addr types.Address) *NanoTX {
	if _, ok := mi.txs[addr]; !ok {
		return nil
	}
	ntx := mi.txs[addr][0]
	if len(mi.txs[addr]) == 1 {
		delete(mi.txs, addr)
	} else {
		mi.txs[addr] = mi.txs[addr][1:]
	}
	return ntx
}

func (mi *mempoolIterator) pop() *NanoTX {
	if mi.pq.Len() == 0 || mi.gasRemaining < minTXGas {
		return nil
	}

	var top *item
	for mi.pq.Len() > 0 {
		// the first item in priority queue is always the item to be popped with the heap
		top = mi.pq[0]
		if top.MaxGas <= mi.gasRemaining {
			break
		}
		mi.logger.With().Debug("tx max gas too high, removing addr from mempool",
			top.ID,
			top.Principal,
			log.Uint64("fee", top.Fee()),
			log.Uint64("gas", top.MaxGas),
			log.Uint64("gas_left", mi.gasRemaining),
			log.Time("received", top.Received))
		_ = heap.Pop(&mi.pq)
		// remove all txs for this principal since we cannot fulfill the lowest nonce for this principal
		delete(mi.txs, top.Principal)
		top = nil
	}

	if top == nil {
		return nil
	}

	ntx := top.NanoTX
	mi.gasRemaining -= ntx.MaxGas
	mi.logger.With().Debug("popping tx",
		ntx.ID,
		ntx.Principal,
		log.Uint64("fee", ntx.Fee()),
		log.Uint64("gas_used", ntx.MaxGas),
		log.Uint64("gas_left", mi.gasRemaining),
		log.Time("received", ntx.Received))
	next := mi.getNext(ntx.Principal)
	if next == nil {
		mi.logger.With().Debug("addr txs exhausted", ntx.Principal)
		_ = heap.Pop(&mi.pq)
	} else {
		mi.logger.With().Debug("added tx for addr",
			next.ID,
			next.Principal,
			log.Uint64("fee", next.Fee()),
			log.Uint64("gas", next.MaxGas),
			log.Time("received", next.Received))
		// updating the item (for the same address) in the heap is less expensive than a pop followed by a push.
		mi.pq.update(top, next)
	}
	return ntx
}

// PopAll returns all the transaction in the mempoolIterator.
func (mi *mempoolIterator) PopAll() ([]*NanoTX, map[types.Address][]*NanoTX) {
	result := make([]*NanoTX, 0)
	byAddrAndNonce := make(map[types.Address][]*NanoTX)
	for {
		popped := mi.pop()
		if popped == nil {
			break
		}
		result = append(result, popped)
		principal := popped.Principal
		if _, ok := byAddrAndNonce[principal]; !ok {
			byAddrAndNonce[principal] = make([]*NanoTX, 0, maxTXsPerAcct)
		}
		byAddrAndNonce[principal] = append(byAddrAndNonce[principal], popped)
	}
	return result, byAddrAndNonce
}
