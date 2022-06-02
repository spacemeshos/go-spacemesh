package txs

import (
	"errors"
	"math"
	"math/rand"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/txs/mocks"
	"github.com/spacemeshos/go-spacemesh/vm/transaction"
)

const (
	numTXsInProposal = 17
	defaultGas       = uint64(100)
	defaultBalance   = uint64(1000)
	defaultAmount    = uint64(10)
	defaultFee       = uint64(5)
)

func newTx(t *testing.T, nonce uint64, amount, fee uint64, signer *signing.EdSigner) *types.Transaction {
	dest := types.Address{byte(rand.Int()), byte(rand.Int()), byte(rand.Int()), byte(rand.Int())}
	return newTxWthRecipient(t, dest, nonce, amount, fee, signer)
}

func newTxWthRecipient(t *testing.T, dest types.Address, nonce uint64, amount, fee uint64, signer *signing.EdSigner) *types.Transaction {
	tx, err := transaction.GenerateCallTransaction(signer, dest, nonce, amount, defaultGas, fee)
	require.NoError(t, err)
	return tx
}

type testConState struct {
	*ConservativeState
	db  *sql.Database
	mvm *mocks.MockvmState
}

func createTestState(t *testing.T, gasLimit uint64) *testConState {
	ctrl := gomock.NewController(t)
	mvm := mocks.NewMockvmState(ctrl)
	db := sql.InMemory()
	cfg := CSConfig{
		BlockGasLimit:      gasLimit,
		NumTXsPerProposal:  numTXsInProposal,
		OptFilterThreshold: 90,
	}

	return &testConState{
		ConservativeState: NewConservativeState(mvm, db,
			WithCSConfig(cfg),
			WithLogger(logtest.New(t))),
		db:  db,
		mvm: mvm,
	}
}

func createConservativeState(t *testing.T) *testConState {
	return createTestState(t, math.MaxUint64)
}

func addBatch(t *testing.T, tcs *testConState, numTXs int) ([]types.TransactionID, []*types.Transaction) {
	t.Helper()
	ids := make([]types.TransactionID, 0, numTXs)
	txs := make([]*types.Transaction, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		addr := types.BytesToAddress(signer.PublicKey().Bytes())
		tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mvm.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
		tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(tx, true))
		ids = append(ids, tx.ID())
		txs = append(txs, tx)
	}
	return ids, txs
}

func createProposalsDupTXs(lid types.LayerID, hash types.Hash32, step, numPer, numProposals int, tids []types.TransactionID) []*types.Proposal {
	total := len(tids)
	proposals := make([]*types.Proposal, 0, numProposals)
	for offset := 0; offset < total; offset += step {
		end := offset + numPer
		if end >= total {
			end = total
		}
		p := types.GenLayerProposal(lid, tids[offset:end])
		p.MeshHash = hash
		proposals = append(proposals, p)
	}
	return proposals
}

func checkIsStable(t *testing.T, expected []types.TransactionID, proposals []*types.Proposal, cs *ConservativeState) {
	t.Helper()
	lid := proposals[0].LayerIndex
	numProposals := len(proposals)
	// reverse the order of proposals. we should still have exactly the same ordered transactions
	newProposals := make([]*types.Proposal, 0, numProposals)
	for i := numProposals - 1; i >= 0; i-- {
		newProposals = append(newProposals, proposals[i])
	}
	got, err := cs.SelectBlockTXs(lid, newProposals)
	require.NoError(t, err)
	require.Equal(t, expected, got)
}

func TestSelectBlockTXs(t *testing.T) {
	tcs := createConservativeState(t)
	totalNumTXs := 100
	tids, txs := addBatch(t, tcs, totalNumTXs)
	// optimistic filtering will query the real state again
	for _, tx := range txs {
		principal := tx.Origin()
		tcs.mvm.EXPECT().GetBalance(principal).Return(defaultBalance, nil).AnyTimes()
		tcs.mvm.EXPECT().GetNonce(principal).Return(nonce, nil).AnyTimes()
	}
	// make sure proposals have overlapping transactions
	numProposals := 10
	numPerProposal := 30
	step := totalNumTXs / numProposals
	meshHash := types.RandomHash()
	lid := types.NewLayerID(1333)
	require.NoError(t, layers.SetAggregatedHash(tcs.db, lid.Sub(1), meshHash))
	proposals := createProposalsDupTXs(lid, meshHash, step, numPerProposal, numProposals, tids)
	got, err := tcs.SelectBlockTXs(lid, proposals)
	require.NoError(t, err)
	require.Len(t, got, totalNumTXs)

	// reverse the order of proposals. we should still have exactly the same ordered transactions
	checkIsStable(t, got, proposals, tcs.ConservativeState)
}

