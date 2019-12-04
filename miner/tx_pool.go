package miner

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/pending_txs"
	"github.com/spacemeshos/go-spacemesh/rand"
	"sync"
)

type TxMempool struct {
	txs      map[types.TransactionId]*types.Transaction
	accounts map[types.Address]*pending_txs.AccountPendingTxs
	txByAddr map[types.Address]map[types.TransactionId]struct{}
	mu       sync.RWMutex
}

func NewTxMemPool() *TxMempool {
	return &TxMempool{
		txs:      make(map[types.TransactionId]*types.Transaction),
		accounts: make(map[types.Address]*pending_txs.AccountPendingTxs),
		txByAddr: make(map[types.Address]map[types.TransactionId]struct{}),
	}
}

func (t *TxMempool) Get(id types.TransactionId) (*types.Transaction, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if tx, found := t.txs[id]; found {
		return tx, nil
	}
	return nil, errors.New("transaction not found in mempool")
}

func (t *TxMempool) GetTxIdsByAddress(addr types.Address) []types.TransactionId {
	var ids []types.TransactionId
	for id := range t.txByAddr[addr] {
		ids = append(ids, id)
	}
	return ids
}

func (t *TxMempool) GetTxsForBlock(numOfTxs int, getState func(addr types.Address) (nonce, balance uint64, err error)) ([]types.TransactionId, error) {
	var txIds []types.TransactionId
	t.mu.RLock()
	for addr, account := range t.accounts {
		nonce, balance, err := getState(addr)
		if err != nil {
			t.mu.RUnlock()
			return nil, fmt.Errorf("failed to get state for addr %s: %v", addr.Short(), err)
		}
		accountTxIds, _, _ := account.ValidTxs(nonce, balance)
		txIds = append(txIds, accountTxIds...)
	}
	t.mu.RUnlock()

	if len(txIds) <= numOfTxs {
		return txIds, nil
	}

	var ret []types.TransactionId
	for idx := range getRandIdxs(numOfTxs, len(txIds)) {
		//noinspection GoNilness
		ret = append(ret, txIds[idx])
	}
	return ret, nil
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

func (t *TxMempool) Put(id types.TransactionId, tx *types.Transaction) {
	t.mu.Lock()
	t.txs[id] = tx
	t.getOrCreate(tx.Origin()).Add(0, tx)
	t.addToAddr(tx.Origin(), id)
	t.addToAddr(tx.Recipient, id)
	t.mu.Unlock()
}

func (t *TxMempool) Invalidate(id types.TransactionId) {
	t.mu.Lock()
	if tx, found := t.txs[id]; found {
		if pendingTxs, found := t.accounts[tx.Origin()]; found {
			// Once a tx appears in a block we want to invalidate all of this nonce's variants. The mempool currently
			// only accepts one version, but this future-proofs it.
			pendingTxs.RemoveNonce(tx.AccountNonce, func(id types.TransactionId) {
				delete(t.txs, id)
			})
			if pendingTxs.IsEmpty() {
				delete(t.accounts, tx.Origin())
			}
		}
		t.removeFromAddr(tx.Origin(), id)
		t.removeFromAddr(tx.Recipient, id)
	}
	t.mu.Unlock()
}

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
func (t *TxMempool) getOrCreate(addr types.Address) *pending_txs.AccountPendingTxs {
	account, found := t.accounts[addr]
	if !found {
		account = pending_txs.NewAccountPendingTxs()
		t.accounts[addr] = account
	}
	return account
}

// ⚠️ must be called under write-lock
func (t *TxMempool) addToAddr(addr types.Address, txId types.TransactionId) {
	addrMap, found := t.txByAddr[addr]
	if !found {
		addrMap = make(map[types.TransactionId]struct{})
		t.txByAddr[addr] = addrMap
	}
	addrMap[txId] = struct{}{}
}

// ⚠️ must be called under write-lock
func (t *TxMempool) removeFromAddr(addr types.Address, txId types.TransactionId) {
	addrMap := t.txByAddr[addr]
	delete(addrMap, txId)
	if len(addrMap) == 0 {
		delete(t.txByAddr, addr)
	}
}
