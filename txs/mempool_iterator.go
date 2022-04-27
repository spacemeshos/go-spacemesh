package txs

import (
	"container/heap"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	txtypes "github.com/spacemeshos/go-spacemesh/txs/types"
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
	return pq[i].fee > pq[j].fee || (pq[i].fee == pq[j].fee && pq[i].received.Before(pq[j].received))
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
// not thread-safe.
type mempoolIterator struct {
	logger  log.Log
	txsLeft int
	pq      priorityQueue
	txs     map[types.Address][]*txtypes.NanoTX
}

// newMempoolIterator builds and returns a mempoolIterator.
func newMempoolIterator(logger log.Log, cs conStateCache, numTXs int) (*mempoolIterator, error) {
	txs, err := cs.GetMempool()
	if err != nil {
		return nil, err
	}
	mi := &mempoolIterator{
		logger:  logger,
		txsLeft: numTXs,
		// each account can only have at most one transaction in the PQ
		// TODO relax this
		pq:  make(priorityQueue, 0, len(txs)),
		txs: txs,
	}
	logger.With().Info("received mempool txs", log.Int("num_accounts", len(txs)))
	mi.buildPQ()
	return mi, nil
}

func (mi *mempoolIterator) buildPQ() {
	i := 0
	for addr, ntxs := range mi.txs {
		ntx := ntxs[0]
		it := &item{
			tid:      ntx.Tid,
			addr:     ntx.Principal,
			fee:      ntx.Fee,
			received: ntx.Received,
			index:    i,
		}
		i++
		mi.pq = append(mi.pq, it)
		mi.logger.With().Debug("adding item to pq",
			ntx.Tid,
			ntx.Principal,
			log.Uint64("fee", ntx.Fee),
			log.Time("received", ntx.Received))

		if len(ntxs) == 1 {
			delete(mi.txs, addr)
		} else {
			mi.txs[addr] = ntxs[1:]
		}
	}
	heap.Init(&mi.pq)
}

func (mi *mempoolIterator) getNext(addr types.Address) *txtypes.NanoTX {
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

func (mi *mempoolIterator) pop() types.TransactionID {
	if len(mi.pq) == 0 || mi.txsLeft == 0 {
		return types.EmptyTransactionID
	}

	// the first item in priority queue is always the item to be pop with the heap
	top := mi.pq[0]
	mi.logger.With().Debug("popping tx",
		top.tid,
		top.addr,
		log.Uint64("fee", top.fee),
		log.Time("received", top.received))
	tid := top.tid
	next := mi.getNext(top.addr)
	if next == nil {
		mi.logger.With().Debug("addr txs exhausted", top.addr)
		_ = heap.Pop(&mi.pq)
	} else {
		mi.logger.With().Debug("added tx for addr",
			next.Tid,
			next.Principal,
			log.Uint64("fee", next.Fee),
			log.Time("received", next.Received))
		// updating the item (for the same address) in the heap is less expensive than a pop followed by a push.
		mi.pq.update(top, next.Tid, next.Fee, next.Received)
	}
	mi.txsLeft--
	return tid
}

// PopAll returns all the transaction in the mempoolIterator.
func (mi *mempoolIterator) PopAll() []types.TransactionID {
	result := make([]types.TransactionID, 0, mi.txsLeft)
	for {
		popped := mi.pop()
		if popped == types.EmptyTransactionID {
			break
		}
		result = append(result, popped)
	}
	return result
}