func TestSelectBlockTXs_PrunedByGasLimit(t *testing.T) {
	totalNumTXs := 100
	expSize := totalNumTXs / 3
	gasLimit := uint64(expSize) * defaultGas
	tcs := createTestState(t, gasLimit)
	tids, txs := addBatch(t, tcs, totalNumTXs)
	// optimistic filtering will query the real state again
	for _, tx := range txs {
		principal := tx.Origin()
		tcs.mvm.EXPECT().GetBalance(principal).Return(defaultBalance, nil).AnyTimes()
		tcs.mvm.EXPECT().GetNonce(principal).Return(nonce, nil).AnyTimes()
	}
	// make sure proposals have overlapping transactions
	numProposals := 10
	numPerProposal := 30
	step := totalNumTXs / numProposals
	meshHash := types.RandomHash()
	lid := types.NewLayerID(1333)
	require.NoError(t, layers.SetAggregatedHash(tcs.db, lid.Sub(1), meshHash))
	proposals := createProposalsDupTXs(lid, meshHash, step, numPerProposal, numProposals, tids)
	got, err := tcs.SelectBlockTXs(lid, proposals)
	require.NoError(t, err)
	require.Len(t, got, expSize)

	// reverse the order of proposals. we should still have exactly the same ordered transactions
	checkIsStable(t, got, proposals, tcs.ConservativeState)
}

func TestSelectBlockTXs_NoOptimisticFiltering(t *testing.T) {
	tcs := createConservativeState(t)
	totalNumTXs := 100
	tids, _ := addBatch(t, tcs, totalNumTXs)
	// make sure proposals have overlapping transactions
	numProposals := 10
	numPerProposal := 30
	step := totalNumTXs / numProposals
	meshHash := types.RandomHash()
	lid := types.NewLayerID(1333)
	require.NoError(t, layers.SetAggregatedHash(tcs.db, lid.Sub(1), meshHash))
	proposals := createProposalsDupTXs(lid, meshHash, step, numPerProposal, numProposals, tids)
	// change proposal's mesh hash
	for _, p := range proposals {
		p.MeshHash = types.RandomHash()
	}
	got, err := tcs.SelectBlockTXs(lid, proposals)
	require.NoError(t, err)
	require.Len(t, got, totalNumTXs)

	// reverse the order of proposals. we should still have exactly the same ordered transactions
	checkIsStable(t, got, proposals, tcs.ConservativeState)
}

func TestSelectBlockTXs_NodeHasDifferentMesh(t *testing.T) {
	tcs := createConservativeState(t)
	numProposals := 10
	meshHash := types.RandomHash()
	lid := types.NewLayerID(1333)
	proposals := make([]*types.Proposal, 0, numProposals)
	for i := 0; i < numProposals; i++ {
		p := types.GenLayerProposal(lid, nil)
		p.MeshHash = meshHash
		proposals = append(proposals, p)
	}
	require.NoError(t, layers.SetAggregatedHash(tcs.db, lid.Sub(1), types.RandomHash()))
	got, err := tcs.SelectBlockTXs(lid, proposals)
	require.ErrorIs(t, err, errNodeHasBadMeshHash)
	require.Nil(t, got)
}

func TestSelectProposalTXs(t *testing.T) {
	tcs := createConservativeState(t)
	numTXs := 2 * numTXsInProposal
	lid := types.NewLayerID(97)
	bid := types.BlockID{100}
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mvm.EXPECT().GetNonce(addr).Return(uint64(0), nil).Times(1)
		tx1 := newTx(t, 0, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(tx1, true))
		// all the TXs with nonce 0 are pending in database
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx1.ID()}))
		tx2 := newTx(t, 1, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(tx2, true))
	}

	got := tcs.SelectProposalTXs(1)
	require.Len(t, got, numTXsInProposal)

	// the second call should have different result than the first, as the transactions should be
	// randomly selected
	got2 := tcs.SelectProposalTXs(1)
	require.Len(t, got2, numTXsInProposal)
	require.NotSubset(t, got, got2)
	require.NotSubset(t, got2, got)
}

