package txs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

const (
	numTXs = 29
	nonce  = uint64(1234)
)

func TestStore_AddToProposal(t *testing.T) {
	db := sql.InMemory()
	st := newStore(db)
	txs := make([]*types.Transaction, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
		require.NoError(t, st.Add(tx, time.Now()))
		txs = append(txs, tx)
	}
	lid0 := types.NewLayerID(233)
	pid0 := types.ProposalID{1, 2, 3}
	lid1 := lid0.Add(1)
	pid1 := types.ProposalID{2, 3, 4}
	require.NoError(t, st.AddToProposal(lid0, pid0, types.ToTransactionIDs(txs)))
	require.NoError(t, st.AddToProposal(lid1, pid1, types.ToTransactionIDs(txs)))

	for _, tx := range txs {
		mtx, err := st.Get(tx.ID())
		require.NoError(t, err)
		require.Equal(t, lid0, mtx.LayerID)
		require.Equal(t, types.EmptyBlockID, mtx.BlockID)
		has, err := transactions.HasProposalTX(db, pid0, tx.ID())
		require.NoError(t, err)
		require.True(t, has)
		has, err = transactions.HasProposalTX(db, pid1, tx.ID())
		require.NoError(t, err)
		require.True(t, has)
	}
}

func TestStore_AddToBlock(t *testing.T) {
	db := sql.InMemory()
	st := newStore(db)
	txs := make([]*types.Transaction, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
		require.NoError(t, st.Add(tx, time.Now()))
		txs = append(txs, tx)
	}
	lid0 := types.NewLayerID(233)
	bid0 := types.BlockID{1, 2, 3}
	lid1 := lid0.Add(1)
	bid1 := types.BlockID{2, 3, 4}
	require.NoError(t, st.AddToBlock(lid0, bid0, types.ToTransactionIDs(txs)))
	require.NoError(t, st.AddToBlock(lid1, bid1, types.ToTransactionIDs(txs)))

	for _, tx := range txs {
		mtx, err := st.Get(tx.ID())
		require.NoError(t, err)
		require.Equal(t, lid0, mtx.LayerID)
		require.Equal(t, bid0, mtx.BlockID)
		has, err := transactions.HasBlockTX(db, bid0, tx.ID())
		require.NoError(t, err)
		require.True(t, has)
		has, err = transactions.HasBlockTX(db, bid1, tx.ID())
		require.NoError(t, err)
		require.True(t, has)
	}
}

func TestStore_ApplyLayer(t *testing.T) {
	db := sql.InMemory()
	st := newStore(db)
	txs := make([]*types.Transaction, 0, numTXs)
	signer := signing.NewEdSigner()
	// create a bunch of transactions with competing txs for the same nonce
	for i := 0; i < numTXs; i++ {
		tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
		require.NoError(t, st.Add(tx, time.Now()))
		txs = append(txs, tx)
	}

	// create a bunch of transactions for different account
	for i := 0; i < numTXs; i++ {
		s := signing.NewEdSigner()
		tx := newTx(t, nonce, defaultAmount, defaultFee, s)
		require.NoError(t, st.Add(tx, time.Now()))
		txs = append(txs, tx)
	}

	lid := types.NewLayerID(233)
	bid := types.BlockID{1, 2, 3}
	principal := types.BytesToAddress(signer.PublicKey().Bytes())
	applied := txs[numTXs-1]
	require.NoError(t, st.ApplyLayer(lid, bid, principal, map[uint64]types.TransactionID{nonce: applied.ID()}))

	for _, tx := range txs {
		mtx, err := st.Get(tx.ID())
		require.NoError(t, err)
		if tx.Origin() != principal {
			require.Equal(t, types.MEMPOOL, mtx.State)
			continue
		}

		require.Equal(t, lid, mtx.LayerID)
		if tx == applied {
			require.Equal(t, types.APPLIED, mtx.State)
		} else {
			require.Equal(t, types.DISCARDED, mtx.State)
		}
	}
}

