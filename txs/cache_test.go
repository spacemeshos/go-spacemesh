package txs

import (
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/txs/mocks"
)

type testCache struct {
	*cache
	mockTP      *mocks.MocktxProvider
	lastApplied types.LayerID
}

type testAcct struct {
	signer         *signing.EdSigner
	principal      types.Address
	nonce, balance uint64
}

func getStateFunc(states map[types.Address]*testAcct) stateFunc {
	return func(addr types.Address) (uint64, uint64) {
		st := states[addr]
		return st.nonce, st.balance
	}
}

func createTestCache(t *testing.T, sf stateFunc) *testCache {
	ctrl := gomock.NewController(t)
	mtp := mocks.NewMocktxProvider(ctrl)
	return &testCache{
		cache:  newCache(mtp, sf, logtest.New(t)),
		mockTP: mtp,
	}
}

func generatePendingTXs(t *testing.T, signer *signing.EdSigner, from, to uint64) []*types.MeshTransaction {
	now := time.Now()
	txs := make([]*types.MeshTransaction, 0, int(to-from+1))
	for i := from; i <= to; i++ {
		txs = append(txs, &types.MeshTransaction{
			Transaction: *newTx(t, i, amount, fee, signer),
			Received:    now.Add(time.Second * time.Duration(i)),
		})
	}
	return txs
}

func checkTX(t *testing.T, c *cache, mtx *types.MeshTransaction) {
	got := c.Get(mtx.ID())
	require.NotNil(t, got)
	require.Equal(t, mtx.ID(), got.Tid)
	require.Equal(t, mtx.LayerID, got.Layer)
	require.Equal(t, mtx.BlockID, got.Block)
}

func checkNoTX(t *testing.T, c *cache, tid types.TransactionID) {
	require.Nil(t, c.Get(tid))
}

func checkMempool(t *testing.T, c *cache, expected map[types.Address][]*types.NanoTX) {
	mempool, err := c.GetMempool()
	require.NoError(t, err)
	require.Len(t, mempool, len(expected))
	for addr := range mempool {
		require.EqualValues(t, expected[addr], mempool[addr])
	}
}

func checkProjection(t *testing.T, c *cache, addr types.Address, nonce, balance uint64) {
	pNonce, pBalance := c.GetProjection(addr)
	require.Equal(t, nonce, pNonce)
	require.Equal(t, balance, pBalance)
}

func toNanoTXs(mtxs []*types.MeshTransaction) []*types.NanoTX {
	ntxs := make([]*types.NanoTX, 0, len(mtxs))
	for _, mtx := range mtxs {
		ntxs = append(ntxs, types.NewNanoTX(mtx))
	}
	return ntxs
}

func createSingleAccountTestCache(t *testing.T) (*testCache, *testAcct) {
	signer := signing.NewEdSigner()
	principal := types.GenerateAddress(signer.PublicKey().Bytes())
	ta := &testAcct{signer: signer, principal: principal, nonce: uint64(12), balance: prevBalance}
	states := map[types.Address]*testAcct{principal: ta}
	return createTestCache(t, getStateFunc(states)), ta
}

func buildSingleAccountCache(t *testing.T, tc *testCache, ta *testAcct, mtxs []*types.MeshTransaction) (uint64, uint64) {
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)

	newNextNonce := ta.nonce + uint64(len(mtxs))
	newBalance := ta.balance
	for _, mtx := range mtxs {
		newBalance -= mtx.Spending()
	}

	tc.lastApplied = types.NewLayerID(99)
	tc.mockTP.EXPECT().LastAppliedLayer().Return(tc.lastApplied, nil)
	tc.mockTP.EXPECT().GetAllPending().Return(mtxs, nil)
	require.NoError(t, tc.BuildFromScratch())
	for _, mtx := range mtxs {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	var expectedMempool map[types.Address][]*types.NanoTX
	if len(mtxs) > 0 {
		expectedMempool = map[types.Address][]*types.NanoTX{ta.principal: toNanoTXs(mtxs)}
	}
	checkMempool(t, tc.cache, expectedMempool)
	return newNextNonce, newBalance
}

