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

	tx1Id, tx1 := newTx(origin, 5, 50)
	pool.Put(tx1Id, tx1)
	nonce, balance = pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-50, balance)

	tx2Id, tx2 := newTx(origin, 6, 150)
	pool.Put(tx2Id, tx2)
	nonce, balance = pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-150, balance)

	pool.Invalidate(tx1Id)
	nonce, balance = pool.GetProjection(origin, prevNonce+1, prevBalance-50)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-150, balance)

	seed := []byte("seed")
	items := pool.GetRandomTxs(1, seed)
	r.Len(items, 1)
	r.Equal(tx2, &items[0])
	nonce, balance = pool.GetProjection(origin, prevNonce+2, prevBalance-50-150)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-150, balance)
}

func TestTxPoolWithAccounts_GetRandomTxs(t *testing.T) {
	r := require.New(t)

	pool := NewTxPoolWithAccounts()
	prevNonce := uint64(5)
	prevBalance := uint64(1000)
	origin := types.BytesToAddress([]byte("abc"))
	const numTxs = 10

	for i := prevNonce; i < prevNonce+numTxs; i++ {
		pool.Put(newTx(origin, i, 50))
	}

	nonce, balance := pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce+numTxs, nonce)
	r.Equal(prevBalance-(50*numTxs), balance)

	seed := []byte("seed")
	txs := pool.GetRandomTxs(5, seed)
	r.Len(txs, 5)
	var nonces []uint64
	for _, tx := range txs {
		nonces = append(nonces, tx.AccountNonce)
	}
	r.ElementsMatch([]uint64{5, 7, 11, 12, 14}, nonces)
}

func TestGetRandIdxs(t *testing.T) {
	seed := []byte("seed")

	idxs := getRandIdxs(5, 10, seed)

	var idsList []uint64
	for id := range idxs {
		idsList = append(idsList, id)
	}
	require.ElementsMatch(t, []uint64{1, 5, 6, 8, 9}, idsList)
}

func newTx(origin types.Address, nonce, amount uint64) (types.TransactionId, *types.AddressableSignedTransaction) {
	tx := types.NewAddressableTx(nonce, origin, types.Address{}, amount, 3, 1)
	return types.GetTransactionId(tx.SerializableSignedTransaction), tx
}
