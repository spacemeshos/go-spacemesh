package txs

import (
	"container/heap"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type item struct {
	tid  types.TransactionID
	addr types.Address

	// TODO replace with gas price
	fee      uint64
	received time.Time

	// The index is needed by update and is maintained by the heap.Interface methods.
	index int // The index of the item in the heap.
}

type priorityQueue []*item

// Len implements head.Interface.
func (pq priorityQueue) Len() int { return len(pq) }

// Less implements head.Interface.
func (pq priorityQueue) Less(i, j int) bool {
	// We want Pop to give us the highest, not lowest, fee, so we use greater than here.
	return pq[i].fee > pq[j].fee || pq[i].received.Before(pq[j].received)
}

// Swap implements head.Interface.
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

// Push implements head.Interface.
func (pq *priorityQueue) Push(i interface{}) {
	n := len(*pq)
	it := i.(*item)
	it.index = n
	*pq = append(*pq, it)
}

// Pop implements head.Interface.
func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	it := old[n-1]
	old[n-1] = nil // avoid memory leak
	it.index = -1  // for safety
	*pq = old[0 : n-1]
	return it
}

// update modifies the fee and value of an item in the queue.
func (pq *priorityQueue) update(it *item, tid types.TransactionID, fee uint64, received time.Time) {
	it.tid = tid
	it.fee = fee
	it.received = received
	heap.Fix(pq, it.index)
}

// mempoolIterator holds the best transaction from the conservative state mempool.
type mempoolIterator struct {
	pq  priorityQueue
	txs map[types.Address][]*nanoTX
}

// newMempoolIterator builds and returns a mempoolIterator.
func newMempoolIterator(cs conStateCache, numTXs int) (*mempoolIterator, error) {
	txs, err := cs.getMempool()
	if err != nil {
		return nil, err
	}
	mi := &mempoolIterator{
		pq:  make(priorityQueue, 0, numTXs),
		txs: txs,
	}
	mi.buildPQ(numTXs)
	return mi, nil
}

func (mi *mempoolIterator) buildPQ(numTXs int) {
	i := 0
	for addr, ntxs := range mi.txs {
		ntx := ntxs[0]
		it := &item{
			tid:      ntx.tid,
			addr:     ntx.addr,
			fee:      ntx.fee,
			received: ntx.received,
			index:    i,
		}
		i++
		mi.pq = append(mi.pq, it)

		if len(ntxs) == 1 {
			delete(mi.txs, addr)
		} else {
			mi.txs[addr] = ntxs[1:]
		}

		if i == numTXs {
			break
		}
	}
	heap.Init(&mi.pq)
}

func (mi *mempoolIterator) getNext(addr types.Address) *nanoTX {
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

func (mi *mempoolIterator) Pop() types.TransactionID {
	// the first item in priority queue is always the item to be pop with the heap
	if len(mi.pq) == 0 {
		return types.EmptyTransactionID
	}

	top := mi.pq[0]
	tid := top.tid
	next := mi.getNext(top.addr)
	if next == nil {
		_ = heap.Pop(&mi.pq)
		return tid
	}

	// updating the item (for the same address) in the heap is less expensive than a pop followed by a push.
	mi.pq.update(top, next.tid, next.fee, next.received)
	return tid
}