func TestCache_Account_HappyFlow(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	// nothing in the cache yet
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)

	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+4)
	newNextNonce := ta.nonce + uint64(len(mtxs))
	newBalance := ta.balance
	for _, mtx := range mtxs {
		newBalance -= mtx.Spending()
	}

	// build the cache from DB
	lastApplied := types.NewLayerID(100)
	tc.mockTP.EXPECT().LastAppliedLayer().Return(lastApplied, nil)
	tc.mockTP.EXPECT().GetAllPending().Return(mtxs, nil)
	require.NoError(t, tc.BuildFromScratch())
	for _, mtx := range mtxs {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*types.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, tc.cache, expectedMempool)

	// tx0 and tx1 got packed into a block
	// tx1 and tx2 got packed into a proposal
	lid := lastApplied.Add(1)
	pid := types.ProposalID{1, 2, 3}
	bid := types.BlockID{3, 2, 1}
	addedToBlock := []types.TransactionID{mtxs[0].ID(), mtxs[1].ID()}
	addedToProposal := []types.TransactionID{mtxs[1].ID(), mtxs[2].ID()}
	tc.mockTP.EXPECT().AddToBlock(lid, bid, addedToBlock).DoAndReturn(
		func(lid types.LayerID, bid types.BlockID, _ []types.TransactionID) error {
			for _, mtx := range mtxs[:2] {
				mtx.LayerID = lid
				mtx.BlockID = bid
			}
			return nil
		})
	tc.mockTP.EXPECT().AddToProposal(lid.Add(1), pid, addedToProposal).DoAndReturn(
		func(lid types.LayerID, _ types.ProposalID, _ []types.TransactionID) error {
			mtxs[2].LayerID = lid
			return nil
		})
	require.NoError(t, tc.LinkTXsWithBlock(lid, bid, addedToBlock))
	require.NoError(t, tc.LinkTXsWithProposal(lid.Add(1), pid, addedToProposal))
	for _, mtx := range mtxs {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	// mempool will only include transactions that are not in proposals/blocks
	expectedMempool = map[types.Address][]*types.NanoTX{ta.principal: toNanoTXs(mtxs[3:])}
	checkMempool(t, tc.cache, expectedMempool)

	// the block with tx0 and tx1 is applied.
	// there is also an incoming fund of `income` to the principal's account
	income := amount * 100
	ta.nonce += 2
	for _, mtx := range mtxs[:2] {
		ta.balance -= mtx.Spending()
	}
	ta.balance += income
	applied := []*types.Transaction{&mtxs[0].Transaction, &mtxs[1].Transaction}
	appliedByNonce := map[uint64]types.TransactionID{
		mtxs[0].AccountNonce: mtxs[0].ID(),
		mtxs[1].AccountNonce: mtxs[1].ID(),
	}
	tc.mockTP.EXPECT().ApplyLayer(lid, bid, ta.principal, appliedByNonce)
	tc.mockTP.EXPECT().DiscardNonceBelow(ta.principal, mtxs[0].AccountNonce)
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce).Return(mtxs[2:], nil)
	for _, mtx := range mtxs[2:] {
		tc.mockTP.EXPECT().SetNextLayerBlock(mtx.ID(), lid).Return(mtx.LayerID, mtx.BlockID, nil)
	}
	require.NoError(t, tc.ApplyLayer(lid, bid, applied))
	for _, mtx := range mtxs[:2] {
		checkNoTX(t, tc.cache, mtx.ID())
	}
	for _, mtx := range mtxs[2:] {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance+income)
	// mempool is unchanged
	checkMempool(t, tc.cache, expectedMempool)

	// revert to one layer before lid
	revertTo := lid.Sub(1)
	ta.nonce -= 2
	ta.balance = prevBalance

	tc.mockTP.EXPECT().UndoLayers(lid).Return(nil)
	tc.mockTP.EXPECT().LastAppliedLayer().Return(revertTo, nil)
	tc.mockTP.EXPECT().GetAllPending().Return(mtxs, nil)
	require.NoError(t, tc.cache.RevertToLayer(revertTo))
	for _, mtx := range mtxs {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
}