func TestSelectProposalTXs_ExhaustGas(t *testing.T) {
	numTXs := 2 * numTXsInProposal
	lid := types.NewLayerID(97)
	bid := types.BlockID{100}
	expSize := numTXsInProposal / 2
	gasLimit := defaultGas * uint64(expSize)
	tcs := createTestState(t, gasLimit)
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mvm.EXPECT().GetNonce(addr).Return(uint64(0), nil).Times(1)
		tx1 := newTx(t, 0, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(tx1, true))
		// all the TXs with nonce 0 are pending in database
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx1.ID()}))
		tx2 := newTx(t, 1, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(tx2, true))
	}
	got := tcs.SelectProposalTXs(1)
	require.Len(t, got, expSize)
}

func TestSelectProposalTXs_ExhaustMemPool(t *testing.T) {
	tcs := createConservativeState(t)
	numTXs := numTXsInProposal - 1
	lid := types.NewLayerID(97)
	bid := types.BlockID{100}
	expected := make([]types.TransactionID, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		signer := signing.NewEdSigner()
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mvm.EXPECT().GetNonce(addr).Return(uint64(0), nil).Times(1)
		tx1 := newTx(t, 0, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(tx1, true))
		// all the TXs with nonce 0 are pending in database
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx1.ID()}))
		tx2 := newTx(t, 1, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(tx2, true))
		expected = append(expected, tx2.ID())
	}
	got := tcs.SelectProposalTXs(1)
	// no reshuffling happens when mempool is exhausted. two list should be exactly the same
	require.Equal(t, expected, got)

	// the second call should have the same result since both exhaust the mempool
	got2 := tcs.SelectProposalTXs(1)
	require.Equal(t, got, got2)
}

func TestSelectProposalTXs_SamePrincipal(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	numTXs := numTXsInProposal * 2
	numInBlock := numTXsInProposal
	lid := types.NewLayerID(97)
	bid := types.BlockID{100}
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(uint64(0), nil).Times(1)
	for i := 0; i < numInBlock; i++ {
		tx := newTx(t, uint64(i), defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(tx, true))
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID()}))
	}
	expected := make([]types.TransactionID, 0, numTXsInProposal)
	for i := 0; i < numTXs; i++ {
		tx := newTx(t, uint64(numInBlock+i), defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(tx, true))
		if i < numTXsInProposal {
			expected = append(expected, tx.ID())
		}
	}
	got := tcs.SelectProposalTXs(1)
	require.Equal(t, expected, got)
}

func TestSelectProposalTXs_TwoPrincipals(t *testing.T) {
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
	tcs.mvm.EXPECT().GetBalance(addr1).Return(defaultBalance*100, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr1).Return(uint64(0), nil).Times(1)
	tcs.mvm.EXPECT().GetBalance(addr2).Return(defaultBalance*100, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr2).Return(uint64(0), nil).Times(1)
	allTXs := make(map[types.TransactionID]*types.Transaction)
	for i := 0; i < numInDBs; i++ {
		tx := newTx(t, uint64(i), defaultAmount, defaultFee, signer1)
		require.NoError(t, tcs.AddToCache(tx, true))
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID()}))
		allTXs[tx.ID()] = tx
		tx = newTx(t, uint64(i), defaultAmount, defaultFee, signer2)
		require.NoError(t, tcs.AddToCache(tx, true))
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID()}))
		allTXs[tx.ID()] = tx
	}
	for i := 0; i < numTXs; i++ {
		tx := newTx(t, uint64(numInDBs+i), defaultAmount, defaultFee, signer1)
		require.NoError(t, tcs.AddToCache(tx, true))
		allTXs[tx.ID()] = tx
		tx = newTx(t, uint64(numInDBs+i), defaultAmount, defaultFee, signer2)
		require.NoError(t, tcs.AddToCache(tx, true))
		allTXs[tx.ID()] = tx
	}
	got := tcs.SelectProposalTXs(1)
	// the odds of picking just one principal is 2^30
	chosen := make(map[types.Address][]*types.Transaction)
	for _, tid := range got {
		tx := allTXs[tid]
		chosen[tx.Origin()] = append(chosen[tx.Origin()], tx)
	}
	require.Len(t, chosen, 2)
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
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx1 := newTx(t, nonce, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(tx1, true))
	require.NoError(t, tcs.LinkTXsWithBlock(types.NewLayerID(10), types.BlockID{100}, []types.TransactionID{tx1.ID()}))
	tx2 := newTx(t, nonce+1, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(tx2, true))

	got, balance := tcs.GetProjection(addr)
	require.EqualValues(t, nonce+2, got)
	require.EqualValues(t, defaultBalance-2*(defaultAmount+defaultFee), balance)
}

