package miner

import (
	"encoding/binary"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestNewTxPoolWithAccounts(t *testing.T) {
	r := require.New(t)

	pool := NewTxMemPool()
	prevNonce := uint64(4)
	prevBalance := uint64(1000)

	signer := signing.NewEdSigner()
	origin := types.BytesToAddress(signer.PublicKey().Bytes())
	nonce, balance := pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce, nonce)
	r.Equal(prevBalance, balance)

	tx1Id, tx1 := newTx(t, 4, 50, signer)
	pool.Put(tx1Id, tx1)
	nonce, balance = pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-50, balance)
	r.ElementsMatch([]types.TransactionId{tx1Id}, pool.GetTxIdsByAddress(origin))
	r.ElementsMatch([]types.TransactionId{tx1Id}, pool.GetTxIdsByAddress(tx1.Recipient))

	tx2Id, tx2 := newTx(t, 5, 150, signer)
	pool.Put(tx2Id, tx2)
	nonce, balance = pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-150, balance)
	r.ElementsMatch([]types.TransactionId{tx1Id, tx2Id}, pool.GetTxIdsByAddress(origin))
	r.ElementsMatch([]types.TransactionId{tx1Id}, pool.GetTxIdsByAddress(tx1.Recipient))
	r.ElementsMatch([]types.TransactionId{tx2Id}, pool.GetTxIdsByAddress(tx2.Recipient))

	pool.Invalidate(tx1Id)
	nonce, balance = pool.GetProjection(origin, prevNonce+1, prevBalance-50)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-150, balance)
	r.ElementsMatch([]types.TransactionId{tx2Id}, pool.GetTxIdsByAddress(origin))
	r.Empty(pool.GetTxIdsByAddress(tx1.Recipient))
	r.ElementsMatch([]types.TransactionId{tx2Id}, pool.GetTxIdsByAddress(tx2.Recipient))

	seed := []byte("seedseed")
	rand.Seed(int64(binary.LittleEndian.Uint64(seed)))
	items, err := pool.GetTxsForBlock(1, getState)
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
	signer := signing.NewEdSigner()
	origin := types.BytesToAddress(signer.PublicKey().Bytes())
	const numTxs = 10

	ids := make([]types.TransactionId, numTxs)
	for i := uint64(0); i < numTxs; i++ {
		id, tx := newTx(t, prevNonce+i, 50, signer)
		ids[i] = id
		fmt.Printf("%d: %v\n", i, id.ShortString())
		pool.Put(id, tx)
	}

	nonce, balance := pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce+numTxs, nonce)
	r.Equal(prevBalance-(50*numTxs), balance)

	seed := []byte("seedseed")
	rand.Seed(int64(binary.LittleEndian.Uint64(seed)))
	txs, err := pool.GetTxsForBlock(5, getState)
	r.NoError(err)
	r.Len(txs, 5)
	var txIds []types.TransactionId
	for _, tx := range txs {
		txIds = append(txIds, tx)
	}
	r.ElementsMatch([]types.TransactionId{ids[6], ids[1], ids[0], ids[7], ids[4]}, txIds)
	/*
		4cb91d90ed9c895dc96a0871fa60dab971f5e3b521672522ebbdbb1c4913732b
		b554c66ebf9f9f335d6dc1facc0ee86e0b6aaf283e5a60b7d2846754808b230d
		b791d54444fa6fec3be511c3e02906ccab7dec03611d61b8b5fc6a5a8015320f
		b21487d43b2bcd27fe1caadff4ddf846e10e0722a78174780a67200bf1dab27e
		570ba6339ad10894fc56e158215c3674207f144a40d29871d01603b08fc07ad1
	*/
}

func TestGetRandIdxs(t *testing.T) {
	seed := []byte("seedseed")
	rand.Seed(int64(binary.LittleEndian.Uint64(seed)))

	idxs := getRandIdxs(5, 10)

	var idsList []uint64
	for id := range idxs {
		idsList = append(idsList, id)
	}
	require.ElementsMatch(t, []uint64{0, 1, 4, 6, 7}, idsList)

	idxs = getRandIdxs(5, 10)

	idsList = []uint64{}
	for id := range idxs {
		idsList = append(idsList, id)
	}
	require.ElementsMatch(t, []uint64{2, 5, 6, 7, 9}, idsList) // new call -> different indices
}

func newTx(t testing.TB, nonce, totalAmount uint64, signer *signing.EdSigner) (types.TransactionId, *types.Transaction) {
	feeAmount := uint64(1)
	rec := types.Address{byte(rand.Int()), byte(rand.Int()), byte(rand.Int()), byte(rand.Int())}
	tx, err := mesh.NewSignedTx(nonce, rec, totalAmount-feeAmount, 3, feeAmount, signer)
	require.NoError(t, err)
	return tx.Id(), tx
}

func BenchmarkTxPoolWithAccounts(b *testing.B) {
	pool := NewTxMemPool()

	const numBatches = 10
	txBatches := make([][]*types.Transaction, numBatches)
	txIdBatches := make([][]types.TransactionId, numBatches)
	for i := 0; i < numBatches; i++ {
		signer := signing.NewEdSigner()
		txBatches[i], txIdBatches[i] = createBatch(b, signer)
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

func createBatch(t testing.TB, signer *signing.EdSigner) ([]*types.Transaction, []types.TransactionId) {
	var txBatch []*types.Transaction
	var txIdBatch []types.TransactionId
	for i := uint64(0); i < 10000; i++ {
		txId, tx := newTx(t, 5+i, 50, signer)
		txBatch = append(txBatch, tx)
		txIdBatch = append(txIdBatch, txId)
	}
	return txBatch, txIdBatch
}
