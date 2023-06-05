package txs

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	numTXsInProposal = 17
	defaultGas       = uint64(100)
	defaultBalance   = uint64(10000000)
	defaultAmount    = uint64(100)
	defaultFee       = uint64(4)
	numTXs           = 29
	nonce            = uint64(1234)
)

func newTx(tb testing.TB, nonce uint64, amount, fee uint64, signer *signing.EdSigner) *types.Transaction {
	tb.Helper()
	var dest types.Address
	_, err := rand.Read(dest[:])
	require.NoError(tb, err)
	return newTxWthRecipient(tb, dest, nonce, amount, fee, signer)
}

func newTxWthRecipient(tb testing.TB, dest types.Address, nonce uint64, amount, fee uint64, signer *signing.EdSigner) *types.Transaction {
	tb.Helper()
	raw := wallet.Spend(signer.PrivateKey(), dest, amount,
		nonce,
		sdk.WithGasPrice(fee),
	)
	tx := types.Transaction{
		RawTx:    types.NewRawTx(raw),
		TxHeader: &types.TxHeader{},
	}
	tx.MaxGas = defaultGas
	tx.MaxSpend = amount
	tx.GasPrice = fee
	tx.Nonce = nonce
	tx.Principal = types.GenerateAddress(signer.PublicKey().Bytes())
	return &tx
}

type testConState struct {
	*ConservativeState
	logger log.Log
	db     *sql.Database
	mvm    *MockvmState

	id peer.ID
}

func (t *testConState) handler() *TxHandler {
	return NewTxHandler(t, t.id, t.logger)
}

func createTestState(t *testing.T, gasLimit uint64) *testConState {
	ctrl := gomock.NewController(t)
	mvm := NewMockvmState(ctrl)
	db := sql.InMemory()
	cfg := CSConfig{
		BlockGasLimit:     gasLimit,
		NumTXsPerProposal: numTXsInProposal,
	}
	logger := logtest.New(t)
	_, pub, err := crypto.GenerateEd25519Key(nil)
	require.NoError(t, err)
	id, err := peer.IDFromPublicKey(pub)
	require.NoError(t, err)

	return &testConState{
		ConservativeState: NewConservativeState(mvm, db,
			WithCSConfig(cfg),
			WithLogger(logger),
		),
		logger: logger,
		db:     db,
		mvm:    mvm,
		id:     id,
	}
}

func createConservativeState(t *testing.T) *testConState {
	return createTestState(t, math.MaxUint64)
}

func addBatch(tb testing.TB, tcs *testConState, numTXs int) ([]types.TransactionID, []*types.Transaction) {
	tb.Helper()
	ids := make([]types.TransactionID, 0, numTXs)
	txs := make([]*types.Transaction, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(tb, err)
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mvm.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
		tx := newTx(tb, nonce+5, defaultAmount, defaultFee, signer)
		require.NoError(tb, tcs.AddToCache(context.Background(), tx))
		ids = append(ids, tx.ID)
		txs = append(txs, tx)
	}
	return ids, txs
}

func TestSelectProposalTXs(t *testing.T) {
	tcs := createConservativeState(t)
	numTXs := 2 * numTXsInProposal
	lid := types.LayerID(97)
	bid := types.BlockID{100}
	for i := 0; i < numTXs; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mvm.EXPECT().GetNonce(addr).Return(uint64(1), nil).Times(1)
		tx1 := newTx(t, 4, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.Background(), tx1))
		// all the TXs with nonce 1 are pending in database
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx1.ID}))
		tx2 := newTx(t, 6, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.Background(), tx2))
	}

	got := tcs.SelectProposalTXs(lid, 1)
	require.Len(t, got, numTXsInProposal)

	// the second call should have different result than the first, as the transactions should be
	// randomly selected
	got2 := tcs.SelectProposalTXs(lid, 1)
	require.Len(t, got2, numTXsInProposal)
	require.NotSubset(t, got, got2)
	require.NotSubset(t, got2, got)
}

