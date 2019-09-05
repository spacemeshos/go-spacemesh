package miner

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
	"strconv"
	"sync"
	"testing"
	"time"
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
	r.ElementsMatch([]uint64{5, 8, 10, 12, 14}, nonces)
}

func TestGetRandIdxs(t *testing.T) {
	seed := []byte("seed")

	idxs := getRandIdxs(5, 10, seed)

	var idsList []uint64
	for id := range idxs {
		idsList = append(idsList, id)
	}
	require.ElementsMatch(t, []uint64{1, 5, 6, 8, 9}, idsList)

	otherSeed := []byte("other seed")

	idxs = getRandIdxs(5, 10, otherSeed)

	idsList = []uint64{}
	for id := range idxs {
		idsList = append(idsList, id)
	}
	require.ElementsMatch(t, []uint64{2, 4, 6, 7, 8}, idsList) // different seed -> different indices
}

func newTx(origin types.Address, nonce, totalAmount uint64) (types.TransactionId, *types.Transaction) {
	feeAmount := uint64(1)
	tx := types.NewTxWithOrigin(nonce, origin, types.Address{}, totalAmount-feeAmount, 3, feeAmount)
	return tx.Id(), tx
}

func BenchmarkTxPoolWithAccounts(b *testing.B) {
	pool := NewTxPoolWithAccounts()

	const numBatches = 10
	txBatches := make([][]*types.Transaction, numBatches)
	txIdBatches := make([][]types.TransactionId, numBatches)
	for i := 0; i < numBatches; i++ {
		txBatches[i], txIdBatches[i] = createBatch(types.BytesToAddress([]byte("origin" + strconv.Itoa(i))))
	}

	wg := &sync.WaitGroup{}

	start := time.Now()
	wg.Add(numBatches)
	for i, batch := range txBatches {
		go addBatch(pool, batch, txIdBatches[i], wg)
	}
	wg.Wait()
	wg.Add(numBatches)
	for _, batch := range txIdBatches {
		go invalidateBatch(pool, batch, wg)
	}
	wg.Wait()
	b.Log(time.Since(start))
}

func addBatch(pool *TxPoolWithAccounts, txBatch []*types.Transaction, txIdBatch []types.TransactionId, wg *sync.WaitGroup) {
	for i, tx := range txBatch {
		pool.Put(txIdBatch[i], tx)
	}
	wg.Done()
}

func invalidateBatch(pool *TxPoolWithAccounts, txIdBatch []types.TransactionId, wg *sync.WaitGroup) {
	for _, txId := range txIdBatch {
		pool.Invalidate(txId)
	}
	wg.Done()
}

func createBatch(origin types.Address) ([]*types.Transaction, []types.TransactionId) {
	var txBatch []*types.Transaction
	var txIdBatch []types.TransactionId
	for i := uint64(0); i < 10000; i++ {
		txId, tx := newTx(origin, 5+i, 50)
		txBatch = append(txBatch, tx)
		txIdBatch = append(txIdBatch, txId)
	}
	return txBatch, txIdBatch
}
