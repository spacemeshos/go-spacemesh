package txs

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/txs/mocks"
	"github.com/spacemeshos/go-spacemesh/vm/transaction"
)

const (
	numTXsInProposal = 5
	defaultBalance   = uint64(1000)
	amount           = uint64(10)
	fee              = uint64(5)
)

func newTx(t *testing.T, nonce uint64, amount, fee uint64, signer *signing.EdSigner) *types.Transaction {
	dest := types.Address{byte(rand.Int()), byte(rand.Int()), byte(rand.Int()), byte(rand.Int())}
	return newTxWthRecipient(t, dest, nonce, amount, fee, signer)
}

func newTxWthRecipient(t *testing.T, dest types.Address, nonce uint64, amount, fee uint64, signer *signing.EdSigner) *types.Transaction {
	tx, err := transaction.GenerateCallTransaction(signer, dest, nonce, amount, 100, fee)
	assert.NoError(t, err)
	return tx
}

type testConState struct {
	*ConservativeState
	db   *sql.Database
	mSVM *mocks.MocksvmState
}

func createConservativeState(t *testing.T) *testConState {
	ctrl := gomock.NewController(t)
	mockSvm := mocks.NewMocksvmState(ctrl)
	db := sql.InMemory()
	return &testConState{
		ConservativeState: NewConservativeState(mockSvm, db, logtest.New(t)),
		db:                db,
		mSVM:              mockSvm,
	}
}

func addBatch(t *testing.T, tcs *testConState, numTXs int) ([]types.TransactionID, []*types.Transaction) {
	t.Helper()
	ids := make([]types.TransactionID, 0, numTXs)
	txs := make([]*types.Transaction, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		addr := types.BytesToAddress(signer.PublicKey().Bytes())
		tcs.mSVM.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mSVM.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
		tx := newTx(t, nonce, amount, fee, signer)
		require.NoError(t, tcs.AddToCache(tx, true))
		ids = append(ids, tx.ID())
		txs = append(txs, tx)
	}
	return ids, txs
}

func TestSelectTXsForProposal(t *testing.T) {
	tcs := createConservativeState(t)
	numTXs := 2 * numTXsInProposal
	lid := types.NewLayerID(97)
	bid := types.BlockID{100}
	expected := make([]types.TransactionID, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mSVM.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mSVM.EXPECT().GetNonce(addr).Return(uint64(0), nil).Times(1)
		tx1 := newTx(t, 0, amount, fee, signer)
		require.NoError(t, tcs.AddToCache(tx1, true))
		// all the TXs with nonce 0 are pending in database
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx1.ID()}))
		tx2 := newTx(t, 1, amount, fee, signer)
		require.NoError(t, tcs.AddToCache(tx2, true))
		expected = append(expected, tx2.ID())
	}
	got, err := tcs.SelectTXsForProposal(numTXsInProposal)
	require.NoError(t, err)
	require.ElementsMatch(t, expected[:numTXsInProposal], got)
}

func TestSelectTXsForProposal_ExhaustMemPool(t *testing.T) {
	tcs := createConservativeState(t)
	numTXs := numTXsInProposal - 1
	lid := types.NewLayerID(97)
	bid := types.BlockID{100}
	expected := make([]types.TransactionID, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mSVM.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mSVM.EXPECT().GetNonce(addr).Return(uint64(0), nil).Times(1)
		tx1 := newTx(t, 0, amount, fee, signer)
		require.NoError(t, tcs.AddToCache(tx1, true))
		// all the TXs with nonce 0 are pending in database
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx1.ID()}))
		tx2 := newTx(t, 1, amount, fee, signer)
		require.NoError(t, tcs.AddToCache(tx2, true))
		expected = append(expected, tx2.ID())
	}
	got, err := tcs.SelectTXsForProposal(numTXsInProposal)
	require.NoError(t, err)
	require.ElementsMatch(t, expected, got)
}

func TestSelectTXsForProposal_SamePrincipal(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	numTXs := numTXsInProposal * 2
	numInBlock := numTXsInProposal
	lid := types.NewLayerID(97)
	bid := types.BlockID{100}
	tcs.mSVM.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mSVM.EXPECT().GetNonce(addr).Return(uint64(0), nil).Times(1)
	for i := 0; i < numInBlock; i++ {
		tx := newTx(t, uint64(i), amount, fee, signer)
		require.NoError(t, tcs.AddToCache(tx, true))
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID()}))
	}
	expected := make([]types.TransactionID, 0, numTXsInProposal)
	for i := 0; i < numTXs; i++ {
		tx := newTx(t, uint64(numInBlock+i), amount, fee, signer)
		require.NoError(t, tcs.AddToCache(tx, true))
		if i < numTXsInProposal {
			expected = append(expected, tx.ID())
		}
	}
	got, err := tcs.SelectTXsForProposal(numTXsInProposal)
	require.NoError(t, err)
	require.ElementsMatch(t, expected, got)
}