func TestCache_Account_TXInMultipleLayers(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+4)
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, mtxs)

	// tx0 got packed into block0 at lid
	// tx1 got packed into block1 at lid and a proposal at lid+1
	bid0 := types.BlockID{1, 2, 3}
	bid1 := types.BlockID{3, 2, 1}
	addedToBlock0 := []types.TransactionID{mtxs[0].ID()}
	addedToBlock1 := []types.TransactionID{mtxs[1].ID()}
	lid := tc.lastApplied.Add(1)
	tc.mockTP.EXPECT().AddToBlock(lid, bid0, addedToBlock0).DoAndReturn(
		func(lid types.LayerID, bid types.BlockID, _ []types.TransactionID) error {
			mtxs[0].LayerID = lid
			mtxs[0].BlockID = bid
			return nil
		})
	tc.mockTP.EXPECT().AddToBlock(lid, bid1, addedToBlock1).DoAndReturn(
		func(lid types.LayerID, bid types.BlockID, _ []types.TransactionID) error {
			mtxs[1].LayerID = lid
			mtxs[1].BlockID = bid
			return nil
		})
	pid := types.ProposalID{3, 3, 3}
	tc.mockTP.EXPECT().AddToProposal(lid.Add(1), pid, addedToBlock1).Return(nil)
	require.NoError(t, tc.LinkTXsWithBlock(lid, bid0, addedToBlock0))
	require.NoError(t, tc.LinkTXsWithBlock(lid, bid1, addedToBlock1))
	require.NoError(t, tc.LinkTXsWithProposal(lid.Add(1), pid, addedToBlock1))
	for _, mtx := range mtxs {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)

	// mempool will only include transactions that are not in proposals/blocks
	expectedMempool := map[types.Address][]*types.NanoTX{ta.principal: toNanoTXs(mtxs[2:])}
	checkMempool(t, tc.cache, expectedMempool)

	// block0 is applied.
	// there is also an incoming fund of `income` to the principal's account
	income := amount * 100
	ta.nonce++
	ta.balance = ta.balance - mtxs[0].Spending() + income
	applied := []*types.Transaction{&mtxs[0].Transaction}
	appliedByNonce := map[uint64]types.TransactionID{mtxs[0].AccountNonce: mtxs[0].ID()}
	tc.mockTP.EXPECT().ApplyLayer(lid, bid0, ta.principal, appliedByNonce)
	tc.mockTP.EXPECT().DiscardNonceBelow(ta.principal, mtxs[0].AccountNonce)
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce).Return(mtxs[1:], nil)
	tc.mockTP.EXPECT().SetNextLayerBlock(mtxs[1].ID(), lid).DoAndReturn(
		func(tid types.TransactionID, lid types.LayerID) (types.LayerID, types.BlockID, error) {
			mtxs[1].LayerID = lid.Add(1)
			return lid.Add(1), types.EmptyBlockID, nil
		})
	for _, mtx := range mtxs[2:] {
		tc.mockTP.EXPECT().SetNextLayerBlock(mtx.ID(), lid).Return(types.LayerID{}, types.EmptyBlockID, nil)
	}
	require.NoError(t, tc.ApplyLayer(lid, bid0, applied))
	checkNoTX(t, tc.cache, mtxs[0].ID())
	for _, mtx := range mtxs[1:] {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance+income)
	// mempool is unchanged
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_TooManyTXs(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	ta.balance = uint64(1000000)
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)

	tc.mockTP.EXPECT().LastAppliedLayer().Return(types.NewLayerID(100), nil)
	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+maxTXsPerAcct)
	tc.mockTP.EXPECT().GetAllPending().Return(mtxs, nil)
	require.ErrorIs(t, tc.BuildFromScratch(), errTooManyNonce)
	last := len(mtxs) - 1
	for _, mtx := range mtxs[:last] {
		checkTX(t, tc.cache, mtx)
	}
	// the last one is not in the cache
	checkNoTX(t, tc.cache, mtxs[last].ID())

	newNextNonce := ta.nonce + maxTXsPerAcct
	newBalance := ta.balance
	for _, mtx := range mtxs[:last] {
		newBalance -= mtx.Spending()
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*types.NanoTX{ta.principal: toNanoTXs(mtxs[:last])}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_TooManySameNonceTXs(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	ta.balance = uint64(1000000)
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)

	tc.mockTP.EXPECT().LastAppliedLayer().Return(types.NewLayerID(100), nil)
	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+maxTXsPerNonce)
	for i, mtx := range mtxs {
		mtx.Fee = fee + uint64(i)
		mtx.AccountNonce = ta.nonce
	}
	// the last one has the highest fee but not considered
	tc.mockTP.EXPECT().GetAllPending().Return(mtxs, nil)
	require.NoError(t, tc.BuildFromScratch())
	cutoff := len(mtxs) - 2

	best := mtxs[cutoff]
	checkProjection(t, tc.cache, ta.principal, ta.nonce+1, ta.balance-best.Spending())
	expectedMempool := map[types.Address][]*types.NanoTX{ta.principal: {types.NewNanoTX(best)}}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_NonceTooSmall_AllPendingTXs(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)

	mtxs := generatePendingTXs(t, ta.signer, ta.nonce-3, ta.nonce-1)
	tc.mockTP.EXPECT().LastAppliedLayer().Return(types.NewLayerID(100), nil)
	tc.mockTP.EXPECT().GetAllPending().Return(mtxs, nil)
	require.NoError(t, tc.BuildFromScratch())
	for _, mtx := range mtxs {
		checkNoTX(t, tc.cache, mtx.ID())
	}

	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.False(t, tc.HasPendingTX(ta.principal))
}

