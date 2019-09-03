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
	Get(id types.TransactionId) (types.AddressableSignedTransaction, error)
	GetAllItems() []types.AddressableSignedTransaction
	Put(id types.TransactionId, item *types.AddressableSignedTransaction)
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

func (t *TxPoolWithAccounts) GetRandomTxs(numOfTxs int, seed []byte) []types.AddressableSignedTransaction {
	txs := t.innerPool.GetAllItems()
	if len(txs) <= numOfTxs {
		return txs
	}

	sort.Slice(txs, func(i, j int) bool {
		id1 := types.GetTransactionId(txs[i].SerializableSignedTransaction)
		id2 := types.GetTransactionId(txs[j].SerializableSignedTransaction)
		return bytes.Compare(id1[:], id2[:]) < 0
	})
	var ret []types.AddressableSignedTransaction
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

func (t *TxPoolWithAccounts) Put(id types.TransactionId, item *types.AddressableSignedTransaction) {
	t.accountsMutex.Lock()
	t.getOrCreate(item.Address).Add([]types.TinyTx{types.AddressableTxToTiny(item)}, 0)
	t.accountsMutex.Unlock()
	t.innerPool.Put(id, item)
}

func (t *TxPoolWithAccounts) Invalidate(id types.TransactionId) {
	t.accountsMutex.Lock()
	if tx, err := t.innerPool.Get(id); err == nil {
		t.accounts[tx.Address].RemoveNonce(tx.AccountNonce)
		if t.accounts[tx.Address].IsEmpty() {
			delete(t.accounts, tx.Address)
		}
	}
	t.innerPool.Invalidate(id)
	t.accountsMutex.Unlock()
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