func TestAddToCache(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.BytesToAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(tx, true))
	got, err := tcs.HasTx(tx.ID())
	require.NoError(t, err)
	require.True(t, got)
}

func TestAddToCache_KnownTX(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.BytesToAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(tx, false))
	got, err := tcs.HasTx(tx.ID())
	require.NoError(t, err)
	require.True(t, got)
}

func TestAddToCache_InsufficientBalance(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.BytesToAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultAmount, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
	err := tcs.AddToCache(tx, true)
	require.ErrorIs(t, err, errInsufficientBalance)
}

func TestGetMeshTransaction(t *testing.T) {
	tcs := createConservativeState(t)
	signer := signing.NewEdSigner()
	addr := types.BytesToAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(tx, true))
	mtx, err := tcs.GetMeshTransaction(tx.ID())
	require.NoError(t, err)
	require.Equal(t, types.MEMPOOL, mtx.State)

	lid := types.NewLayerID(10)
	pid := types.ProposalID{1, 2, 3}
	require.NoError(t, tcs.LinkTXsWithProposal(lid, pid, []types.TransactionID{tx.ID()}))
	mtx, err = tcs.GetMeshTransaction(tx.ID())
	require.NoError(t, err)
	require.Equal(t, types.PROPOSAL, mtx.State)

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
	require.Len(t, missing, len(badIDs))
	for _, id := range badIDs {
		require.NotNil(t, missing[id])
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

	tcs.mvm.EXPECT().GetBalance(addr1).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr1).Return(nonce, nil).Times(1)
	tx0 := newTxWthRecipient(t, addr2, nonce, defaultAmount, defaultFee, signer1)
	require.NoError(t, tcs.AddToCache(tx0, true))
	tx1 := newTxWthRecipient(t, addr3, nonce+1, defaultAmount, defaultFee, signer1)
	require.NoError(t, tcs.AddToCache(tx1, true))
	require.NoError(t, tcs.LinkTXsWithBlock(types.NewLayerID(5), types.BlockID{11}, []types.TransactionID{tx1.ID()}))

	tcs.mvm.EXPECT().GetBalance(addr2).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr2).Return(nonce, nil).Times(1)
	tx2 := newTxWthRecipient(t, addr3, nonce, defaultAmount, defaultFee, signer2)
	require.NoError(t, tcs.AddToCache(tx2, true))
	tx3 := newTxWthRecipient(t, addr3, nonce+1, defaultAmount, defaultFee, signer2)
	require.NoError(t, tcs.AddToCache(tx3, true))
	require.NoError(t, tcs.LinkTXsWithBlock(types.NewLayerID(6), types.BlockID{12}, []types.TransactionID{tx3.ID()}))

	// nothing in the range of 1-4
	mtxs, err = tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(4), addr1)
	require.NoError(t, err)
	require.Empty(t, mtxs)

	mtxs, err = tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(5), addr1)
	require.NoError(t, err)
	require.Len(t, mtxs, 1)
	require.Equal(t, mtxs[0].Transaction, *tx1)

	mtxs, err = tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(6), addr2)
	require.NoError(t, err)
	require.Len(t, mtxs, 1)
	require.Equal(t, mtxs[0].Transaction, *tx3)

	mtxs, err = tcs.GetTransactionsByAddress(types.NewLayerID(1), types.NewLayerID(6), addr3)
	require.NoError(t, err)
	require.Len(t, mtxs, 2)
	require.Equal(t, mtxs[0].Transaction, *tx1)
	require.Equal(t, mtxs[1].Transaction, *tx3)
}