func TestCache_Account_InsufficientBalance_AllPendingTXs(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)

	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+2)
	for _, mtx := range mtxs {
		// make it so none of the txs is feasible
		mtx.Amount = ta.balance
	}
	tc.mockTP.EXPECT().LastAppliedLayer().Return(types.NewLayerID(100), nil)
	tc.mockTP.EXPECT().GetAllPending().Return(mtxs, nil)
	require.NoError(t, tc.BuildFromScratch())
	for _, mtx := range mtxs {
		checkNoTX(t, tc.cache, mtx.ID())
	}

	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.True(t, tc.HasPendingTX(ta.principal))
}

func TestCache_Account_Add_TooManyTXs(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	ta.balance = uint64(1000000)
	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+maxTXsPerAcct-1)
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, mtxs)

	oneTooMany := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce+maxTXsPerAcct, amount, fee, ta.signer),
		Received:    time.Now(),
	}
	require.ErrorIs(t, tc.Add(&oneTooMany.Transaction, oneTooMany.Received), errTooManyNonce)
	checkNoTX(t, tc.cache, oneTooMany.ID())

	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*types.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_Add_SuperiorReplacesInferior(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	oldOne := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, amount, fee, ta.signer),
		Received:    time.Now(),
	}
	buildSingleAccountCache(t, tc, ta, []*types.MeshTransaction{oldOne})

	// now add a superior tx
	higherFee := fee + 1
	better := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, amount, higherFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, tc.Add(&better.Transaction, better.Received))
	checkTX(t, tc.cache, better)
	checkNoTX(t, tc.cache, oldOne.ID())
	checkProjection(t, tc.cache, ta.principal, ta.nonce+1, ta.balance-better.Spending())
	expectedMempool := map[types.Address][]*types.NanoTX{ta.principal: {types.NewNanoTX(better)}}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_Add_SuperiorReplacesInferior_EvictLaterNonce(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+4)
	buildSingleAccountCache(t, tc, ta, mtxs)

	// now add a tx at the next nonce that cause all later nonce transactions to be infeasible
	higherFee := fee + 1
	bigAmount := ta.balance - higherFee
	better := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, bigAmount, higherFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, tc.Add(&better.Transaction, better.Received))
	checkTX(t, tc.cache, better)
	for _, mtx := range mtxs {
		checkNoTX(t, tc.cache, mtx.ID())
	}
	checkProjection(t, tc.cache, ta.principal, ta.nonce+1, 0)
	expectedMempool := map[types.Address][]*types.NanoTX{ta.principal: {types.NewNanoTX(better)}}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_Add_NonceTooSmall(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	buildSingleAccountCache(t, tc, ta, nil)

	tx := newTx(t, ta.nonce-1, amount, fee, ta.signer)
	require.ErrorIs(t, tc.Add(tx, time.Now()), errBadNonce)
	checkNoTX(t, tc.cache, tx.ID())
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.False(t, tc.HasPendingTX(ta.principal))
}

