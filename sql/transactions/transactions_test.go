package transactions

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/svm/transaction"
)

func mustTx(tx *types.Transaction, err error) *types.Transaction {
	if err != nil {
		panic(err)
	}
	return tx
}

func TestGetHas(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer1 := signing.NewEdSignerFromRand(rng)
	signer2 := signing.NewEdSignerFromRand(rng)
	lid := types.NewLayerID(10)
	bid := types.BlockID{1, 1}
	txs := []*types.Transaction{
		mustTx(transaction.GenerateCallTransaction(signer1, types.Address{1}, 1, 191, 1, 1)),
		mustTx(transaction.GenerateCallTransaction(signer2, types.Address{2}, 1, 191, 1, 1)),
		mustTx(transaction.GenerateCallTransaction(signer1, types.Address{3}, 1, 191, 1, 1)),
	}

	for _, tx := range txs {
		require.NoError(t, Add(db, lid, bid, tx))
	}
	for _, tx := range txs {
		received, err := Get(db, tx.ID())
		require.NoError(t, err)
		require.Equal(t, &types.MeshTransaction{
			Transaction: *tx,
			LayerID:     lid,
			BlockID:     bid,
		}, received)
		has, err := Has(db, tx.ID())
		require.NoError(t, err)
		require.True(t, has)
	}
}

func TestGetBlob(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer := signing.NewEdSignerFromRand(rng)
	lid := types.NewLayerID(10)
	bid := types.BlockID{1, 1}
	tx := mustTx(transaction.GenerateCallTransaction(signer, types.Address{1}, 1, 191, 1, 1))

	require.NoError(t, Add(db, lid, bid, tx))
	buf, err := GetBlob(db, tx.ID())
	require.NoError(t, err)
	encoded, err := codec.Encode(tx)
	require.NoError(t, err)
	require.Equal(t, encoded, buf)
}

func TestPending(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer := signing.NewEdSignerFromRand(rng)
	lid := types.NewLayerID(10)
	bid := types.BlockID{1, 1}
	txs := []*types.Transaction{
		mustTx(transaction.GenerateCallTransaction(signer, types.Address{1}, 1, 191, 1, 1)),
		mustTx(transaction.GenerateCallTransaction(signer, types.Address{2}, 1, 191, 1, 1)),
	}

	for _, tx := range txs {
		require.NoError(t, Add(db, lid, bid, tx))
	}

	filtered, err := FilterPending(db, txs[0].Origin())
	require.NoError(t, err)
	require.Len(t, filtered, 2)

	for _, tx := range txs {
		require.NoError(t, Applied(db, tx.ID()))
	}
	filtered, err = FilterPending(db, txs[0].Origin())
	require.NoError(t, err)
	require.Empty(t, filtered)
}

func TestDelete(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer := signing.NewEdSignerFromRand(rng)
	lid := types.NewLayerID(10)
	bid := types.BlockID{1, 1}
	txs := []*types.Transaction{
		mustTx(transaction.GenerateCallTransaction(signer, types.Address{1}, 1, 191, 1, 1)),
		mustTx(transaction.GenerateCallTransaction(signer, types.Address{2}, 1, 191, 1, 1)),
	}

	for _, tx := range txs {
		require.NoError(t, Add(db, lid, bid, tx))
		has, err := Has(db, tx.ID())
		require.NoError(t, err)
		require.True(t, has)
		require.NoError(t, MarkDeleted(db, tx.ID()))
		has, err = Has(db, tx.ID())
		require.NoError(t, err)
		require.True(t, has)
	}
}

func TestFilter(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer1 := signing.NewEdSignerFromRand(rng)
	signer2 := signing.NewEdSignerFromRand(rng)
	lid := types.NewLayerID(10)
	bid := types.BlockID{1, 1}
	txs := []*types.Transaction{
		mustTx(transaction.GenerateCallTransaction(signer1, types.Address{1}, 1, 191, 1, 1)),
		mustTx(transaction.GenerateCallTransaction(signer2, types.Address{2}, 1, 191, 1, 1)),
		mustTx(transaction.GenerateCallTransaction(signer1, types.Address{2}, 1, 191, 1, 1)),
	}

	for _, tx := range txs {
		require.NoError(t, Add(db, lid, bid, tx))
	}

	filtered, err := FilterByOrigin(db, lid, lid, txs[1].Origin())
	require.NoError(t, err)
	require.Len(t, filtered, 1)

	filtered, err = FilterByDestination(db, lid, lid, txs[1].Recipient)
	require.NoError(t, err)
	require.Len(t, filtered, 2)
}

func TestFilterLayers(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer := signing.NewEdSignerFromRand(rng)
	start := types.NewLayerID(10)
	const n = 10

	for i := 1; i <= n; i++ {
		require.NoError(t, Add(db, start.Add(uint32(i)), types.BlockID{},
			mustTx(transaction.GenerateCallTransaction(signer, types.Address{1}, uint64(i), 191, 1, 1))))
	}
	for i := 1; i <= n; i++ {
		filtered, err := FilterByOrigin(db,
			start, start.Add(uint32(i)),
			types.BytesToAddress(signer.PublicKey().Bytes()),
		)
		require.NoError(t, err)
		require.Len(t, filtered, i)
	}
}

func TestFilterByAddress(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer1 := signing.NewEdSignerFromRand(rng)
	signer2 := signing.NewEdSignerFromRand(rng)
	signer2Address := types.BytesToAddress(signer2.PublicKey().Bytes())
	lid := types.NewLayerID(10)
	bid := types.BlockID{1, 1}
	txs := []*types.Transaction{
		mustTx(transaction.GenerateCallTransaction(signer1, types.Address{1}, 1, 191, 1, 1)),
		mustTx(transaction.GenerateCallTransaction(signer2, types.Address{2}, 1, 191, 1, 1)),
		mustTx(transaction.GenerateCallTransaction(signer1, signer2Address, 1, 191, 1, 1)),
	}
	for _, tx := range txs {
		require.NoError(t, Add(db, lid, bid, tx))
	}
	// should be nothing before lid
	filtered, err := FilterByAddress(db, types.NewLayerID(1), lid.Sub(1), signer2Address)
	require.NoError(t, err)
	require.Empty(t, filtered)

	filtered, err = FilterByAddress(db, lid, lid, signer2Address)
	require.NoError(t, err)
	require.Len(t, filtered, 2)
}