func TestSelectProposalTXs_ExhaustGas(t *testing.T) {
	numTXs := 2 * numTXsInProposal
	lid := types.LayerID(97)
	bid := types.BlockID{100}
	expSize := numTXsInProposal / 2
	gasLimit := defaultGas * uint64(expSize)
	tcs := createTestState(t, gasLimit)
	for i := 0; i < numTXs; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mvm.EXPECT().GetNonce(addr).Return(uint64(0), nil).Times(1)
		tx1 := newTx(t, 0, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.Background(), tx1))
		// all the TXs with nonce 0 are pending in database
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx1.ID}))
		tx2 := newTx(t, 1, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.Background(), tx2))
	}
	got := tcs.SelectProposalTXs(lid, 1)
	require.Len(t, got, expSize)
}

func TestSelectProposalTXs_ExhaustMemPool(t *testing.T) {
	if util.IsWindows() {
		t.Skip("Skipping test in Windows (https://github.com/spacemeshos/go-spacemesh/issues/3624)")
	}
	tcs := createConservativeState(t)
	numTXs := numTXsInProposal - 1
	lid := types.LayerID(97)
	bid := types.BlockID{100}
	expected := make([]types.TransactionID, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		addr := types.GenerateAddress(signer.PublicKey().Bytes())
		tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
		tcs.mvm.EXPECT().GetNonce(addr).Return(uint64(0), nil).Times(1)
		tx1 := newTx(t, 0, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.Background(), tx1))
		// all the TXs with nonce 0 are pending in database
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx1.ID}))
		tx2 := newTx(t, 1, defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.Background(), tx2))
		expected = append(expected, tx2.ID)
	}
	got := tcs.SelectProposalTXs(lid, 1)
	// no reshuffling happens when mempool is exhausted. two list should be exactly the same
	require.Equal(t, expected, got)

	// the second call should have the same result since both exhaust the mempool
	got2 := tcs.SelectProposalTXs(lid, 1)
	require.Equal(t, got, got2)
}

func TestSelectProposalTXs_SamePrincipal(t *testing.T) {
	tcs := createConservativeState(t)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	numTXs := numTXsInProposal * 2
	numInBlock := numTXsInProposal
	lid := types.LayerID(97)
	bid := types.BlockID{100}
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(uint64(0), nil).Times(1)
	for i := 0; i < numInBlock; i++ {
		tx := newTx(t, uint64(i), defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.Background(), tx))
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID}))
	}
	expected := make([]types.TransactionID, 0, numTXsInProposal)
	for i := 0; i < numTXs; i++ {
		tx := newTx(t, uint64(numInBlock+i), defaultAmount, defaultFee, signer)
		require.NoError(t, tcs.AddToCache(context.Background(), tx))
		if i < numTXsInProposal {
			expected = append(expected, tx.ID)
		}
	}
	got := tcs.SelectProposalTXs(lid, 1)
	require.Equal(t, expected, got)
}

func TestSelectProposalTXs_TwoPrincipals(t *testing.T) {
	const (
		numInProposal = 30
		numTXs        = numInProposal * 2
		numInDBs      = numInProposal
	)
	tcs := createConservativeState(t)
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	addr1 := types.GenerateAddress(signer1.PublicKey().Bytes())
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	addr2 := types.GenerateAddress(signer2.PublicKey().Bytes())
	lid := types.LayerID(97)
	bid := types.BlockID{100}
	tcs.mvm.EXPECT().GetBalance(addr1).Return(defaultBalance*100, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr1).Return(uint64(0), nil).Times(1)
	tcs.mvm.EXPECT().GetBalance(addr2).Return(defaultBalance*100, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr2).Return(uint64(0), nil).Times(1)
	allTXs := make(map[types.TransactionID]*types.Transaction)
	for i := 0; i < numInDBs; i++ {
		tx := newTx(t, uint64(i), defaultAmount, defaultFee, signer1)
		require.NoError(t, tcs.AddToCache(context.Background(), tx))
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID}))
		allTXs[tx.ID] = tx
		tx = newTx(t, uint64(i), defaultAmount, defaultFee, signer2)
		require.NoError(t, tcs.AddToCache(context.Background(), tx))
		require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID}))
		allTXs[tx.ID] = tx
	}
	for i := 0; i < numTXs; i++ {
		tx := newTx(t, uint64(numInDBs+i), defaultAmount, defaultFee, signer1)
		require.NoError(t, tcs.AddToCache(context.Background(), tx))
		allTXs[tx.ID] = tx
		tx = newTx(t, uint64(numInDBs+i), defaultAmount, defaultFee, signer2)
		require.NoError(t, tcs.AddToCache(context.Background(), tx))
		allTXs[tx.ID] = tx
	}
	got := tcs.SelectProposalTXs(lid, 1)
	// the odds of picking just one principal is 2^30
	chosen := make(map[types.Address][]*types.Transaction)
	for _, tid := range got {
		tx := allTXs[tid]
		chosen[tx.Principal] = append(chosen[tx.Principal], tx)
	}
	require.Len(t, chosen, 2)
	require.Contains(t, chosen, addr1)
	require.Contains(t, chosen, addr2)
	// making sure nonce values are in order
	for i, tx := range chosen[addr1] {
		require.Equal(t, uint64(i+numInDBs), tx.Nonce)
	}
	for i, tx := range chosen[addr2] {
		require.Equal(t, uint64(i+numInDBs), tx.Nonce)
	}
}