func TestCache_Account_Add_NonceTooBig(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	buildSingleAccountCache(t, tc, ta, nil)

	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+1)
	// adding the larger nonce tx first
	require.ErrorIs(t, tc.Add(&mtxs[1].Transaction, mtxs[1].Received), errNonceTooBig)
	checkNoTX(t, tc.cache, mtxs[1].ID())
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.True(t, tc.HasPendingTX(ta.principal))

	// now add the tx that bridge the nonce gap
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce+1).Return([]*types.MeshTransaction{mtxs[1]}, nil)
	require.NoError(t, tc.Add(&mtxs[0].Transaction, mtxs[0].Received))
	for _, mtx := range mtxs {
		checkTX(t, tc.cache, mtx)
	}
	newBalance := ta.balance
	for _, mtx := range mtxs {
		newBalance -= mtx.Spending()
	}
	checkProjection(t, tc.cache, ta.principal, ta.nonce+2, newBalance)
	expectedMempool := map[types.Address][]*types.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_Add_InsufficientBalance_NewNonce(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	buildSingleAccountCache(t, tc, ta, nil)

	tx := newTx(t, ta.nonce, prevBalance, fee, ta.signer)
	require.ErrorIs(t, tc.Add(tx, time.Now()), errInsufficientBalance)
	checkNoTX(t, tc.cache, tx.ID())
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.True(t, tc.HasPendingTX(ta.principal))
}

func TestCache_Account_Add_InsufficientBalance_ExistingNonce(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, amount, fee, ta.signer),
		Received:    time.Now(),
	}
	buildSingleAccountCache(t, tc, ta, []*types.MeshTransaction{mtx})

	spender := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, ta.balance, fee, ta.signer),
		Received:    time.Now(),
	}
	require.ErrorIs(t, tc.Add(&spender.Transaction, spender.Received), errInsufficientBalance)
	checkNoTX(t, tc.cache, spender.ID())
	checkProjection(t, tc.cache, ta.principal, ta.nonce+1, ta.balance-mtx.Spending())
	expectedMempool := map[types.Address][]*types.NanoTX{ta.principal: {types.NewNanoTX(mtx)}}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_BalanceRelaxedAfterApply(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, amount, fee, ta.signer),
		Received:    time.Now(),
	}
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, []*types.MeshTransaction{mtx})

	pending := generatePendingTXs(t, ta.signer, ta.nonce+1, ta.nonce+4)
	largeAmount := prevBalance
	for _, p := range pending {
		p.Amount = largeAmount
		require.Error(t, tc.Add(&p.Transaction, p.Received))
		checkNoTX(t, tc.cache, p.ID())
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*types.NanoTX{ta.principal: {types.NewNanoTX(mtx)}}
	checkMempool(t, tc.cache, expectedMempool)

	// apply lid
	// there is also an incoming fund of `income` to the principal's account, which will make
	// transactions in `pending` feasible now
	income := prevBalance * 100
	ta.nonce++
	ta.balance = ta.balance - mtx.Spending() + income
	applied := []*types.Transaction{&mtx.Transaction}
	appliedByNonce := map[uint64]types.TransactionID{mtx.AccountNonce: mtx.ID()}
	lid := tc.applied.Add(1)
	bid := types.BlockID{1, 2, 3}
	tc.mockTP.EXPECT().ApplyLayer(lid, bid, ta.principal, appliedByNonce)
	tc.mockTP.EXPECT().DiscardNonceBelow(ta.principal, mtx.AccountNonce)
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce).Return(pending, nil)
	for _, p := range pending {
		tc.mockTP.EXPECT().SetNextLayerBlock(p.ID(), lid).Return(types.LayerID{}, types.EmptyBlockID, nil)
	}
	require.NoError(t, tc.ApplyLayer(lid, bid, applied))
	// all pending txs are added to cache now
	newNextNonce = ta.nonce + uint64(len(pending))
	newBalance = ta.balance
	for _, p := range pending {
		newBalance -= p.Spending()
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool = map[types.Address][]*types.NanoTX{ta.principal: toNanoTXs(pending)}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_BalanceRelaxedAfterApply_EvictLaterNonce(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+4)
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, mtxs)

	higherFee := fee + 1
	largeAmount := prevBalance - higherFee
	better := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce+1, largeAmount, higherFee, ta.signer),
		Received:    time.Now(),
	}

	require.ErrorIs(t, tc.Add(&better.Transaction, better.Received), errInsufficientBalance)
	checkNoTX(t, tc.cache, better.ID())
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*types.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, tc.cache, expectedMempool)

	// apply lid
	// there is also an incoming fund of `income` to the principal's account
	// the income is just enough to allow `better` to be feasible
	income := mtxs[0].Spending()
	ta.nonce++
	ta.balance = ta.balance - mtxs[0].Spending() + income
	applied := []*types.Transaction{&mtxs[0].Transaction}
	appliedByNonce := map[uint64]types.TransactionID{mtxs[0].AccountNonce: mtxs[0].ID()}
	lid := tc.applied.Add(1)
	bid := types.BlockID{1, 2, 3}
	tc.mockTP.EXPECT().ApplyLayer(lid, bid, ta.principal, appliedByNonce)
	tc.mockTP.EXPECT().DiscardNonceBelow(ta.principal, mtxs[0].AccountNonce)
	pending := append(mtxs[1:], better)
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce).Return(pending, nil)
	for _, p := range pending {
		tc.mockTP.EXPECT().SetNextLayerBlock(p.ID(), lid).Return(types.LayerID{}, types.EmptyBlockID, nil)
	}
	require.NoError(t, tc.ApplyLayer(lid, bid, applied))
	checkProjection(t, tc.cache, ta.principal, ta.nonce+1, 0)
	expectedMempool = map[types.Address][]*types.NanoTX{ta.principal: {types.NewNanoTX(better)}}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_EvictedAfterApply(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, amount, fee, ta.signer),
		Received:    time.Now(),
	}
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, []*types.MeshTransaction{mtx})

	ta.nonce++
	ta.balance = ta.balance - mtx.Spending()
	applied := []*types.Transaction{&mtx.Transaction}
	appliedByNonce := map[uint64]types.TransactionID{mtx.AccountNonce: mtx.ID()}
	lid := tc.applied.Add(1)
	bid := types.BlockID{1, 2, 3}
	tc.mockTP.EXPECT().ApplyLayer(lid, bid, ta.principal, appliedByNonce)
	tc.mockTP.EXPECT().DiscardNonceBelow(ta.principal, mtx.AccountNonce)
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce).Return(nil, nil)
	require.NoError(t, tc.ApplyLayer(lid, bid, applied))
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	checkMempool(t, tc.cache, nil)
	require.False(t, tc.HasPendingTX(ta.principal))
}

