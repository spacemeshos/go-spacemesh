package txs

import (
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/rand"
)

// TxMempool is a struct that holds txs received via gossip network.
type TxMempool struct {
	mu       sync.RWMutex
	txs      map[types.TransactionID]*types.Transaction
	accounts map[types.Address]*AccountPendingTxs
}

// NewTxMemPool returns a new TxMempool struct.
func NewTxMemPool() *TxMempool {
	return &TxMempool{
		txs:      make(map[types.TransactionID]*types.Transaction),
		accounts: make(map[types.Address]*AccountPendingTxs),
	}
}

// Get returns transaction by provided id, it returns an error if transaction is not found.
func (t *TxMempool) Get(id types.TransactionID) (*types.Transaction, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if tx, found := t.txs[id]; found {
		return tx, nil
	}
	return nil, errors.New("transaction not found in mempool")
}

func (t *TxMempool) getCandidateTXs(numTXs int, getState func(types.Address) (nonce, balance uint64, err error)) ([]types.TransactionID, error) {
	txIds := make([]types.TransactionID, 0, numTXs)

	t.mu.RLock()
	defer t.mu.RUnlock()
	for addr, account := range t.accounts {
		nonce, balance, err := getState(addr)
		if err != nil {
			return nil, fmt.Errorf("failed to get state for addr %s: %v", addr.Short(), err)
		}
		accountTxIds, _, _ := account.ValidTxs(nonce, balance)
		txIds = append(txIds, accountTxIds...)
	}
	return txIds, nil
}

// SelectTopNTransactions picks a specific number of random txs for miner. This function also receives a state calculation function
// to allow returning only transactions that will probably be valid.
func (t *TxMempool) SelectTopNTransactions(numOfTxs int, getState func(types.Address) (nonce, balance uint64, err error)) ([]types.TransactionID, []*types.Transaction, error) {
	txIds, err := t.getCandidateTXs(numOfTxs, getState)
	if err != nil {
		return nil, nil, err
	}

	if len(txIds) <= numOfTxs {
		return txIds, t.getTxByIds(txIds), nil
	}

	var ret []types.TransactionID
	for idx := range getRandIdxs(numOfTxs, len(txIds)) {
		// noinspection GoNilness
		ret = append(ret, txIds[idx])
	}

	return ret, t.getTxByIds(ret), nil
}

func (t *TxMempool) getTxByIds(txsIDs []types.TransactionID) []*types.Transaction {
	txs := make([]*types.Transaction, 0, len(txsIDs))
	for _, tx := range txsIDs {
		txs = append(txs, t.txs[tx])
	}
	return txs
}

func getRandIdxs(numOfTxs, spaceSize int) map[uint64]struct{} {
	idxs := make(map[uint64]struct{})
	for len(idxs) < numOfTxs {
		rndInt := rand.Uint64()
		idx := rndInt % uint64(spaceSize)
		idxs[idx] = struct{}{}
	}
	return idxs
}

// Put inserts a transaction into the mem pool. It indexes it by source and dest addresses as well.
func (t *TxMempool) Put(id types.TransactionID, tx *types.Transaction) {
	t.put(id, tx)
	events.ReportNewTx(types.LayerID{}, tx)
	events.ReportAccountUpdate(tx.Origin())
	events.ReportAccountUpdate(tx.GetRecipient())
}

func (t *TxMempool) put(id types.TransactionID, tx *types.Transaction) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.txs[id] = tx
	t.getOrCreate(tx.Origin()).Add(types.LayerID{}, tx)
}

// Invalidate removes transaction from pool.
func (t *TxMempool) Invalidate(id types.TransactionID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if tx, found := t.txs[id]; found {
		if pendingTxs, found := t.accounts[tx.Origin()]; found {
			// Once a tx appears in a block we want to invalidate all of this nonce's variants. The mempool currently
			// only accepts one version, but this future-proofs it.
			pendingTxs.RemoveNonce(tx.AccountNonce, func(id types.TransactionID) {
				delete(t.txs, id)
			})
			if pendingTxs.IsEmpty() {
				delete(t.accounts, tx.Origin())
			}
			// We only report those transactions that are being dropped from the txpool here as
			// conflicting since they won't be reported anywhere else. There is no need to report
			// the initial tx here since it'll be reported as part of a new block/layer anyway.
			events.ReportTxWithValidity(types.LayerID{}, tx, false)
		}
	}
}

// GetProjection returns the estimated nonce and balance for the provided address addr and previous nonce and balance
// projecting state is done by applying transactions from the pool.
func (t *TxMempool) GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64) {
	t.mu.RLock()
	account, found := t.accounts[addr]
	t.mu.RUnlock()
	if !found {
		return prevNonce, prevBalance
	}
	return account.GetProjection(prevNonce, prevBalance)
}

// ⚠️ must be called under write-lock.
func (t *TxMempool) getOrCreate(addr types.Address) *AccountPendingTxs {
	account, found := t.accounts[addr]
	if !found {
		account = NewAccountPendingTxs()
		t.accounts[addr] = account
	}
	return account
}