func TestGetProjection(t *testing.T) {
	tcs := createConservativeState(t)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx1 := newTx(t, nonce, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(context.Background(), tx1))
	require.NoError(t, tcs.LinkTXsWithBlock(types.LayerID(10), types.BlockID{100}, []types.TransactionID{tx1.ID}))
	tx2 := newTx(t, nonce+1, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(context.Background(), tx2))

	got, balance := tcs.GetProjection(addr)
	require.EqualValues(t, nonce+2, got)
	require.EqualValues(t, defaultBalance-2*(defaultAmount+defaultFee*defaultGas), balance)
}

func TestAddToCache(t *testing.T) {
	tcs := createConservativeState(t)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(context.Background(), tx))
	has := tcs.cache.Has(tx.ID)
	require.True(t, has)
	got, err := transactions.Get(tcs.db, tx.ID)
	require.NoError(t, err)
	require.Equal(t, *tx, got.Transaction)
}

func TestAddToCache_BadNonceNotPersisted(t *testing.T) {
	tcs := createConservativeState(t)
	tx := &types.Transaction{
		RawTx: types.NewRawTx([]byte{1, 1, 1}),
		TxHeader: &types.TxHeader{
			Principal: types.Address{1, 1},
			Nonce:     100,
		},
	}
	tcs.mvm.EXPECT().GetBalance(tx.Principal).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(tx.Principal).Return(tx.Nonce+1, nil).Times(1)
	require.ErrorIs(t, tcs.AddToCache(context.Background(), tx), errBadNonce)
	checkTXNotInDB(t, tcs.db, tx.ID)
}

func TestAddToCache_NonceGap(t *testing.T) {
	tcs := createConservativeState(t)
	tx := &types.Transaction{
		RawTx: types.NewRawTx([]byte{1, 1, 1}),
		TxHeader: &types.TxHeader{
			Principal: types.Address{1, 1},
			Nonce:     100,
		},
	}
	tcs.mvm.EXPECT().GetBalance(tx.Principal).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(tx.Principal).Return(tx.Nonce-2, nil).Times(1)
	require.NoError(t, tcs.AddToCache(context.Background(), tx))
	require.True(t, tcs.cache.Has(tx.ID))
	require.False(t, tcs.cache.MoreInDB(tx.Principal))
	checkTXStateFromDB(t, tcs.db, []*types.MeshTransaction{{Transaction: *tx}}, types.MEMPOOL)
}

func TestAddToCache_InsufficientBalance(t *testing.T) {
	tcs := createConservativeState(t)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultAmount, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(context.Background(), tx))
	checkNoTX(t, tcs.cache, tx.ID)
	require.True(t, tcs.cache.MoreInDB(addr))
	checkTXStateFromDB(t, tcs.db, []*types.MeshTransaction{{Transaction: *tx}}, types.MEMPOOL)
}

