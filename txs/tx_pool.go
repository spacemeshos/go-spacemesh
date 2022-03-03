package txs

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
)

// txPool is a struct that holds txs received via gossip network.
type txPool struct {
	mu       sync.RWMutex
	txs      map[types.TransactionID]*types.Transaction
	accounts map[types.Address]*accountPendingTxs
}

// newTxPool returns a new txPool struct.
func newTxPool() *txPool {
	return &txPool{
		txs:      make(map[types.TransactionID]*types.Transaction),
		accounts: make(map[types.Address]*accountPendingTxs),
	}
}

func (tp *txPool) get(id types.TransactionID) (*types.MeshTransaction, error) {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	if tx, found := tp.txs[id]; found {
		return &types.MeshTransaction{
			Transaction: *tx,
			State:       types.MEMPOOL,
		}, nil
	}

	return nil, errors.New("tx not in mempool")
}

func (tp *txPool) add(id types.TransactionID, tx *types.Transaction) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	tp.txs[id] = tx
	account, found := tp.accounts[tx.Origin()]
	if !found {
		account = newAccountPendingTxs()
		tp.accounts[tx.Origin()] = account
	}
	account.Add(types.LayerID{}, tx)
}

func (tp *txPool) remove(id types.TransactionID) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if tx, found := tp.txs[id]; found {
		if pendingTxs, found := tp.accounts[tx.Origin()]; found {
			// Once a tx appears in a block we want to invalidate all of this nonce's variants. The mempool currently
			// only accepts one version, but this future-proofs it.
			pendingTxs.RemoveNonce(tx.AccountNonce, func(id types.TransactionID) {
				delete(tp.txs, id)
			})
			if pendingTxs.IsEmpty() {
				delete(tp.accounts, tx.Origin())
			}
			// We only report those transactions that are being dropped from the txpool here as
			// conflicting since they won't be reported anywhere else. There is no need to report
			// the initial tx here since it'll be reported as part of a new block/layer anyway.
			events.ReportTxWithValidity(types.LayerID{}, tx, false)
		}
	}
}

func (tp *txPool) getProjection(addr types.Address, prevNonce, prevBalance uint64) (uint64, uint64) {
	tp.mu.RLock()
	account, found := tp.accounts[addr]
	tp.mu.RUnlock()
	if !found {
		return prevNonce, prevBalance
	}

	return account.GetProjection(prevNonce, prevBalance)
}

func (tp *txPool) getCandidates(numTXs int, getProjection func(types.Address) (uint64, uint64, error)) ([]types.TransactionID, []*types.Transaction, error) {
	txIDs := make([]types.TransactionID, 0, numTXs)
	txByPrincipal := make(map[types.Address][]types.TransactionID)

	tp.mu.RLock()
	defer tp.mu.RUnlock()
	for addr, account := range tp.accounts {
		nonce, balance, err := getProjection(addr)
		if err != nil {
			return nil, nil, fmt.Errorf("get projection for addr %s: %v", addr.Short(), err)
		}
		accountTxIds, _, _ := account.ValidTxs(nonce, balance)
		txIDs = append(txIDs, accountTxIds...)
		txByPrincipal[addr] = accountTxIds
	}

	txs := make([]*types.Transaction, 0, len(txIDs))
	for _, tx := range txIDs {
		txs = append(txs, tp.txs[tx])
	}

	if len(txIDs) <= numTXs {
		return txIDs, txs, nil
	}

	// randomly select an address and fill the TX in nonce order
	uniqueAddrs := make([]types.Address, 0, len(txByPrincipal))
	for addr := range txByPrincipal {
		uniqueAddrs = append(uniqueAddrs, addr)
	}
	var (
		ret      []types.TransactionID
		retTXs   []*types.Transaction
		numAddrs = len(uniqueAddrs)
	)
	for len(ret) < numTXs {
		idx := rand.Uint64() % uint64(numAddrs)
		addr := uniqueAddrs[idx]
		if _, ok := txByPrincipal[addr]; !ok {
			// TXs for this addr is exhausted
			continue
		}
		// take the first tx (the lowest nonce) for this account
		id := txByPrincipal[addr][0]
		ret = append(ret, id)
		retTXs = append(retTXs, tp.txs[id])
		txByPrincipal[addr] = txByPrincipal[addr][1:]
		if len(txByPrincipal[addr]) == 0 {
			delete(txByPrincipal, addr)
		}
		if len(txByPrincipal) == 0 {
			// all addresses are exhausted
			break
		}
	}
	return ret, retTXs, nil
}
