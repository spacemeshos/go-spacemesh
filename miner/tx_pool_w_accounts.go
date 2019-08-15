package miner

import "github.com/spacemeshos/go-spacemesh/types"

type TxPool interface {
	Get(id types.TransactionId) (types.AddressableSignedTransaction, error)
	PopItems(size int) []types.AddressableSignedTransaction
	Put(id types.TransactionId, item *types.AddressableSignedTransaction)
	Invalidate(id types.TransactionId)
}

type TxPoolWithAccounts struct {
	TxPool
}

func NewTxPoolWithAccounts() *TxPoolWithAccounts {
	return &TxPoolWithAccounts{
		TxPool: NewTypesTransactionIdMemPool(),
	}
}

func (t *TxPoolWithAccounts) Get(id types.TransactionId) (types.AddressableSignedTransaction, error) {
	return t.TxPool.Get(id)
}

func (t *TxPoolWithAccounts) PopItems(size int) []types.AddressableSignedTransaction {
	return t.TxPool.PopItems(size)
}

func (t *TxPoolWithAccounts) Put(id types.TransactionId, item *types.AddressableSignedTransaction) {
	t.TxPool.Put(id, item)
}

func (t *TxPoolWithAccounts) Invalidate(id types.TransactionId) {
	t.TxPool.Invalidate(id)
}
