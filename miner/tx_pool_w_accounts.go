package miner

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/pending_txs"
)

type TxPool interface {
	Get(id types.TransactionId) (types.AddressableSignedTransaction, error)
	PopItems(size int) []types.AddressableSignedTransaction
	Put(id types.TransactionId, item *types.AddressableSignedTransaction)
	Invalidate(id types.TransactionId)
}

type TxPoolWithAccounts struct {
	TxPool
	accounts map[types.Address]*pending_txs.AccountPendingTxs
}

func NewTxPoolWithAccounts() *TxPoolWithAccounts {
	return &TxPoolWithAccounts{
		TxPool:   NewTypesTransactionIdMemPool(),
		accounts: make(map[types.Address]*pending_txs.AccountPendingTxs),
	}
}

func (t *TxPoolWithAccounts) PopItems(size int) []types.AddressableSignedTransaction {
	txs := t.TxPool.PopItems(size)
	for _, tx := range txs {
		t.accounts[tx.Address].RemoveNonce(tx.AccountNonce)
	}
	return txs
}

func (t *TxPoolWithAccounts) Put(id types.TransactionId, item *types.AddressableSignedTransaction) {
	t.getOrCreate(item.Address).Add([]types.TinyTx{types.AddressableTxToTiny(item)}, 0)
	t.TxPool.Put(id, item)
}

func (t *TxPoolWithAccounts) Invalidate(id types.TransactionId) {
	if tx, err := t.TxPool.Get(id); err == nil {
		t.accounts[tx.Address].RemoveNonce(tx.AccountNonce)
	}
	t.TxPool.Invalidate(id)
}

func (t *TxPoolWithAccounts) GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64) {
	account, found := t.accounts[addr]
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
