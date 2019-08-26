package miner

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNewTxPoolWithAccounts(t *testing.T) {
	r := require.New(t)

	pool := NewTxPoolWithAccounts()
	prevNonce := uint64(5)
	prevBalance := uint64(1000)

	origin := types.BytesToAddress([]byte("abc"))
	nonce, balance := pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce, nonce)
	r.Equal(prevBalance, balance)

	tx1 := newTx(origin, 5, 50)
	pool.Put(types.GetTransactionId(tx1.SerializableSignedTransaction), tx1)
	nonce, balance = pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-50, balance)

	tx2 := newTx(origin, 6, 150)
	pool.Put(types.GetTransactionId(tx2.SerializableSignedTransaction), tx2)
	nonce, balance = pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-150, balance)

	pool.Invalidate(types.GetTransactionId(tx1.SerializableSignedTransaction))
	nonce, balance = pool.GetProjection(origin, prevNonce+1, prevBalance-50)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-150, balance)

	items := pool.PopItems(10)
	r.Len(items, 1)
	r.Equal(tx2, &items[0])
	nonce, balance = pool.GetProjection(origin, prevNonce+2, prevBalance-50-150)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-150, balance)
}

func newTx(origin types.Address, nonce, amount uint64) *types.AddressableSignedTransaction {
	return types.NewAddressableTx(nonce, origin, types.Address{}, amount, 3, 1)
}
