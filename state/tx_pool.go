package state

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/pendingtxs"
	"github.com/spacemeshos/go-spacemesh/rand"
	"sync"
)

// TxMempool is a struct that holds txs received via gossip network
type TxMempool struct {
	txs      map[types.TransactionID]*types.Transaction
	accounts map[types.Address]*pendingtxs.AccountPendingTxs
	txByAddr map[types.Address]map[types.TransactionID]struct{}
	mu       sync.RWMutex
}

// NewTxMemPool returns a new TxMempool struct
func NewTxMemPool() *TxMempool {
	return &TxMempool{
		txs:      make(map[types.TransactionID]*types.Transaction),
		accounts: make(map[types.Address]*pendingtxs.AccountPendingTxs),
		txByAddr: make(map[types.Address]map[types.TransactionID]struct{}),
	}
}

// Get returns transaction by provided id, it returns an error if transaction is not found
func (t *TxMempool) Get(id types.TransactionID) (*types.Transaction, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if tx, found := t.txs[id]; found {
		return tx, nil
	}
	return nil, errors.New("transaction not found in mempool")
}

// GetTxIdsByAddress returns all transactions from/to a specific address
func (t *TxMempool) GetTxIdsByAddress(addr types.Address) []types.TransactionID {
	var ids []types.TransactionID
	for id := range t.txByAddr[addr] {
		ids = append(ids, id)
	}
	return ids
}

// GetTxsForBlock gets a specific number of random txs for a block. This function also receives a state calculation function
// to allow returning only transactions that will probably be valid
func (t *TxMempool) GetTxsForBlock(numOfTxs int, getState func(addr types.Address) (nonce, balance uint64, err error)) ([]types.TransactionID, []*types.Transaction, error) {
	var txIds []types.TransactionID
	t.mu.RLock()
	for addr, account := range t.accounts {
		nonce, balance, err := getState(addr)
		if err != nil {
			t.mu.RUnlock()
			return nil, nil, fmt.Errorf("failed to get state for addr %s: %v", addr.Short(), err)
		}
		accountTxIds, _, _ := account.ValidTxs(nonce, balance)
		txIds = append(txIds, accountTxIds...)
	}
	t.mu.RUnlock()

	if len(txIds) <= numOfTxs {
		return txIds, t.getTxByIds(txIds), nil
	}

	var ret []types.TransactionID
	for idx := range getRandIdxs(numOfTxs, len(txIds)) {
		//noinspection GoNilness
		ret = append(ret, txIds[idx])
	}

	return ret, t.getTxByIds(ret), nil
}

func (t *TxMempool) getTxByIds(txsIDs []types.TransactionID) (txs []*types.Transaction) {
	for _, tx := range txsIDs {
		txs = append(txs, t.txs[tx])
	}
	return
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

// Put inserts a transaction into the mem pool. It indexes it by source and dest addresses as well
func (t *TxMempool) Put(id types.TransactionID, tx *types.Transaction) {
	t.mu.Lock()
	t.txs[id] = tx
	t.getOrCreate(tx.Origin()).Add(0, tx)
	t.addToAddr(tx.Origin(), id)
	t.addToAddr(tx.Recipient, id)
	t.mu.Unlock()
	events.ReportNewTx(tx)
}

// Invalidate removes transaction from pool
func (t *TxMempool) Invalidate(id types.TransactionID) {
	t.mu.Lock()
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
			events.ReportTxWithValidity(tx, false)
		}
		t.removeFromAddr(tx.Origin(), id)
		t.removeFromAddr(tx.Recipient, id)
	}
	t.mu.Unlock()
}

// GetProjection returns the estimated nonce and balance for the provided address addr and previous nonce and balance
// projecting state is done by applying transactions from the pool
func (t *TxMempool) GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64) {
	t.mu.RLock()
	account, found := t.accounts[addr]
	t.mu.RUnlock()
	if !found {
		return prevNonce, prevBalance
	}
	return account.GetProjection(prevNonce, prevBalance)
}

// ⚠️ must be called under write-lock
func (t *TxMempool) getOrCreate(addr types.Address) *pendingtxs.AccountPendingTxs {
	account, found := t.accounts[addr]
	if !found {
		account = pendingtxs.NewAccountPendingTxs()
		t.accounts[addr] = account
	}
	return account
}

// ⚠️ must be called under write-lock
func (t *TxMempool) addToAddr(addr types.Address, txID types.TransactionID) {
	addrMap, found := t.txByAddr[addr]
	if !found {
		addrMap = make(map[types.TransactionID]struct{})
		t.txByAddr[addr] = addrMap
	}
	addrMap[txID] = struct{}{}
}

// ⚠️ must be called under write-lock
func (t *TxMempool) removeFromAddr(addr types.Address, txID types.TransactionID) {
	addrMap := t.txByAddr[addr]
	delete(addrMap, txID)
	if len(addrMap) == 0 {
		delete(t.txByAddr, addr)
	}
}
