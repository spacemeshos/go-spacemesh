package miner

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/pending_txs"
	"github.com/spacemeshos/sha256-simd"
	"sort"
	"sync"
)

type TxMempool struct {
	txs      map[types.TransactionId]*types.Transaction
	accounts map[types.Address]*pending_txs.AccountPendingTxs
	mu       sync.RWMutex
}

func NewTxMemPool() *TxMempool {
	return &TxMempool{
		txs:      make(map[types.TransactionId]*types.Transaction),
		accounts: make(map[types.Address]*pending_txs.AccountPendingTxs),
	}
}

func (t *TxMempool) Get(id types.TransactionId) (types.Transaction, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if tx, found := t.txs[id]; found {
		return *tx, nil
	}
	return types.Transaction{}, errors.New("transaction not found in mempool")
}

func (t *TxMempool) GetRandomTxs(numOfTxs int, seed []byte) []types.TransactionId {
	var txIds []types.TransactionId
	t.mu.RLock()
	for id := range t.txs {
		txIds = append(txIds, id)
	}
	t.mu.RUnlock()

	if len(txIds) <= numOfTxs {
		return txIds
	}

	sort.Slice(txIds, func(i, j int) bool {
		return bytes.Compare(txIds[i].Bytes(), txIds[j].Bytes()) < 0
	})
	var ret []types.TransactionId
	for idx := range getRandIdxs(numOfTxs, len(txIds), seed) {
		ret = append(ret, txIds[idx])
	}
	return ret
}

func getRandIdxs(numOfTxs, spaceSize int, seed []byte) map[uint64]struct{} {
	idxs := make(map[uint64]struct{})
	i := uint64(0)
	for len(idxs) < numOfTxs {
		message := make([]byte, len(seed)+binary.Size(i))
		copy(message, seed)
		binary.LittleEndian.PutUint64(message[len(seed):], i)
		msgHash := sha256.Sum256(message)
		msgInt := binary.LittleEndian.Uint64(msgHash[:8])
		idx := msgInt % uint64(spaceSize)
		idxs[idx] = struct{}{}
		i++
	}
	return idxs
}

func (t *TxMempool) Put(id types.TransactionId, tx *types.Transaction) {
	t.mu.Lock()
	t.txs[id] = tx
	t.getOrCreate(tx.Origin()).Add(0, tx)
	t.mu.Unlock()
}

func (t *TxMempool) Invalidate(id types.TransactionId) {
	t.mu.Lock()
	if tx, found := t.txs[id]; found {
		if pendingTxs, found := t.accounts[tx.Origin()]; found {
			pendingTxs.RemoveNonce(tx.AccountNonce, func(id types.TransactionId) {
				delete(t.txs, id)
			})
			if pendingTxs.IsEmpty() {
				delete(t.accounts, tx.Origin())
			}
		}
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

func (t *TxMempool) getOrCreate(addr types.Address) *pending_txs.AccountPendingTxs {
	account, found := t.accounts[addr]
	if !found {
		account = pending_txs.NewAccountPendingTxs()
		t.accounts[addr] = account
	}
	return account
}