func TestAddToCache_TooManyForOneAccount(t *testing.T) {
	tcs := createConservativeState(t)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(uint64(math.MaxUint64), nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	mtxs := make([]*types.MeshTransaction, 0, maxTXsPerAcct+1)
	for i := 0; i <= maxTXsPerAcct; i++ {
		tx := newTx(t, nonce+uint64(i), defaultAmount, defaultFee, signer)
		mtxs = append(mtxs, &types.MeshTransaction{Transaction: *tx})
		require.NoError(t, tcs.AddToCache(context.Background(), tx))
	}
	require.True(t, tcs.cache.MoreInDB(addr))
	checkTXStateFromDB(t, tcs.db, mtxs, types.MEMPOOL)
}

func TestGetMeshTransaction(t *testing.T) {
	tcs := createConservativeState(t)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
	require.NoError(t, tcs.AddToCache(context.Background(), tx))
	mtx, err := tcs.GetMeshTransaction(tx.ID)
	require.NoError(t, err)
	require.Equal(t, types.MEMPOOL, mtx.State)

	lid := types.LayerID(10)
	pid := types.ProposalID{1, 2, 3}
	require.NoError(t, tcs.LinkTXsWithProposal(lid, pid, []types.TransactionID{tx.ID}))
	mtx, err = tcs.GetMeshTransaction(tx.ID)
	require.NoError(t, err)
	require.Equal(t, types.MEMPOOL, mtx.State)

	bid := types.BlockID{2, 3, 4}
	require.NoError(t, tcs.LinkTXsWithBlock(lid, bid, []types.TransactionID{tx.ID}))
	mtx, err = tcs.GetMeshTransaction(tx.ID)
	require.NoError(t, err)
	require.Equal(t, types.MEMPOOL, mtx.State)
}

func TestUpdateCache_UpdateHeader(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.LayerID(1)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	addr := types.GenerateAddress(signer.PublicKey().Bytes())
	tcs.mvm.EXPECT().GetBalance(addr).Return(defaultBalance, nil).Times(1)
	tcs.mvm.EXPECT().GetNonce(addr).Return(nonce, nil).Times(1)
	tx := newTx(t, nonce, defaultAmount, defaultFee, signer)
	hdrless := *tx
	hdrless.TxHeader = nil

	// the hdrless tx is saved via syncing from blocks
	require.NoError(t, transactions.Add(tcs.db, &hdrless, time.Now()))
	got, err := transactions.Get(tcs.db, tx.ID)
	require.NoError(t, err)
	require.Nil(t, got.TxHeader)

	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{
			LayerIndex: lid,
			TxIDs:      []types.TransactionID{tx.ID},
		})
	executed := []types.TransactionWithResult{
		{
			Transaction: *tx,
			TransactionResult: types.TransactionResult{
				Layer: lid,
				Block: block.ID(),
			},
		},
	}
	tcs.mvm.EXPECT().GetBalance(tx.Principal).Return(defaultBalance-(defaultAmount+defaultFee), nil)
	tcs.mvm.EXPECT().GetNonce(tx.Principal).Return(nonce+1, nil)
	require.NoError(t, tcs.UpdateCache(context.Background(), lid, block.ID(), executed, nil))

	got, err = transactions.Get(tcs.db, tx.ID)
	require.NoError(t, err)
	require.NotNil(t, got.TxHeader)
}

func TestUpdateCache(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.LayerID(1)
	ids, txs := addBatch(t, tcs, numTXs)
	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{
			LayerIndex: lid,
			TxIDs:      ids,
		})
	executed := make([]types.TransactionWithResult, 0, numTXs)
	for _, tx := range txs {
		executed = append(executed, types.TransactionWithResult{
			Transaction: *tx,
			TransactionResult: types.TransactionResult{
				Layer: lid,
				Block: block.ID(),
			},
		})
		tcs.mvm.EXPECT().GetBalance(tx.Principal).Return(defaultBalance-(defaultAmount+defaultFee), nil)
		tcs.mvm.EXPECT().GetNonce(tx.Principal).Return(nonce+1, nil)
	}
	require.NoError(t, tcs.UpdateCache(context.Background(), lid, block.ID(), executed, nil))

	for _, id := range ids {
		mtx, err := transactions.Get(tcs.db, id)
		require.NoError(t, err)
		require.Equal(t, types.APPLIED, mtx.State)
		require.Equal(t, lid, mtx.LayerID)
		require.Equal(t, block.ID(), mtx.BlockID)
	}
}

