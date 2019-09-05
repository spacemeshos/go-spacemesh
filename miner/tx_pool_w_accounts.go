package miner

import (
	"bytes"
	"encoding/binary"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/pending_txs"
	"github.com/spacemeshos/sha256-simd"
	"sort"
	"sync"
)

type innerPool interface {
	Get(id types.TransactionId) (types.Transaction, error)
	GetAllItems() []types.Transaction
	Put(id types.TransactionId, item *types.Transaction)
	Invalidate(id types.TransactionId)
}

type TxPoolWithAccounts struct {
	innerPool
	accounts      map[types.Address]*pending_txs.AccountPendingTxs
	accountsMutex sync.RWMutex
}

func NewTxPoolWithAccounts() *TxPoolWithAccounts {
	return &TxPoolWithAccounts{
		innerPool: NewTxMemPool(),
		accounts:  make(map[types.Address]*pending_txs.AccountPendingTxs),
	}
}

func (t *TxPoolWithAccounts) GetRandomTxs(numOfTxs int, seed []byte) []types.Transaction {
	txs := t.innerPool.GetAllItems()
	if len(txs) <= numOfTxs {
		return txs
	}

	sort.Slice(txs, func(i, j int) bool {
		id1 := txs[i].Id()
		id2 := txs[j].Id()
		return bytes.Compare(id1[:], id2[:]) < 0
	})
	var ret []types.Transaction
	for idx := range getRandIdxs(numOfTxs, len(txs), seed) {
		ret = append(ret, txs[idx])
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

func (t *TxPoolWithAccounts) Put(id types.TransactionId, tx *types.Transaction) {
	t.accountsMutex.Lock()
	t.getOrCreate(tx.Origin()).Add([]types.TinyTx{types.TxToTiny(tx)}, 0)
	t.accountsMutex.Unlock()
	t.innerPool.Put(id, tx)
}

func (t *TxPoolWithAccounts) Invalidate(id types.TransactionId) {
	t.accountsMutex.Lock()
	if tx, err := t.innerPool.Get(id); err == nil {
		t.removeNonce(tx)
	}
	t.innerPool.Invalidate(id)
	t.accountsMutex.Unlock()
}

func (t *TxPoolWithAccounts) removeNonce(tx types.Transaction) {
	pendingTxs, found := t.accounts[tx.Origin()]
	if !found {
		return
	}
	pendingTxs.RemoveNonce(tx.AccountNonce)
	if pendingTxs.IsEmpty() {
		delete(t.accounts, tx.Origin())
	}
}

func (t *TxPoolWithAccounts) GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64) {
	t.accountsMutex.RLock()
	account, found := t.accounts[addr]
	t.accountsMutex.RUnlock()
	if !found {
		return prevNonce, prevBalance
	}
	return account.GetProjection(prevNonce, prevBalance)
}

func (t *TxPoolWithAccounts) getOrCreate(addr types.Address) *pending_txs.AccountPendingTxs {
	account, found := t.accounts[addr]
	if !found {
		account = pending_txs.NewAccountPendingTxs()
		t.accounts[addr] = account
	}
	return account
}