func TestStore_UndoLayers_Simple(t *testing.T) {
	db := sql.InMemory()
	st := newStore(db)
	lid := types.NewLayerID(233)
	numLayers := 5
	txs := make([]*types.Transaction, 0, numLayers*numTXs)
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		principal := types.BytesToAddress(signer.PublicKey().Bytes())
		for j := 0; j < numLayers; j++ {
			lyr := lid.Add(uint32(j))
			nnc := nonce + uint64(j)
			tx := newTx(t, nnc, defaultAmount, defaultFee, signer)
			require.NoError(t, st.Add(tx, time.Now()))
			txs = append(txs, tx)
			require.NoError(t, st.ApplyLayer(lyr, types.RandomBlockID(), principal, map[uint64]types.TransactionID{nnc: tx.ID()}))
		}
	}
	got, err := st.GetAllPending()
	require.NoError(t, err)
	require.Len(t, got, 0)

	for i, tx := range txs {
		mtx, err := st.Get(tx.ID())
		require.NoError(t, err)
		require.Equal(t, lid.Add(uint32(i%numLayers)), mtx.LayerID)
		require.Equal(t, types.APPLIED, mtx.State)
	}

	for i := numLayers - 1; i >= 0; i-- {
		require.NoError(t, st.UndoLayers(lid.Add(uint32(i))))
		got, err = st.GetAllPending()
		require.NoError(t, err)
		require.Len(t, got, numTXs*(numLayers-i))
	}
}

func TestStore_UndoLayers_TXsInMultipleLayers(t *testing.T) {
	db := sql.InMemory()
	st := newStore(db)
	signerA := signing.NewEdSigner()
	principalA := types.BytesToAddress(signerA.PublicKey().Bytes())
	signerB := signing.NewEdSigner()
	principalB := types.BytesToAddress(signerB.PublicKey().Bytes())
	txA0 := newTx(t, nonce, defaultAmount, defaultFee, signerA)
	txA1 := newTx(t, nonce+1, defaultAmount, defaultFee, signerA)
	txA2 := newTx(t, nonce+2, defaultAmount, defaultFee, signerA)
	txB0 := newTx(t, nonce, defaultAmount, defaultFee, signerB)
	txB1 := newTx(t, nonce+1, defaultAmount, defaultFee, signerB)
	txs := []*types.Transaction{txA0, txA1, txA2, txB0, txB1}
	for _, tx := range txs {
		require.NoError(t, st.Add(tx, time.Now()))
	}
	lid0 := types.NewLayerID(233)
	lid1 := lid0.Add(1)
	pid0 := types.ProposalID{1, 2, 3}
	pid1 := types.ProposalID{2, 3, 4}
	require.NoError(t, st.AddToProposal(lid0, pid0, []types.TransactionID{txA0.ID(), txA1.ID(), txA2.ID(), txB0.ID()}))
	require.NoError(t, st.AddToProposal(lid1, pid1, []types.TransactionID{txA1.ID(), txA2.ID(), txB1.ID()}))

	bid0 := types.BlockID{5, 6, 7}
	// for some reason txA1 and txA2 were not included in the block
	require.NoError(t, st.AddToBlock(lid0, bid0, []types.TransactionID{txA0.ID(), txB0.ID()}))
	require.NoError(t, st.ApplyLayer(lid0, bid0, principalA, map[uint64]types.TransactionID{
		txA0.AccountNonce: txA0.ID(),
	}))
	require.NoError(t, st.ApplyLayer(lid0, bid0, principalB, map[uint64]types.TransactionID{
		txB0.AccountNonce: txB0.ID(),
	}))

	bid1 := types.BlockID{6, 7, 8}
	require.NoError(t, st.AddToBlock(lid1, bid1, []types.TransactionID{txA1.ID(), txA2.ID(), txB1.ID()}))
	require.NoError(t, st.ApplyLayer(lid1, bid1, principalA, map[uint64]types.TransactionID{
		txA1.AccountNonce: txA1.ID(),
		txA2.AccountNonce: txA2.ID(),
	}))
	require.NoError(t, st.ApplyLayer(lid1, bid1, principalB, map[uint64]types.TransactionID{
		txB1.AccountNonce: txB1.ID(),
	}))

	require.NoError(t, st.UndoLayers(lid0))
	for _, tx := range []*types.Transaction{txA0, txB0} {
		got, err := st.Get(tx.ID())
		require.NoError(t, err)
		require.Equal(t, types.BLOCK, got.State)
		require.Equal(t, lid0, got.LayerID)
	}
	for _, tx := range []*types.Transaction{txA1, txA2} {
		got, err := st.Get(tx.ID())
		require.NoError(t, err)
		require.Equal(t, types.PROPOSAL, got.State)
		require.Equal(t, lid0, got.LayerID)
	}
	got, err := st.Get(txB1.ID())
	require.NoError(t, err)
	require.Equal(t, types.BLOCK, got.State)
	require.Equal(t, lid1, got.LayerID)
}