func TestSelectTXsForProposal_TwoPrincipals(t *testing.T) {
	const (
		numInProposal = 30
		numTXs        = numInProposal * 2
		numInDBs      = numInProposal
	)
	tcs := createConservativeState(t)
	signer1 := signing.NewEdSigner()
	addr1 := types.GenerateAddress(signer1.PublicKey().Bytes())
	signer2 := signing.NewEdSigner()
	addr2 := types.GenerateAddress(signer2.PublicKey().Bytes())
	lid := types.NewLayerID(97)
	bid := types.BlockID{100}
	tcs.mSVM.EXPECT().GetBalance(addr1).Return(defaultBalance*100, nil).Times(1)
	tcs.mSVM.EXPECT().GetNonce(addr1).Return(uint64(0), nil).Times(1)
	tcs.mSVM.EXPECT().GetBalance(addr2).Return(defaultBalance*100, nil).Times(1)
	tcs.mSVM.EXPECT().GetNonce(addr2).Return(uint64(0), nil).Times(1)
	allTXs := make(map[types.TransactionID]*types.Transaction)
	for i := 0; i < numInDBs; i++ {
		tx := newTx(t, uint64(i), amount, fee, signer1)
		require.NoError(t, tcs.AddToCache(tx, true))
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID()}))
		allTXs[tx.ID()] = tx
		tx = newTx(t, uint64(i), amount, fee, signer2)
		require.NoError(t, tcs.AddToCache(tx, true))
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID()}))
		allTXs[tx.ID()] = tx
	}
	for i := 0; i < numTXs; i++ {
		tx := newTx(t, uint64(numInDBs+i), amount, fee, signer1)
		require.NoError(t, tcs.AddToCache(tx, true))
		allTXs[tx.ID()] = tx
		tx = newTx(t, uint64(numInDBs+i), amount, fee, signer2)
		require.NoError(t, tcs.AddToCache(tx, true))
		allTXs[tx.ID()] = tx
	}
	got, err := tcs.SelectTXsForProposal(numInProposal)
	require.NoError(t, err)
	// the odds of picking just one principal is 2^30
	chosen := make(map[types.Address][]*types.Transaction)
	for _, tid := range got {
		tx := allTXs[tid]
		chosen[tx.Origin()] = append(chosen[tx.Origin()], tx)
	}
	assert.Len(t, chosen, 2)
	require.Contains(t, chosen, addr1)
	require.Contains(t, chosen, addr2)
	// making sure nonce values are in order
	for i, tx := range chosen[addr1] {
		require.Equal(t, uint64(i+numInDBs), tx.AccountNonce)
	}
	for i, tx := range chosen[addr2] {
		require.Equal(t, uint64(i+numInDBs), tx.AccountNonce)
	}
}

func TestGetProjection(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.BytesToAddress(signer.PublicKey().Bytes())
	tcs.mSVM.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mSVM.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx1 := newTx(t, nonce, amount, fee, signer)
	require.NoError(t, tcs.AddToCache(tx1, true))
	require.NoError(t, tcs.LinkTXsWithBlock(types.NewLayerID(10), types.BlockID{100}, []types.TransactionID{tx1.ID()}))
	tx2 := newTx(t, nonce+1, amount, fee, signer)
	require.NoError(t, tcs.AddToCache(tx2, true))

	got, balance := tcs.GetProjection(addr)
	assert.EqualValues(t, nonce+2, got)
	assert.EqualValues(t, defaultBalance-2*(amount+fee), balance)
}

func TestAddToCache(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.BytesToAddress(signer.PublicKey().Bytes())
	tcs.mSVM.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mSVM.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx := newTx(t, nonce, amount, fee, signer)
	assert.NoError(t, tcs.AddToCache(tx, true))
	got, err := tcs.HasTx(tx.ID())
	require.NoError(t, err)
	assert.True(t, got)
}

func TestAddToCache_KnownTX(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.BytesToAddress(signer.PublicKey().Bytes())
	tcs.mSVM.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mSVM.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx := newTx(t, nonce, amount, fee, signer)
	assert.NoError(t, tcs.AddToCache(tx, false))
	got, err := tcs.HasTx(tx.ID())
	require.NoError(t, err)
	assert.True(t, got)
}