func TestCache_Account_NotEvictedAfterApplyDueToNonceGap(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, amount, fee, ta.signer),
		Received:    time.Now(),
	}
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, []*types.MeshTransaction{mtx})

	ta.nonce++
	ta.balance = ta.balance - mtx.Spending()
	applied := []*types.Transaction{&mtx.Transaction}
	appliedByNonce := map[uint64]types.TransactionID{mtx.AccountNonce: mtx.ID()}
	lid := tc.applied.Add(1)
	bid := types.BlockID{1, 2, 3}
	pendingWithGap := generatePendingTXs(t, ta.signer, mtx.AccountNonce+2, mtx.AccountNonce+3)
	tc.mockTP.EXPECT().ApplyLayer(lid, bid, ta.principal, appliedByNonce)
	tc.mockTP.EXPECT().DiscardNonceBelow(ta.principal, mtx.AccountNonce)
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce).Return(pendingWithGap, nil)
	for _, p := range pendingWithGap {
		tc.mockTP.EXPECT().SetNextLayerBlock(p.ID(), lid).Return(types.LayerID{}, types.EmptyBlockID, nil)
	}
	require.NoError(t, tc.ApplyLayer(lid, bid, applied))
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	checkMempool(t, tc.cache, nil)
	require.True(t, tc.HasPendingTX(ta.principal))
}

func TestCache_Account_TXsAppliedOutOfOrder(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+1)
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, mtxs)

	applied := []*types.Transaction{&mtxs[1].Transaction}
	lid := tc.applied.Add(1)
	bid := types.BlockID{1, 2, 3}
	require.ErrorIs(t, tc.ApplyLayer(lid, bid, applied), errNonceNotInOrder)
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*types.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_BuildFromScratch(t *testing.T) {
}

func TestCache_Add(t *testing.T) {
}

func TestCache_LinkTXsWithProposal(t *testing.T) {
}

func TestCache_LinkTXsWithBlock(t *testing.T) {
}

func TestCache_ApplyLayer(t *testing.T) {
}

func TestCache_ApplyLayer_OutOfOrder(t *testing.T) {
}

func TestCache_RevertToLayer(t *testing.T) {
}

func TestCache_GetMempool(t *testing.T) {
	// mark some txs with layer after the first empty layer
}

func TestCache_GetProjection(t *testing.T) {
}
