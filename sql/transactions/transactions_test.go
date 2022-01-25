package transactions

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
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
		mustTx(types.NewSignedTx(1, types.Address{1}, 191, 1, 1, signer1)),
		mustTx(types.NewSignedTx(1, types.Address{2}, 191, 1, 1, signer2)),
		mustTx(types.NewSignedTx(1, types.Address{3}, 191, 1, 1, signer1)),
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
	tx := mustTx(types.NewSignedTx(1, types.Address{1}, 191, 1, 1, signer))

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
		mustTx(types.NewSignedTx(1, types.Address{1}, 191, 1, 1, signer)),
		mustTx(types.NewSignedTx(1, types.Address{2}, 191, 1, 1, signer)),
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
		mustTx(types.NewSignedTx(1, types.Address{1}, 191, 1, 1, signer)),
		mustTx(types.NewSignedTx(1, types.Address{2}, 191, 1, 1, signer)),
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
		mustTx(types.NewSignedTx(1, types.Address{1}, 191, 1, 1, signer1)),
		mustTx(types.NewSignedTx(1, types.Address{2}, 191, 1, 1, signer2)),
		mustTx(types.NewSignedTx(1, types.Address{2}, 191, 1, 1, signer1)),
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
			mustTx(types.NewSignedTx(uint64(i), types.Address{1}, 191, 1, 1, signer))))
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
		mustTx(types.NewSignedTx(1, types.Address{1}, 191, 1, 1, signer1)),
		mustTx(types.NewSignedTx(1, types.Address{2}, 191, 1, 1, signer2)),
		mustTx(types.NewSignedTx(1, signer2Address, 191, 1, 1, signer1)),
	}
	for _, tx := range txs {
		require.NoError(t, Add(db, lid, bid, tx))
	}
	filtered, err := FilterByAddress(db, lid, lid, signer2Address)
	require.NoError(t, err)
	require.Len(t, filtered, 2)
}
