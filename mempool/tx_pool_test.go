package mempool

import (
	"encoding/binary"
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/require"
)

func getState(types.Address) (uint64, uint64, error) {
	return 5, 1000, nil
}

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

	tx1 := newTx(t, 4, 50, signer)
	pool.Put(tx1.ID(), tx1)
	nonce, balance = pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-50, balance)
	r.ElementsMatch([]*types.Transaction{tx1}, pool.GetTxsByAddress(origin))
	r.ElementsMatch([]*types.Transaction{tx1}, pool.GetTxsByAddress(tx1.Recipient))

	tx2 := newTx(t, 5, 150, signer)
	pool.Put(tx2.ID(), tx2)
	nonce, balance = pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-150, balance)
	r.ElementsMatch([]*types.Transaction{tx1, tx2}, pool.GetTxsByAddress(origin))
	r.ElementsMatch([]*types.Transaction{tx1}, pool.GetTxsByAddress(tx1.Recipient))
	r.ElementsMatch([]*types.Transaction{tx2}, pool.GetTxsByAddress(tx2.Recipient))

	pool.Invalidate(tx1.ID())
	nonce, balance = pool.GetProjection(origin, prevNonce+1, prevBalance-50)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-150, balance)
	r.ElementsMatch([]*types.Transaction{tx2}, pool.GetTxsByAddress(origin))
	r.Empty(pool.GetTxsByAddress(tx1.Recipient))
	r.ElementsMatch([]*types.Transaction{tx2}, pool.GetTxsByAddress(tx2.Recipient))

	seed := []byte("seedseed")
	rand.Seed(int64(binary.LittleEndian.Uint64(seed)))
	items, _, err := pool.GetTxsForBlock(1, getState)
	r.NoError(err)
	r.Len(items, 1)
	r.Equal(tx2.ID(), items[0])
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

	ids := make([]types.TransactionID, numTxs)
	for i := uint64(0); i < numTxs; i++ {
		tx := newTx(t, prevNonce+i, 50, signer)
		ids[i] = tx.ID()
		pool.Put(ids[i], tx)
	}

	nonce, balance := pool.GetProjection(origin, prevNonce, prevBalance)
	r.Equal(prevNonce+numTxs, nonce)
	r.Equal(prevBalance-(50*numTxs), balance)

	seed := []byte("seedseed")
	rand.Seed(int64(binary.LittleEndian.Uint64(seed)))
	txs, _, err := pool.GetTxsForBlock(5, getState)
	r.NoError(err)
	r.Len(txs, 5)
	var txIds []types.TransactionID
	for _, tx := range txs {
		txIds = append(txIds, tx)
	}
	r.ElementsMatch([]types.TransactionID{ids[6], ids[1], ids[0], ids[7], ids[4]}, txIds)
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

func BenchmarkTxPoolWithAccounts(b *testing.B) {
	pool := NewTxMemPool()

	const numBatches = 10
	txBatches := make([][]*types.Transaction, numBatches)
	txIDBatches := make([][]types.TransactionID, numBatches)
	for i := 0; i < numBatches; i++ {
		signer := signing.NewEdSigner()
		txBatches[i], txIDBatches[i] = createBatch(b, signer)
	}

	wg := &sync.WaitGroup{}

	start := time.Now()
	wg.Add(numBatches)
	for i, batch := range txBatches {
		go addBatch(pool, batch, txIDBatches[i], wg)
	}
	wg.Wait()
	wg.Add(numBatches)
	for _, batch := range txIDBatches {
		go invalidateBatch(pool, batch, wg)
	}
	wg.Wait()
	b.Log(time.Since(start))
}

func addBatch(pool *TxMempool, txBatch []*types.Transaction, txIDBatch []types.TransactionID, wg *sync.WaitGroup) {
	for i, tx := range txBatch {
		pool.Put(txIDBatch[i], tx)
	}
	wg.Done()
}

func invalidateBatch(pool *TxMempool, txIDBatch []types.TransactionID, wg *sync.WaitGroup) {
	for _, txID := range txIDBatch {
		pool.Invalidate(txID)
	}
	wg.Done()
}

func createBatch(t testing.TB, signer *signing.EdSigner) ([]*types.Transaction, []types.TransactionID) {
	var txBatch []*types.Transaction
	var txIDBatch []types.TransactionID
	for i := uint64(0); i < 10000; i++ {
		tx, err := types.NewSignedTx(5+1, types.Address{}, 50, 100, 1, signer)
		require.NoError(t, err)
		//tx := newTx(t, 5+i, 50, signer)
		txBatch = append(txBatch, tx)
		txIDBatch = append(txIDBatch, tx.ID())
	}
	return txBatch, txIDBatch
}

func newTx(t *testing.T, nonce, totalAmount uint64, signer *signing.EdSigner) *types.Transaction {
	feeAmount := uint64(1)
	rec := types.Address{byte(rand.Int()), byte(rand.Int()), byte(rand.Int()), byte(rand.Int())}
	return createTransaction(t, nonce, rec, totalAmount-feeAmount, feeAmount, signer)
}

func createTransaction(t *testing.T, nonce uint64, destination types.Address, amount, fee uint64, signer *signing.EdSigner) *types.Transaction {
	tx, err := types.NewSignedTx(nonce, destination, amount, 100, fee, signer)
	assert.NoError(t, err)
	return tx
}