func TestApplyLayer(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.NewLayerID(1)
	ids, txs := addBatch(t, tcs, numTXs)
	coinbase := types.GenerateAddress(types.RandomBytes(20))
	weight := util.WeightFromFloat64(200.56)
	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{
			LayerIndex: lid,
			Rewards: []types.AnyReward{
				{Coinbase: coinbase, Weight: types.RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()}},
			},
			TxIDs: ids,
		})
	for _, tx := range txs {
		tcs.mvm.EXPECT().GetBalance(tx.Origin()).Return(defaultBalance-(defaultAmount+defaultFee), nil)
		tcs.mvm.EXPECT().GetNonce(tx.Origin()).Return(nonce+1, nil)
	}
	tcs.mvm.EXPECT().ApplyLayer(lid, gomock.Any(), block.Rewards).DoAndReturn(
		func(_ types.LayerID, got []*types.Transaction, _ []types.AnyReward) ([]*types.Transaction, error) {
			require.ElementsMatch(t, got, txs)
			return nil, nil
		}).Times(1)
	failed, err := tcs.ApplyLayer(block)
	require.NoError(t, err)
	require.Empty(t, failed)

	for _, id := range ids {
		mtx, err := tcs.GetMeshTransaction(id)
		require.NoError(t, err)
		require.Equal(t, types.APPLIED, mtx.State)
		require.Equal(t, block.ID(), mtx.BlockID)
		require.Equal(t, lid, mtx.LayerID)
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

func TestApplyLayer_TXsFailedVM(t *testing.T) {
	const numFailed = 2
	tcs := createConservativeState(t)
	lid := types.NewLayerID(1)
	ids, txs := addBatch(t, tcs, numTXs)
	for _, tx := range txs[numFailed:] {
		principal := tx.Origin()
		tcs.mvm.EXPECT().GetBalance(principal).Return(defaultBalance-(tx.Amount+tx.Fee), nil).Times(1)
		tcs.mvm.EXPECT().GetNonce(principal).Return(nonce+1, nil).Times(1)
	}
	coinbase := types.GenerateAddress(types.RandomBytes(20))
	weight := util.WeightFromFloat64(200.56)
	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{
			LayerIndex: lid,
			Rewards: []types.AnyReward{
				{Coinbase: coinbase, Weight: types.RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()}},
			},
			TxIDs: ids,
		})
	tcs.mvm.EXPECT().ApplyLayer(lid, gomock.Any(), block.Rewards).DoAndReturn(
		func(_ types.LayerID, got []*types.Transaction, _ []types.AnyReward) ([]*types.Transaction, error) {
			require.ElementsMatch(t, got, txs)
			return got[:numFailed], nil
		}).Times(1)
	failed, err := tcs.ApplyLayer(block)
	require.NoError(t, err)
	require.Len(t, failed, numFailed)

	for i, id := range ids {
		mtx, err := tcs.GetMeshTransaction(id)
		require.NoError(t, err)
		if i < numFailed {
			require.Equal(t, types.MEMPOOL, mtx.State)
			require.Equal(t, types.EmptyBlockID, mtx.BlockID)
			require.Equal(t, types.LayerID{}, mtx.LayerID)
		} else {
			require.Equal(t, types.APPLIED, mtx.State)
			require.Equal(t, block.ID(), mtx.BlockID)
			require.Equal(t, lid, mtx.LayerID)
		}
	}
}

func TestApplyLayer_VMError(t *testing.T) {
	const numFailed = 2
	tcs := createConservativeState(t)
	lid := types.NewLayerID(1)
	ids, txs := addBatch(t, tcs, numTXs)
	coinbase := types.GenerateAddress(types.RandomBytes(20))
	weight := util.WeightFromFloat64(200.56)
	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{
			LayerIndex: lid,
			Rewards: []types.AnyReward{
				{Coinbase: coinbase, Weight: types.RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()}},
			},
			TxIDs: ids,
		})
	errVM := errors.New("vm error")
	tcs.mvm.EXPECT().ApplyLayer(lid, gomock.Any(), block.Rewards).DoAndReturn(
		func(_ types.LayerID, got []*types.Transaction, _ []types.AnyReward) ([]*types.Transaction, error) {
			require.ElementsMatch(t, got, txs)
			return got[:numFailed], errVM
		}).Times(1)
	failed, err := tcs.ApplyLayer(block)
	require.ErrorIs(t, err, errVM)
	require.Nil(t, failed)

	for _, id := range ids {
		mtx, err := tcs.GetMeshTransaction(id)
		require.NoError(t, err)
		require.Equal(t, types.MEMPOOL, mtx.State)
		require.Equal(t, types.EmptyBlockID, mtx.BlockID)
		require.Equal(t, types.LayerID{}, mtx.LayerID)
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
