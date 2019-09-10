package miner

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestNewTxPoolWithAccounts(t *testing.T) {
	r := require.New(t)

	pool := NewTxMemPool()
	prevNonce := uint64(4)
	prevBalance := uint64(1000)

	origin := types.BytesToAddress([]byte("abc"))
	nonce, balance := pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce, nonce)
	r.Equal(prevBalance, balance)

	tx1Id, tx1 := newTx(origin, 4, 50)
	pool.Put(tx1Id, tx1)
	nonce, balance = pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-50, balance)

	tx2Id, tx2 := newTx(origin, 5, 150)
	pool.Put(tx2Id, tx2)
	nonce, balance = pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-150, balance)

	pool.Invalidate(tx1Id)
	nonce, balance = pool.GetProjection(origin, prevNonce+1, prevBalance-50)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-150, balance)

	seed := []byte("seed")
	items, err := pool.GetTxsForBlock(1, seed, getState)
	r.NoError(err)
	r.Len(items, 1)
	r.Equal(tx2.Id(), items[0])
	nonce, balance = pool.GetProjection(origin, prevNonce+2, prevBalance-50-150)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-150, balance)
}

func TestTxPoolWithAccounts_GetRandomTxs(t *testing.T) {
	r := require.New(t)

	pool := NewTxMemPool()
	prevNonce := uint64(5)
	prevBalance := uint64(1000)
	origin := types.BytesToAddress([]byte("abc"))
	const numTxs = 10

	ids := make([]types.TransactionId, numTxs)
	for i := uint64(0); i < numTxs; i++ {
		id, tx := newTx(origin, prevNonce+i, 50)
		ids[i] = id
		fmt.Printf("%d: %v\n", i, id.String())
		pool.Put(id, tx)
	}

	nonce, balance := pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce+numTxs, nonce)
	r.Equal(prevBalance-(50*numTxs), balance)

	seed := []byte("seed")
	txs, err := pool.GetTxsForBlock(5, seed, getState)
	r.NoError(err)
	r.Len(txs, 5)
	var txIds []types.TransactionId
	for _, tx := range txs {
		txIds = append(txIds, tx)
	}
	r.ElementsMatch([]types.TransactionId{ids[5], ids[0], ids[9], ids[3], ids[7]}, txIds)
	/*
		e960de27caffa83e6e8bbcf126268115d7b0284b15b67e3d1394db63ddf551cc
		b791d54444fa6fec3be511c3e02906ccab7dec03611d61b8b5fc6a5a8015320f
		064d86ea163b6b9e2bdf476e2f4da78bf05f1cf90d87d561605498451a0ff56c
		6e1409da76706c440385655be5730b34c0e02545f2d1b54ec9e407d3c6fef8e4
		b21487d43b2bcd27fe1caadff4ddf846e10e0722a78174780a67200bf1dab27e
	*/
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
	pool := NewTxMemPool()

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

func addBatch(pool *TxMempool, txBatch []*types.Transaction, txIdBatch []types.TransactionId, wg *sync.WaitGroup) {
	for i, tx := range txBatch {
		pool.Put(txIdBatch[i], tx)
	}
	wg.Done()
}

func invalidateBatch(pool *TxMempool, txIdBatch []types.TransactionId, wg *sync.WaitGroup) {
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