func TestAddToCache_InsufficientBalance(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.BytesToAddress(signer.PublicKey().Bytes())
	tcs.mSVM.EXPECT().GetBalance(addr).Return(amount, nil).Times(1)
	tcs.mSVM.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx := newTx(t, nonce, amount, fee, signer)
	err := tcs.AddToCache(tx, true)
	assert.ErrorIs(t, err, errInsufficientBalance)
}

func TestGetMeshTransaction(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.BytesToAddress(signer.PublicKey().Bytes())
	tcs.mSVM.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mSVM.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx := newTx(t, nonce, amount, fee, signer)
	assert.NoError(t, tcs.AddToCache(tx, true))
	mtx, err := tcs.GetMeshTransaction(tx.ID())
	require.NoError(t, err)
	assert.Equal(t, types.MEMPOOL, mtx.State)

	lid := types.NewLayerID(10)
	pid := types.ProposalID{1, 2, 3}
	require.NoError(t, tcs.LinkTXsWithProposal(lid, pid, []types.TransactionID{tx.ID()}))
	mtx, err = tcs.GetMeshTransaction(tx.ID())
	require.NoError(t, err)
	assert.Equal(t, types.PROPOSAL, mtx.State)

	bid := types.BlockID{2, 3, 4}
	require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID()}))
	mtx, err = tcs.GetMeshTransaction(tx.ID())
	require.NoError(t, err)
	require.Equal(t, types.BLOCK, mtx.State)
}

func TestGetMeshTransactions(t *testing.T) {
	tcs := createConservativeState(t)
	tids, txs := addBatch(t, tcs, numTXs)
	badIDs := []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()}
	allIDs := append(tids, badIDs...)

	mtxs, missing := tcs.GetMeshTransactions(allIDs)
	require.Len(t, tids, numTXs)
	for i, mtx := range mtxs {
		require.Equal(t, *txs[i], mtx.Transaction)
	}
	assert.Len(t, missing, len(badIDs))
	for _, id := range badIDs {
		assert.NotNil(t, missing[id])
	}
}

func TestGetTransactionsByAddress(t *testing.T) {
	tcs := createConservativeState(t)

	signer1 := signing.NewEdSigner()
	addr1 := types.GenerateAddress(signer1.PublicKey().Bytes())
	signer2 := signing.NewEdSigner()
	addr2 := types.GenerateAddress(signer2.PublicKey().Bytes())
	signer3 := signing.NewEdSigner()
	addr3 := types.GenerateAddress(signer3.PublicKey().Bytes())

	mtxs, err := tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(100), addr1)
	require.NoError(t, err)
	require.Empty(t, mtxs)
	mtxs, err = tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(100), addr2)
	require.NoError(t, err)
	require.Empty(t, mtxs)
	mtxs, err = tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(100), addr3)
	require.NoError(t, err)
	require.Empty(t, mtxs)

	tcs.mSVM.EXPECT().GetBalance(addr1).Return(defaultBalance, nil).Times(1)
	tcs.mSVM.EXPECT().GetNonce(addr1).Return(nonce, nil).Times(1)
	tx0 := newTxWthRecipient(t, addr2, nonce, amount, fee, signer1)
	assert.NoError(t, tcs.AddToCache(tx0, true))
	tx1 := newTxWthRecipient(t, addr3, nonce+1, amount, fee, signer1)
	assert.NoError(t, tcs.AddToCache(tx1, true))
	require.NoError(t, tcs.LinkTXsWithBlock(types.NewLayerID(5), types.BlockID{11}, []types.TransactionID{tx1.ID()}))

	tcs.mSVM.EXPECT().GetBalance(addr2).Return(defaultBalance, nil).Times(1)
	tcs.mSVM.EXPECT().GetNonce(addr2).Return(nonce, nil).Times(1)
	tx2 := newTxWthRecipient(t, addr3, nonce, amount, fee, signer2)
	assert.NoError(t, tcs.AddToCache(tx2, true))
	tx3 := newTxWthRecipient(t, addr3, nonce+1, amount, fee, signer2)
	assert.NoError(t, tcs.AddToCache(tx3, true))
	require.NoError(t, tcs.LinkTXsWithBlock(types.NewLayerID(6), types.BlockID{12}, []types.TransactionID{tx3.ID()}))

	// nothing in the range of 1-4
	mtxs, err = tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(4), addr1)
	require.NoError(t, err)
	require.Empty(t, mtxs)

	mtxs, err = tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(5), addr1)
	require.NoError(t, err)
	require.Len(t, mtxs, 1)
	assert.Equal(t, mtxs[0].Transaction, *tx1)

	mtxs, err = tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(6), addr2)
	require.NoError(t, err)
	require.Len(t, mtxs, 1)
	assert.Equal(t, mtxs[0].Transaction, *tx3)

	mtxs, err = tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(6), addr3)
	require.NoError(t, err)
	require.Len(t, mtxs, 2)
	assert.Equal(t, mtxs[0].Transaction, *tx1)
	assert.Equal(t, mtxs[1].Transaction, *tx3)
}

