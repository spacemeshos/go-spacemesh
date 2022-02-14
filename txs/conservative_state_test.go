package txs

import (
	"encoding/binary"
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
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/svm/transaction"
	"github.com/spacemeshos/go-spacemesh/txs/mocks"
)

const (
	numTXsInProposal = 5
	prevBalance      = uint64(1000)
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

func checkIDsAndTXsMatch(t *testing.T, ids []types.TransactionID, txs []*types.Transaction, expectedSize int) {
	t.Helper()
	assert.Len(t, ids, expectedSize)
	assert.Len(t, txs, expectedSize)
	for i, id := range ids {
		assert.Equal(t, id, txs[i].ID())
	}
}

func writeToDB(t *testing.T, db *sql.Database, lid types.LayerID, bid types.BlockID, tx *types.Transaction) {
	t.Helper()
	require.NoError(t, transactions.Add(db, lid, bid, tx))
}

func TestSelectTXsForProposal(t *testing.T) {
	tcs := createConservativeState(t)
	numTXs := 2 * numTXsInProposal
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mSVM.EXPECT().GetBalance(addr).Return(prevBalance).Times(1)
		tcs.mSVM.EXPECT().GetNonce(addr).Return(uint64(0)).Times(1)
		tx1 := newTx(t, 0, amount, fee, signer)
		// all the TXs with nonce 0 are pending in database
		writeToDB(t, tcs.db, types.NewLayerID(10), types.BlockID{100}, tx1)
		tx2 := newTx(t, 1, amount, fee, signer)
		require.NoError(t, tcs.AddTxToMempool(tx2, false))
	}
	ids, txs, err := tcs.SelectTXsForProposal(numTXsInProposal)
	require.NoError(t, err)
	checkIDsAndTXsMatch(t, ids, txs, numTXsInProposal)
	// make sure pending TXs in db are accounted for
	for _, tx := range txs {
		assert.EqualValues(t, 1, tx.AccountNonce)
	}
}

func TestSelectTXsForProposal_ExhaustMemPool(t *testing.T) {
	tcs := createConservativeState(t)
	numTXs := numTXsInProposal - 1
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mSVM.EXPECT().GetBalance(addr).Return(prevBalance).Times(1)
		tcs.mSVM.EXPECT().GetNonce(addr).Return(uint64(0)).Times(1)
		tx1 := newTx(t, 0, amount, fee, signer)
		// all the TXs with nonce 0 are pending in database
		writeToDB(t, tcs.db, types.NewLayerID(10), types.BlockID{100}, tx1)
		tx2 := newTx(t, 1, amount, fee, signer)
		require.NoError(t, tcs.AddTxToMempool(tx2, false))
	}
	ids, txs, err := tcs.SelectTXsForProposal(numTXsInProposal)
	require.NoError(t, err)
	checkIDsAndTXsMatch(t, ids, txs, numTXs)
	// make sure pending TXs in db are accounted for
	for _, tx := range txs {
		assert.EqualValues(t, 1, tx.AccountNonce)
	}
}