func TestUpdateCache_EmptyLayer(t *testing.T) {
	tcs := createConservativeState(t)
	lid := types.LayerID(1)
	ids, _ := addBatch(t, tcs, numTXs)
	require.NoError(t, tcs.LinkTXsWithBlock(lid, types.BlockID{1, 2, 3}, ids))
	mempoolTxs := tcs.SelectProposalTXs(lid, 2)
	require.Empty(t, mempoolTxs)

	require.NoError(t, tcs.UpdateCache(context.Background(), lid, types.EmptyBlockID, nil, nil))
	mempoolTxs = tcs.SelectProposalTXs(lid, 2)
	require.Len(t, mempoolTxs, len(ids))
}

func TestConsistentHandling(t *testing.T) {
	// there are two different workflows for transactions
	// 1. receive gossiped transaction and verify it immediately
	// 2. receive synced transaction and delay verification
	// this test is meant to ensure that both of them will result in a consistent
	// conservative cache state

	instances := []*testConState{
		createConservativeState(t),
		createConservativeState(t),
	}

	rng := rand.New(rand.NewSource(101))
	signers := make([]*signing.EdSigner, 30)
	nonces := make([]uint64, len(signers))
	for i := range signers {
		signer, err := signing.NewEdSigner(
			signing.WithKeyFromRand(rng),
		)
		require.NoError(t, err)
		signers[i] = signer
	}
	for _, instance := range instances {
		instance.mvm.EXPECT().GetBalance(gomock.Any()).Return(defaultBalance, nil).AnyTimes()
		instance.mvm.EXPECT().GetNonce(gomock.Any()).Return(uint64(0), nil).AnyTimes()
	}
	for lid := 1; lid < 10; lid++ {
		txs := make([]*types.Transaction, 100)
		ids := make([]types.TransactionID, len(txs))
		raw := make([]types.Transaction, len(txs))
		verified := make([]types.Transaction, len(txs))
		for i := range txs {
			signer := rng.Intn(len(signers))
			txs[i] = newTx(t, nonces[signer], 1, 1, signers[signer])
			nonces[signer]++
			noheader := *(txs[i])
			noheader.TxHeader = nil
			ids[i] = txs[i].ID
			raw[i] = noheader
			verified[i] = *txs[i]

			req := smocks.NewMockValidationRequest(gomock.NewController(t))
			req.EXPECT().Parse().Times(1).Return(txs[i].TxHeader, nil)
			req.EXPECT().Verify().Times(1).Return(true)
			instances[0].mvm.EXPECT().Validation(txs[i].RawTx).Times(1).Return(req)

			failed := smocks.NewMockValidationRequest(gomock.NewController(t))
			failed.EXPECT().Parse().Times(1).Return(nil, errors.New("test"))
			instances[1].mvm.EXPECT().Validation(txs[i].RawTx).Times(1).Return(failed)

			require.Equal(t, nil, instances[0].handler().HandleGossipTransaction(context.Background(), p2p.NoPeer, txs[i].Raw))
			require.NoError(t, instances[1].handler().HandleBlockTransaction(context.Background(), p2p.NoPeer, txs[i].Raw))
		}
		block := types.NewExistingBlock(types.BlockID{byte(lid)},
			types.InnerBlock{
				LayerIndex: types.LayerID(uint32(lid)),
				TxIDs:      ids,
			},
		)
		results := make([]types.TransactionWithResult, len(txs))
		for i, tx := range txs {
			results[i] = types.TransactionWithResult{
				Transaction: *tx,
				TransactionResult: types.TransactionResult{
					Layer: block.LayerIndex,
					Block: block.ID(),
				},
			}
		}
		for _, instance := range instances {
			require.NoError(t, instance.UpdateCache(context.Background(), block.LayerIndex, block.ID(), results, nil))
			require.NoError(t, layers.SetApplied(instance.db, block.LayerIndex, block.ID()))
		}
	}
}