func TestApplyLayer(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.NewLayerID(1)
	ids, txs := addBatch(t, tcs, numTXs)
	coinbase := types.GenerateAddress(types.RandomBytes(20))
	reward := uint64(200)
	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{
			LayerIndex: lid,
			Rewards: []types.AnyReward{
				{Address: coinbase, Amount: reward},
			},
			TxIDs: ids,
		})
	rewards := []types.AnyReward{{Address: coinbase, Amount: reward}}
	for _, tx := range txs {
		tcs.mSVM.EXPECT().GetBalance(tx.Origin()).Return(defaultBalance-(amount+fee), nil)
		tcs.mSVM.EXPECT().GetNonce(tx.Origin()).Return(nonce+1, nil)
	}
	tcs.mSVM.EXPECT().ApplyLayer(lid, gomock.Any(), rewards).DoAndReturn(
		func(_ types.LayerID, got []*types.Transaction, _ []types.AnyReward) ([]*types.Transaction, error) {
			require.ElementsMatch(t, got, txs)
			return nil, nil
		}).Times(1)
	failed, err := tcs.ApplyLayer(block)
	require.NoError(t, err)
	assert.Empty(t, failed)

	for _, id := range ids {
		mtx, err := tcs.GetMeshTransaction(id)
		require.NoError(t, err)
		assert.Equal(t, types.APPLIED, mtx.State)
		assert.Equal(t, block.ID(), mtx.BlockID)
		assert.Equal(t, lid, mtx.LayerID)
	}
}

func TestApplyLayer_OutOfOrder(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.NewLayerID(10)
	ids, _ := addBatch(t, tcs, numTXs)
	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{
			LayerIndex: lid,
			TxIDs:      ids,
		})
	_, err := tcs.ApplyLayer(block)
	require.ErrorIs(t, err, errLayerNotInOrder)
}

func TestApplyLayer_Failed(t *testing.T) {
	const numFailed = 2
	tcs := createConservativeState(t)
	lid := types.NewLayerID(1)
	ids, txs := addBatch(t, tcs, numTXs)
	coinbase := types.GenerateAddress(types.RandomBytes(20))
	reward := uint64(200)
	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{
			LayerIndex: lid,
			Rewards: []types.AnyReward{
				{Address: coinbase, Amount: reward},
			},
			TxIDs: ids,
		})
	rewards := []types.AnyReward{{Address: coinbase, Amount: reward}}
	errSVM := errors.New("svm")
	tcs.mSVM.EXPECT().ApplyLayer(lid, gomock.Any(), rewards).DoAndReturn(
		func(_ types.LayerID, got []*types.Transaction, _ []types.AnyReward) ([]*types.Transaction, error) {
			require.ElementsMatch(t, got, txs)
			return got[:numFailed], errSVM
		}).Times(1)
	failed, err := tcs.ApplyLayer(block)
	assert.ErrorIs(t, err, errSVM)
	assert.Len(t, failed, numFailed)

	for _, id := range ids {
		mtx, err := tcs.GetMeshTransaction(id)
		require.NoError(t, err)
		assert.Equal(t, types.MEMPOOL, mtx.State)
		assert.Equal(t, types.EmptyBlockID, mtx.BlockID)
		assert.Equal(t, types.LayerID{}, mtx.LayerID)
	}
}

func TestTXFetcher(t *testing.T) {
	tcs := createConservativeState(t)
	ids, txs := addBatch(t, tcs, numTXs)
	for i, id := range ids {
		buf, err := tcs.Transactions().Get(id.Bytes())
		require.NoError(t, err)
		var rst types.Transaction
		require.NoError(t, codec.Decode(buf, &rst))
		require.NoError(t, rst.CalcAndSetOrigin())
		rst.ID() // side effects
		require.Equal(t, txs[i], &rst)
	}
}