func TestSelectTXsForProposal_SamePrincipal(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	numTXs := numTXsInProposal * 2
	numInDBs := numTXsInProposal
	tcs.mSVM.EXPECT().GetBalance(addr).Return(prevBalance).Times(1)
	tcs.mSVM.EXPECT().GetNonce(addr).Return(uint64(0)).Times(1)
	for i := 0; i < numInDBs; i++ {
		tx := newTx(t, uint64(i), amount, fee, signer)
		writeToDB(t, tcs.db, types.NewLayerID(10), types.BlockID{100}, tx)
	}
	expected := make([]types.TransactionID, 0, numTXsInProposal)
	for i := 0; i < numTXs; i++ {
		tx := newTx(t, uint64(numInDBs+i), amount, fee, signer)
		require.NoError(t, tcs.AddTxToMempool(tx, false))
		if i < numTXsInProposal {
			expected = append(expected, tx.ID())
		}
	}
	ids, txs, err := tcs.SelectTXsForProposal(numTXsInProposal)
	require.NoError(t, err)
	checkIDsAndTXsMatch(t, ids, txs, numTXsInProposal)
	assert.Equal(t, len(expected), len(ids))
	// FIXME: the randomizing of the mempool TX selection doesn't consider same account nonce order
	// assert.EqualValues(t, types.SortTransactionIDs(expected), types.SortTransactionIDs(ids))
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

func TestGetProjection(t *testing.T) {
	const nextNonce = uint64(1)
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	tx1 := newTx(t, nextNonce, amount, fee, signer)
	writeToDB(t, tcs.db, types.NewLayerID(10), types.BlockID{100}, tx1)
	tx2 := newTx(t, nextNonce+1, amount, fee, signer)
	require.NoError(t, tcs.AddTxToMempool(tx2, false))

	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mSVM.EXPECT().GetBalance(addr).Return(prevBalance).Times(1)
	tcs.mSVM.EXPECT().GetNonce(addr).Return(nextNonce).Times(1)
	nonce, balance, err := tcs.GetProjection(addr)
	require.NoError(t, err)
	assert.EqualValues(t, nextNonce+2, nonce)
	assert.EqualValues(t, prevBalance-2*(amount+fee), balance)
}

func TestAddTxToMempool(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	tx := newTx(t, uint64(0), amount, fee, signer)
	assert.NoError(t, tcs.AddTxToMempool(tx, false))
	got, err := tcs.GetMeshTransaction(tx.ID())
	require.NoError(t, err)
	assert.Equal(t, *tx, got.Transaction)
	assert.Equal(t, types.MEMPOOL, got.State)
}

func TestAddTxToMempool_CheckValidity(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mSVM.EXPECT().GetBalance(addr).Return(prevBalance).Times(1)
	tcs.mSVM.EXPECT().GetNonce(addr).Return(uint64(0)).Times(1)
	tx := newTx(t, uint64(0), amount, fee, signer)
	assert.NoError(t, tcs.AddTxToMempool(tx, true))
	got, err := tcs.GetMeshTransaction(tx.ID())
	require.NoError(t, err)
	assert.Equal(t, *tx, got.Transaction)
	assert.Equal(t, types.MEMPOOL, got.State)
}

func TestAddTxToMempool_InsufficientBalance(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mSVM.EXPECT().GetBalance(addr).Return(amount).Times(1)
	tcs.mSVM.EXPECT().GetNonce(addr).Return(uint64(0)).Times(1)
	tx := newTx(t, uint64(0), amount, fee, signer)
	err := tcs.AddTxToMempool(tx, true)
	assert.ErrorIs(t, err, errInsufficientBalance)
}

func addBatchToMemPool(t *testing.T, cs *ConservativeState, numTXs int) ([]types.TransactionID, []*types.Transaction) {
	t.Helper()
	ids := make([]types.TransactionID, 0, numTXs)
	txs := make([]*types.Transaction, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		tx := newTx(t, 1, amount, fee, signer)
		require.NoError(t, cs.AddTxToMempool(tx, false))
		ids = append(ids, tx.ID())
		txs = append(txs, tx)
	}
	return ids, txs
}

func addBatchToDB(t *testing.T, db *sql.Database, lid types.LayerID, bid types.BlockID, numTXs int) ([]types.TransactionID, []*types.Transaction) {
	t.Helper()
	ids := make([]types.TransactionID, 0, numTXs)
	txs := make([]*types.Transaction, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		tx := newTx(t, 1, amount, fee, signer)
		writeToDB(t, db, lid, bid, tx)
		ids = append(ids, tx.ID())
		txs = append(txs, tx)
	}
	return ids, txs
}

func TestStoreTransactionsFromMemPool(t *testing.T) {
	tcs := createConservativeState(t)
	ids, _ := addBatchToMemPool(t, tcs.ConservativeState, 10)
	for _, id := range ids {
		mtx, err := tcs.GetMeshTransaction(id)
		require.NoError(t, err)
		assert.Equal(t, types.MEMPOOL, mtx.State)
	}
	lid := types.NewLayerID(10)
	bid := types.RandomBlockID()
	assert.NoError(t, tcs.StoreTransactionsFromMemPool(lid, bid, ids))
	for _, id := range ids {
		mtx, err := tcs.GetMeshTransaction(id)
		require.NoError(t, err)
		assert.Equal(t, types.PENDING, mtx.State)
		assert.Equal(t, lid, mtx.LayerID)
		assert.Equal(t, bid, mtx.BlockID)
	}
}

func TestReinsertTxsToMemPool(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.NewLayerID(10)
	bid := types.RandomBlockID()
	ids, _ := addBatchToDB(t, tcs.db, lid, bid, 10)
	for _, id := range ids {
		mtx, err := tcs.GetMeshTransaction(id)
		require.NoError(t, err)
		assert.Equal(t, types.PENDING, mtx.State)
		assert.Equal(t, lid, mtx.LayerID)
		assert.Equal(t, bid, mtx.BlockID)
	}
	assert.NoError(t, tcs.ReinsertTxsToMemPool(ids))
	for _, id := range ids {
		mtx, err := tcs.GetMeshTransaction(id)
		require.NoError(t, err)
		assert.Equal(t, types.MEMPOOL, mtx.State)
	}
}

func TestGetMeshTransaction(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	tx := newTx(t, uint64(0), amount, fee, signer)
	require.NoError(t, tcs.AddTxToMempool(tx, false))
	mtx, err := tcs.GetMeshTransaction(tx.ID())
	require.NoError(t, err)
	assert.Equal(t, types.MEMPOOL, mtx.State)
	lid := types.NewLayerID(10)
	bid := types.RandomBlockID()

	tcs.pool.removeFromMemPool(tx.ID())
	writeToDB(t, tcs.db, lid, bid, tx)
	mtx, err = tcs.GetMeshTransaction(tx.ID())
	require.NoError(t, err)
	assert.Equal(t, types.PENDING, mtx.State)

	tcs.pool.markApplied(tx.ID())
	mtx, err = tcs.GetMeshTransaction(tx.ID())
	require.NoError(t, err)
	assert.Equal(t, types.APPLIED, mtx.State)

	tcs.pool.markDeleted(tx.ID())
	mtx, err = tcs.GetMeshTransaction(tx.ID())
	require.NoError(t, err)
	assert.Equal(t, types.DELETED, mtx.State)
}

func TestGetTransactions(t *testing.T) {
	const numTX = 10
	tcs := createConservativeState(t)
	lid := types.NewLayerID(10)
	bid := types.RandomBlockID()
	dbTXs, _ := addBatchToDB(t, tcs.db, lid, bid, numTX)
	memTXs, _ := addBatchToMemPool(t, tcs.ConservativeState, numTX)
	ids := append(dbTXs, memTXs...)
	badIDs := []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()}
	allIDs := append(ids, badIDs...)

	txs, missing := tcs.GetTransactions(allIDs)
	checkIDsAndTXsMatch(t, ids, txs, 2*numTX)
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

	memtx1 := newTxWthRecipient(t, addr2, uint64(0), amount, fee, signer1)
	require.NoError(t, tcs.AddTxToMempool(memtx1, false))
	dbtx1 := newTxWthRecipient(t, addr3, uint64(1), amount, fee, signer1)
	writeToDB(t, tcs.db, types.NewLayerID(5), types.BlockID{11}, dbtx1)

	memtx2 := newTxWthRecipient(t, addr3, uint64(0), amount, fee, signer2)
	require.NoError(t, tcs.AddTxToMempool(memtx2, false))
	dbtx2 := newTxWthRecipient(t, addr3, uint64(1), amount, fee, signer2)
	writeToDB(t, tcs.db, types.NewLayerID(6), types.BlockID{12}, dbtx2)

	_, err = tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(4), addr1)
	require.NoError(t, err)
	// FIXME: this should not fail
	// require.Empty(t, mtxs)

	mtxs, err = tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(5), addr1)
	require.NoError(t, err)
	require.Len(t, mtxs, 1)
	assert.Equal(t, mtxs[0].Transaction, *dbtx1)

	mtxs, err = tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(6), addr2)
	require.NoError(t, err)
	require.Len(t, mtxs, 1)
	assert.Equal(t, mtxs[0].Transaction, *dbtx2)

	mtxs, err = tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(6), addr3)
	require.NoError(t, err)
	require.Len(t, mtxs, 2)
	assert.Equal(t, mtxs[0].Transaction, *dbtx1)
	assert.Equal(t, mtxs[1].Transaction, *dbtx2)
}

func TestApplyLayer(t *testing.T) {
	const numTX = 10
	tcs := createConservativeState(t)
	lid := types.NewLayerID(10)
	ids, _ := addBatchToDB(t, tcs.db, lid, types.EmptyBlockID, numTX)
	rewards := map[types.Address]uint64{types.GenerateAddress(types.RandomBytes(20)): 100}
	tcs.mSVM.EXPECT().ApplyLayer(lid, gomock.Any(), rewards).DoAndReturn(
		func(_ types.LayerID, txs []*types.Transaction, _ map[types.Address]uint64) ([]*types.Transaction, error) {
			checkIDsAndTXsMatch(t, ids, txs, numTX)
			return nil, nil
		}).Times(1)
	bid := types.RandomBlockID()
	failed, err := tcs.ApplyLayer(lid, bid, ids, rewards)
	require.NoError(t, err)
	assert.Empty(t, failed)

	for _, id := range ids {
		mtx, err := tcs.GetMeshTransaction(id)
		require.NoError(t, err)
		assert.Equal(t, types.APPLIED, mtx.State)
		assert.Equal(t, bid, mtx.BlockID)
		assert.Equal(t, lid, mtx.LayerID)
	}
}

func TestApplyLayer_Failed(t *testing.T) {
	const (
		numTX     = 10
		numFailed = 2
	)
	tcs := createConservativeState(t)
	lid := types.NewLayerID(10)
	ids, _ := addBatchToDB(t, tcs.db, lid, types.EmptyBlockID, numTX)
	rewards := map[types.Address]uint64{types.GenerateAddress(types.RandomBytes(20)): 100}
	errSVM := errors.New("svm")
	tcs.mSVM.EXPECT().ApplyLayer(lid, gomock.Any(), rewards).DoAndReturn(
		func(_ types.LayerID, txs []*types.Transaction, _ map[types.Address]uint64) ([]*types.Transaction, error) {
			checkIDsAndTXsMatch(t, ids, txs, numTX)
			return txs[:numFailed], errSVM
		}).Times(1)
	bid := types.RandomBlockID()
	failed, err := tcs.ApplyLayer(lid, bid, ids, rewards)
	assert.ErrorIs(t, err, errSVM)
	assert.Len(t, failed, numFailed)

	for _, id := range ids {
		mtx, err := tcs.GetMeshTransaction(id)
		require.NoError(t, err)
		assert.Equal(t, types.PENDING, mtx.State)
		assert.Equal(t, types.EmptyBlockID, mtx.BlockID)
		assert.Equal(t, lid, mtx.LayerID)
	}
}

func TestTXetcherIncludesMemPool(t *testing.T) {
	tcs := createConservativeState(t)
	const numTX = 10

	dbids, dbTXs := addBatchToDB(t, tcs.db, types.NewLayerID(10), types.EmptyBlockID, numTX)
	memids, memTXs := addBatchToMemPool(t, tcs.ConservativeState, numTX)
	ids := append(dbids, memids...)
	txs := append(dbTXs, memTXs...)

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
