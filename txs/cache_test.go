package txs

import (
	"math/rand"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/txs/mocks"
	txtypes "github.com/spacemeshos/go-spacemesh/txs/types"
)

type testCache struct {
	*cache
	mockTP *mocks.MocktxProvider
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
	t.Helper()
	ctrl := gomock.NewController(t)
	mtp := mocks.NewMocktxProvider(ctrl)
	return &testCache{
		cache:  newCache(mtp, sf, logtest.New(t)),
		mockTP: mtp,
	}
}

func generatePendingTXs(t *testing.T, signer *signing.EdSigner, from, to uint64) []*types.MeshTransaction {
	t.Helper()
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
	t.Helper()
	require.Nil(t, c.Get(tid))
}

func checkMempool(t *testing.T, c *cache, expected map[types.Address][]*txtypes.NanoTX) {
	t.Helper()
	mempool, err := c.GetMempool()
	require.NoError(t, err)
	require.Len(t, mempool, len(expected))
	for addr := range mempool {
		require.EqualValues(t, expected[addr], mempool[addr])
	}
}

func checkProjection(t *testing.T, c *cache, addr types.Address, nonce, balance uint64) {
	t.Helper()
	pNonce, pBalance := c.GetProjection(addr)
	require.Equal(t, nonce, pNonce)
	require.Equal(t, balance, pBalance)
}

func toNanoTXs(mtxs []*types.MeshTransaction) []*txtypes.NanoTX {
	ntxs := make([]*txtypes.NanoTX, 0, len(mtxs))
	for _, mtx := range mtxs {
		ntxs = append(ntxs, txtypes.NewNanoTX(mtx))
	}
	return ntxs
}

func createCache(t *testing.T, numAccounts int) (*testCache, map[types.Address]*testAcct) {
	t.Helper()
	accounts := make(map[types.Address]*testAcct)
	for i := 0; i < numAccounts; i++ {
		signer := signing.NewEdSigner()
		principal := types.GenerateAddress(signer.PublicKey().Bytes())
		accounts[principal] = &testAcct{
			signer:    signer,
			principal: principal,
			nonce:     uint64(rand.Int63n(1000)),
			balance:   rand.Uint64(),
		}
	}
	return createTestCache(t, getStateFunc(accounts)), accounts
}

func createSingleAccountTestCache(t *testing.T) (*testCache, *testAcct) {
	t.Helper()
	signer := signing.NewEdSigner()
	principal := types.GenerateAddress(signer.PublicKey().Bytes())
	ta := &testAcct{signer: signer, principal: principal, nonce: uint64(rand.Int63n(1000)), balance: defaultBalance}
	states := map[types.Address]*testAcct{principal: ta}
	return createTestCache(t, getStateFunc(states)), ta
}

func buildCache(t *testing.T, tc *testCache, accounts map[types.Address]*testAcct, accountTXs map[types.Address][]*types.MeshTransaction, totalPending int) {
	t.Helper()
	allPending := make([]*types.MeshTransaction, 0, totalPending)
	for principal, ta := range accounts {
		if mtxs, ok := accountTXs[principal]; ok {
			checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
			allPending = append(allPending, mtxs...)
		}
	}
	tc.mockTP.EXPECT().GetAllPending().Return(allPending, nil)
	require.NoError(t, tc.BuildFromScratch())

	expectedMempool := make(map[types.Address][]*txtypes.NanoTX)
	for principal, ta := range accounts {
		if mtxs, ok := accountTXs[principal]; ok {
			num := len(mtxs)
			if num > maxTXsPerAcct {
				num = maxTXsPerAcct
			}
			newNextNonce := ta.nonce + uint64(num)
			newBalance := ta.balance
			for _, mtx := range mtxs[:num] {
				checkTX(t, tc.cache, mtx)
				newBalance -= mtx.Spending()
			}
			checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
			expectedMempool[principal] = toNanoTXs(mtxs[:num])
		}
	}
	checkMempool(t, tc.cache, expectedMempool)
}

func buildSingleAccountCache(t *testing.T, tc *testCache, ta *testAcct, mtxs []*types.MeshTransaction) (uint64, uint64) {
	t.Helper()
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)

	newNextNonce := ta.nonce + uint64(len(mtxs))
	newBalance := ta.balance
	for _, mtx := range mtxs {
		newBalance -= mtx.Spending()
	}

	tc.mockTP.EXPECT().GetAllPending().Return(mtxs, nil)
	require.NoError(t, tc.BuildFromScratch())
	for _, mtx := range mtxs {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	var expectedMempool map[types.Address][]*txtypes.NanoTX
	if len(mtxs) > 0 {
		expectedMempool = map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
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
	tc.mockTP.EXPECT().GetAllPending().Return(mtxs, nil)
	require.NoError(t, tc.BuildFromScratch())
	for _, mtx := range mtxs {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, tc.cache, expectedMempool)

	// tx0 and tx1 got packed into a block
	// tx1 and tx2 got packed into a proposal
	lid := types.NewLayerID(97)
	pid := types.ProposalID{1, 2, 3}
	bid := types.BlockID{3, 2, 1}
	addedToBlock := []types.TransactionID{mtxs[0].ID(), mtxs[1].ID()}
	for _, mtx := range mtxs[:2] {
		mtx.LayerID = lid
		mtx.BlockID = bid
	}
	addedToProposal := []types.TransactionID{mtxs[1].ID(), mtxs[2].ID()}
	mtxs[2].LayerID = lid.Add(1)
	tc.mockTP.EXPECT().AddToBlock(lid, bid, addedToBlock).Return(nil)
	tc.mockTP.EXPECT().AddToProposal(lid.Add(1), pid, addedToProposal).Return(nil)
	require.NoError(t, tc.LinkTXsWithBlock(lid, bid, addedToBlock))
	require.NoError(t, tc.LinkTXsWithProposal(lid.Add(1), pid, addedToProposal))
	for _, mtx := range mtxs {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	// mempool will only include transactions that are not in proposals/blocks
	expectedMempool = map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs[3:])}
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
	tc.mockTP.EXPECT().LastAppliedLayer().Return(lid.Sub(1), nil)
	tc.mockTP.EXPECT().ApplyLayer(lid, bid, ta.principal, appliedByNonce)
	tc.mockTP.EXPECT().DiscardNonceBelow(ta.principal, mtxs[0].AccountNonce)
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce).Return(mtxs[2:], nil)
	for _, mtx := range mtxs[2:] {
		tc.mockTP.EXPECT().SetNextLayerBlock(mtx.ID(), lid).Return(mtx.LayerID, mtx.BlockID, nil)
	}
	warns, errs := tc.ApplyLayer(lid, bid, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)
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
	ta.balance = defaultBalance

	tc.mockTP.EXPECT().UndoLayers(lid).Return(nil)
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

	lid := types.NewLayerID(97)
	// tx0 got packed into block0 at lid
	// tx1 got packed into block1 at lid and a proposal at lid+1
	bid0 := types.BlockID{1, 2, 3}
	bid1 := types.BlockID{3, 2, 1}
	addedToBlock0 := []types.TransactionID{mtxs[0].ID()}
	mtxs[0].LayerID = lid
	mtxs[0].BlockID = bid0
	addedToBlock1 := []types.TransactionID{mtxs[1].ID()}
	mtxs[1].LayerID = lid
	mtxs[1].BlockID = bid1
	tc.mockTP.EXPECT().AddToBlock(lid, bid0, addedToBlock0).Return(nil)
	tc.mockTP.EXPECT().AddToBlock(lid, bid1, addedToBlock1).Return(nil)
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
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs[2:])}
	checkMempool(t, tc.cache, expectedMempool)

	// block0 is applied.
	// there is also an incoming fund of `income` to the principal's account
	income := amount * 100
	ta.nonce++
	ta.balance = ta.balance - mtxs[0].Spending() + income
	applied := []*types.Transaction{&mtxs[0].Transaction}
	appliedByNonce := map[uint64]types.TransactionID{mtxs[0].AccountNonce: mtxs[0].ID()}
	tc.mockTP.EXPECT().LastAppliedLayer().Return(lid.Sub(1), nil)
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
	warns, errs := tc.ApplyLayer(lid, bid0, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)
	checkNoTX(t, tc.cache, mtxs[0].ID())
	for _, mtx := range mtxs[1:] {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance+income)
	// mempool is unchanged
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_TooManyNonce(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	ta.balance = uint64(1000000)
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)

	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+maxTXsPerAcct)
	tc.mockTP.EXPECT().GetAllPending().Return(mtxs, nil)
	require.NoError(t, tc.BuildFromScratch())
	require.True(t, tc.MoreInDB(ta.principal))
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
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs[:last])}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_TooManySameNonceTXs(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	ta.balance = uint64(1000000)
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)

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
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(best)}}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_NonceTooSmall_AllPendingTXs(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)

	mtxs := generatePendingTXs(t, ta.signer, ta.nonce-3, ta.nonce-1)
	tc.mockTP.EXPECT().GetAllPending().Return(mtxs, nil)
	require.NoError(t, tc.BuildFromScratch())
	for _, mtx := range mtxs {
		checkNoTX(t, tc.cache, mtx.ID())
	}

	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.False(t, tc.MoreInDB(ta.principal))
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
	tc.mockTP.EXPECT().GetAllPending().Return(mtxs, nil)
	require.NoError(t, tc.BuildFromScratch())
	for _, mtx := range mtxs {
		checkNoTX(t, tc.cache, mtx.ID())
	}

	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.True(t, tc.MoreInDB(ta.principal))
}

func TestCache_Account_Add_TooManyNonce_OK(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	ta.balance = uint64(1000000)
	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+maxTXsPerAcct-1)
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, mtxs)

	oneTooMany := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce+maxTXsPerAcct, amount, fee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, tc.Add(&oneTooMany.Transaction, oneTooMany.Received))
	require.True(t, tc.MoreInDB(ta.principal))
	checkNoTX(t, tc.cache, oneTooMany.ID())

	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
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
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(better)}}
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
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(better)}}
	checkMempool(t, tc.cache, expectedMempool)
	require.True(t, tc.cache.MoreInDB(ta.principal))
}

func TestCache_Account_Add_NonceTooSmall(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	buildSingleAccountCache(t, tc, ta, nil)

	tx := newTx(t, ta.nonce-1, amount, fee, ta.signer)
	require.ErrorIs(t, tc.Add(tx, time.Now()), errBadNonce)
	checkNoTX(t, tc.cache, tx.ID())
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.False(t, tc.MoreInDB(ta.principal))
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
	require.True(t, tc.MoreInDB(ta.principal))

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
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_Add_InsufficientBalance_NewNonce(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	buildSingleAccountCache(t, tc, ta, nil)

	tx := newTx(t, ta.nonce, defaultBalance, fee, ta.signer)
	require.ErrorIs(t, tc.Add(tx, time.Now()), errInsufficientBalance)
	checkNoTX(t, tc.cache, tx.ID())
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.True(t, tc.MoreInDB(ta.principal))
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
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(mtx)}}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_Add_OutOfOrder(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+2)

	// txs were received via gossip in this order: mtxs[2], mtxs[0], mtxs[1]
	require.ErrorIs(t, tc.Add(&mtxs[2].Transaction, mtxs[2].Received), errNonceTooBig)
	checkNoTX(t, tc.cache, mtxs[2].ID())
	require.True(t, tc.cache.MoreInDB(ta.principal))
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)

	pending := []*types.MeshTransaction{mtxs[2]}
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, mtxs[0].AccountNonce+1).Return(pending, nil)
	require.NoError(t, tc.Add(&mtxs[0].Transaction, mtxs[0].Received))
	checkTX(t, tc.cache, mtxs[0])
	checkNoTX(t, tc.cache, mtxs[2].ID())
	require.True(t, tc.cache.MoreInDB(ta.principal))
	checkProjection(t, tc.cache, ta.principal, mtxs[0].AccountNonce+1, ta.balance-mtxs[0].Spending())
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(mtxs[0])}}
	checkMempool(t, tc.cache, expectedMempool)

	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, mtxs[1].AccountNonce+1).Return(pending, nil)
	require.NoError(t, tc.Add(&mtxs[1].Transaction, mtxs[1].Received))
	checkTX(t, tc.cache, mtxs[1])
	checkTX(t, tc.cache, mtxs[2])
	require.False(t, tc.cache.MoreInDB(ta.principal))
	newBalance := ta.balance
	for _, mtx := range mtxs {
		newBalance -= mtx.Spending()
	}
	checkProjection(t, tc.cache, ta.principal, ta.nonce+uint64(len(mtxs)), newBalance)
	expectedMempool = map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_AppliedTXsNotInCache(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+2)
	// only add the first TX to cache
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, mtxs[:1])

	applied := []*types.Transaction{&mtxs[0].Transaction, &mtxs[1].Transaction, &mtxs[2].Transaction}
	appliedByNonce := map[uint64]types.TransactionID{
		mtxs[0].AccountNonce: mtxs[0].ID(),
		mtxs[1].AccountNonce: mtxs[1].ID(),
		mtxs[2].AccountNonce: mtxs[2].ID(),
	}
	ta.nonce = newNextNonce + 2
	ta.balance = newBalance - mtxs[1].Spending() - mtxs[2].Spending()
	lid := types.NewLayerID(97)
	bid := types.BlockID{1, 2, 3}
	tc.mockTP.EXPECT().LastAppliedLayer().Return(lid.Sub(1), nil)
	tc.mockTP.EXPECT().ApplyLayer(lid, bid, ta.principal, appliedByNonce)
	tc.mockTP.EXPECT().DiscardNonceBelow(ta.principal, mtxs[0].AccountNonce)
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce).Return(nil, nil)
	warns, errs := tc.ApplyLayer(lid, bid, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
}

func TestCache_Account_TooManyNonceAfterApply(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	ta.balance = uint64(1000000)
	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+maxTXsPerAcct+1)
	// build the cache with just one tx
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, mtxs[:1])

	// apply the tx
	applied := []*types.Transaction{&mtxs[0].Transaction}
	appliedByNonce := map[uint64]types.TransactionID{mtxs[0].AccountNonce: mtxs[0].ID()}
	ta.nonce = newNextNonce
	ta.balance = newBalance
	lid := types.NewLayerID(97)
	bid := types.BlockID{1, 2, 3}
	pending := mtxs[1:]
	for _, p := range pending {
		tc.mockTP.EXPECT().SetNextLayerBlock(p.ID(), lid).Return(types.LayerID{}, types.EmptyBlockID, nil)
	}
	tc.mockTP.EXPECT().LastAppliedLayer().Return(lid.Sub(1), nil)
	tc.mockTP.EXPECT().ApplyLayer(lid, bid, ta.principal, appliedByNonce)
	tc.mockTP.EXPECT().DiscardNonceBelow(ta.principal, mtxs[0].AccountNonce)
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce).Return(pending, nil)
	warns, errs := tc.ApplyLayer(lid, bid, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)

	// cache can only accommodate maxTXsPerAcct nonce
	for i := 0; i < maxTXsPerAcct; i++ {
		newNextNonce++
		newBalance -= pending[i].Spending()
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(pending[:maxTXsPerAcct])}
	checkMempool(t, tc.cache, expectedMempool)
	require.True(t, tc.MoreInDB(ta.principal))
}

func TestCache_Account_BalanceRelaxedAfterApply(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, amount, fee, ta.signer),
		Received:    time.Now(),
	}
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, []*types.MeshTransaction{mtx})

	pending := generatePendingTXs(t, ta.signer, ta.nonce+1, ta.nonce+4)
	largeAmount := defaultBalance
	for _, p := range pending {
		p.Amount = largeAmount
		require.Error(t, tc.Add(&p.Transaction, p.Received))
		checkNoTX(t, tc.cache, p.ID())
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(mtx)}}
	checkMempool(t, tc.cache, expectedMempool)

	// apply lid
	// there is also an incoming fund of `income` to the principal's account, which will make
	// transactions in `pending` feasible now
	income := defaultBalance * 100
	ta.nonce++
	ta.balance = ta.balance - mtx.Spending() + income
	applied := []*types.Transaction{&mtx.Transaction}
	appliedByNonce := map[uint64]types.TransactionID{mtx.AccountNonce: mtx.ID()}
	lid := types.NewLayerID(97)
	bid := types.BlockID{1, 2, 3}
	tc.mockTP.EXPECT().LastAppliedLayer().Return(lid.Sub(1), nil)
	tc.mockTP.EXPECT().ApplyLayer(lid, bid, ta.principal, appliedByNonce)
	tc.mockTP.EXPECT().DiscardNonceBelow(ta.principal, mtx.AccountNonce)
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce).Return(pending, nil)
	for _, p := range pending {
		tc.mockTP.EXPECT().SetNextLayerBlock(p.ID(), lid).Return(types.LayerID{}, types.EmptyBlockID, nil)
	}
	warns, errs := tc.ApplyLayer(lid, bid, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)
	// all pending txs are added to cache now
	newNextNonce = ta.nonce + uint64(len(pending))
	newBalance = ta.balance
	for _, p := range pending {
		newBalance -= p.Spending()
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool = map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(pending)}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_BalanceRelaxedAfterApply_EvictLaterNonce(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+4)
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, mtxs)

	higherFee := fee + 1
	largeAmount := defaultBalance - higherFee
	better := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce+1, largeAmount, higherFee, ta.signer),
		Received:    time.Now(),
	}

	require.ErrorIs(t, tc.Add(&better.Transaction, better.Received), errInsufficientBalance)
	checkNoTX(t, tc.cache, better.ID())
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, tc.cache, expectedMempool)

	// apply lid
	// there is also an incoming fund of `income` to the principal's account
	// the income is just enough to allow `better` to be feasible
	income := mtxs[0].Spending()
	ta.nonce++
	ta.balance = ta.balance - mtxs[0].Spending() + income
	applied := []*types.Transaction{&mtxs[0].Transaction}
	appliedByNonce := map[uint64]types.TransactionID{mtxs[0].AccountNonce: mtxs[0].ID()}
	lid := types.NewLayerID(97)
	bid := types.BlockID{1, 2, 3}
	tc.mockTP.EXPECT().LastAppliedLayer().Return(lid.Sub(1), nil)
	tc.mockTP.EXPECT().ApplyLayer(lid, bid, ta.principal, appliedByNonce)
	tc.mockTP.EXPECT().DiscardNonceBelow(ta.principal, mtxs[0].AccountNonce)
	pending := append(mtxs[1:], better)
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce).Return(pending, nil)
	for _, p := range pending {
		tc.mockTP.EXPECT().SetNextLayerBlock(p.ID(), lid).Return(types.LayerID{}, types.EmptyBlockID, nil)
	}
	warns, errs := tc.ApplyLayer(lid, bid, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)
	checkProjection(t, tc.cache, ta.principal, ta.nonce+1, 0)
	expectedMempool = map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(better)}}
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
	lid := types.NewLayerID(97)
	bid := types.BlockID{1, 2, 3}
	tc.mockTP.EXPECT().LastAppliedLayer().Return(lid.Sub(1), nil)
	tc.mockTP.EXPECT().ApplyLayer(lid, bid, ta.principal, appliedByNonce)
	tc.mockTP.EXPECT().DiscardNonceBelow(ta.principal, mtx.AccountNonce)
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce).Return(nil, nil)
	warns, errs := tc.ApplyLayer(lid, bid, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	checkMempool(t, tc.cache, nil)
	require.False(t, tc.MoreInDB(ta.principal))
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
	lid := types.NewLayerID(97)
	bid := types.BlockID{1, 2, 3}
	pendingWithGap := generatePendingTXs(t, ta.signer, mtx.AccountNonce+2, mtx.AccountNonce+3)
	tc.mockTP.EXPECT().LastAppliedLayer().Return(lid.Sub(1), nil)
	tc.mockTP.EXPECT().ApplyLayer(lid, bid, ta.principal, appliedByNonce)
	tc.mockTP.EXPECT().DiscardNonceBelow(ta.principal, mtx.AccountNonce)
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce).Return(pendingWithGap, nil)
	for _, p := range pendingWithGap {
		tc.mockTP.EXPECT().SetNextLayerBlock(p.ID(), lid).Return(types.LayerID{}, types.EmptyBlockID, nil)
	}
	warns, errs := tc.ApplyLayer(lid, bid, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	checkMempool(t, tc.cache, nil)
	require.True(t, tc.MoreInDB(ta.principal))
}

func TestCache_Account_TXsAppliedOutOfOrder(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+1)
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, mtxs)

	applied := []*types.Transaction{&mtxs[1].Transaction}
	lid := types.NewLayerID(97)
	bid := types.BlockID{1, 2, 3}
	tc.mockTP.EXPECT().LastAppliedLayer().Return(lid.Sub(1), nil)
	tc.mockTP.EXPECT().GetAcctPendingFromNonce(ta.principal, ta.nonce).Return(mtxs, nil)
	for _, mtx := range mtxs {
		tc.mockTP.EXPECT().SetNextLayerBlock(mtx.ID(), lid).Return(mtx.LayerID, mtx.BlockID, nil)
	}
	warns, errs := tc.ApplyLayer(lid, bid, applied)
	require.NotEmpty(t, warns)
	require.ErrorIs(t, warns[0], errNonceNotInOrder)
	require.Empty(t, errs)
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_BuildFromScratch(t *testing.T) {
	tc, accounts := createCache(t, 1000)
	mtxs := make(map[types.Address][]*types.MeshTransaction)
	totalNumTXs := 0
	for principal, ta := range accounts {
		numTXs := uint64(rand.Intn(100))
		if numTXs == 0 {
			continue
		}
		minBalance := numTXs * (amount + fee)
		if ta.balance < minBalance {
			ta.balance = minBalance
		}
		mtxs[principal] = generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+numTXs-1)
		totalNumTXs += int(numTXs)
	}
	buildCache(t, tc, accounts, mtxs, totalNumTXs)
}

func TestCache_BuildFromScratch_AllHaveTooManyNonce_OK(t *testing.T) {
	numAccounts := 10
	tc, accounts := createCache(t, 10)
	// create too many nonce for each account
	numTXsEach := maxTXsPerAcct + 1
	totalNumTXs := numAccounts * numTXsEach
	byAddrAndNonce := make(map[types.Address][]*types.MeshTransaction)
	for principal, ta := range accounts {
		minBalance := uint64(numTXsEach) * (amount + fee)
		if ta.balance < minBalance {
			ta.balance = minBalance
		}
		byAddrAndNonce[principal] = generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+uint64(numTXsEach)-1)
	}
	buildCache(t, tc, accounts, byAddrAndNonce, totalNumTXs)
	for principal := range accounts {
		require.True(t, tc.MoreInDB(principal))
	}
}

func TestCache_Add(t *testing.T) {
	tc, accounts := createCache(t, 1000)
	buildCache(t, tc, accounts, nil, 0)

	expectedMempool := make(map[types.Address][]*txtypes.NanoTX)
	for principal, ta := range accounts {
		numTXs := uint64(rand.Intn(100))
		if numTXs == 0 {
			continue
		}
		minBalance := numTXs * (amount + fee)
		if ta.balance < minBalance {
			ta.balance = minBalance
		}
		mtxs := generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+numTXs-1)

		newNextNonce := ta.nonce + uint64(len(mtxs))
		newBalance := ta.balance
		for _, mtx := range mtxs {
			require.NoError(t, tc.Add(&mtx.Transaction, mtx.Received))
			checkTX(t, tc.cache, mtx)
			newBalance -= mtx.Spending()
		}
		checkProjection(t, tc.cache, principal, newNextNonce, newBalance)
		expectedMempool[principal] = toNanoTXs(mtxs)
	}
	checkMempool(t, tc.cache, expectedMempool)
}

func buildSmallCache(t *testing.T, tc *testCache, accounts map[types.Address]*testAcct, maxTX int) map[types.Address][]*types.MeshTransaction {
	t.Helper()
	mtxsByAccount := make(map[types.Address][]*types.MeshTransaction)
	totalNumTXs := 0
	for principal, ta := range accounts {
		numTXs := uint64(rand.Intn(maxTX))
		if numTXs == 0 {
			continue
		}
		minBalance := numTXs * (amount + fee)
		if ta.balance < minBalance {
			ta.balance = minBalance
		}
		mtxsByAccount[principal] = generatePendingTXs(t, ta.signer, ta.nonce, ta.nonce+numTXs-1)
		totalNumTXs += int(numTXs)
	}
	buildCache(t, tc, accounts, mtxsByAccount, totalNumTXs)
	return mtxsByAccount
}

func checkMempoolSize(t *testing.T, c *cache, expected int) {
	t.Helper()
	mempool, err := c.GetMempool()
	require.NoError(t, err)
	numTXs := 0
	for _, ntxs := range mempool {
		numTXs += len(ntxs)
	}
	require.Equal(t, expected, numTXs)
}

func TestCache_LinkTXsWithProposal(t *testing.T) {
	tc, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, tc, accounts, 10)
	lid0 := types.NewLayerID(97)
	pid0 := types.ProposalID{1, 2, 3}
	// take the first tx out of each account for proposal 0
	tids0 := make([]types.TransactionID, 0, len(mtxsByAccount))
	totalNumTXs := 0
	for _, mtxs := range mtxsByAccount {
		totalNumTXs += len(mtxs)
		tids0 = append(tids0, mtxs[0].ID())
		mtxs[0].LayerID = lid0
	}
	tc.mockTP.EXPECT().AddToProposal(lid0, pid0, tids0).Return(nil)
	require.NoError(t, tc.LinkTXsWithProposal(lid0, pid0, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, tc.cache, mtxs[0])
	}

	lid1 := lid0.Add(1)
	pid1 := types.ProposalID{2, 3, 4}
	// take the second tx out of each account for proposal 1
	tids1 := make([]types.TransactionID, 0, len(mtxsByAccount))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) > 1 {
			tids1 = append(tids1, mtxs[1].ID())
			mtxs[1].LayerID = lid1
		}
	}
	tc.mockTP.EXPECT().AddToProposal(lid1, pid1, tids1).Return(nil)
	require.NoError(t, tc.LinkTXsWithProposal(lid1, pid1, tids1))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) > 1 {
			checkTX(t, tc.cache, mtxs[1])
		}
	}
	checkMempoolSize(t, tc.cache, totalNumTXs-len(tids0)-len(tids1))
}

func TestCache_LinkTXsWithProposal_MultipleLayers(t *testing.T) {
	tc, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, tc, accounts, 10)
	lid0 := types.NewLayerID(97)
	pid0 := types.ProposalID{1, 2, 3}
	// take the first tx out of each account for proposal 0
	tids0 := make([]types.TransactionID, 0, len(mtxsByAccount))
	totalNumTXs := 0
	for _, mtxs := range mtxsByAccount {
		totalNumTXs += len(mtxs)
		tids0 = append(tids0, mtxs[0].ID())
		mtxs[0].LayerID = lid0
	}
	tc.mockTP.EXPECT().AddToProposal(lid0, pid0, tids0).Return(nil)
	require.NoError(t, tc.LinkTXsWithProposal(lid0, pid0, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, tc.cache, mtxs[0])
	}

	lid1 := lid0.Add(1)
	pid1 := types.ProposalID{2, 3, 4}
	// take the same set of txs in proposal 0
	tc.mockTP.EXPECT().AddToProposal(lid1, pid1, tids0).Return(nil)
	require.NoError(t, tc.LinkTXsWithProposal(lid1, pid1, tids0))
	for _, mtxs := range mtxsByAccount {
		// all txs should still be at lid0
		checkTX(t, tc.cache, mtxs[0])
	}
	checkMempoolSize(t, tc.cache, totalNumTXs-len(tids0))
}

func TestCache_LinkTXsWithBlock(t *testing.T) {
	tc, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, tc, accounts, 10)
	lid0 := types.NewLayerID(97)
	bid0 := types.BlockID{1, 2, 3}
	// take the first tx out of each account for block 0
	tids0 := make([]types.TransactionID, 0, len(mtxsByAccount))
	totalNumTXs := 0
	for _, mtxs := range mtxsByAccount {
		totalNumTXs += len(mtxs)
		tids0 = append(tids0, mtxs[0].ID())
		mtxs[0].LayerID = lid0
		mtxs[0].BlockID = bid0
	}
	tc.mockTP.EXPECT().AddToBlock(lid0, bid0, tids0).Return(nil)
	require.NoError(t, tc.LinkTXsWithBlock(lid0, bid0, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, tc.cache, mtxs[0])
	}

	lid1 := lid0.Add(1)
	bid1 := types.BlockID{2, 3, 4}
	// take the second tx out of each account for block 1
	tids1 := make([]types.TransactionID, 0, len(mtxsByAccount))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) > 1 {
			tids1 = append(tids1, mtxs[1].ID())
			mtxs[1].LayerID = lid1
			mtxs[1].BlockID = bid1
		}
	}
	tc.mockTP.EXPECT().AddToBlock(lid1, bid1, tids1).Return(nil)
	require.NoError(t, tc.LinkTXsWithBlock(lid1, bid1, tids1))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) > 1 {
			checkTX(t, tc.cache, mtxs[1])
		}
	}
	checkMempoolSize(t, tc.cache, totalNumTXs-len(tids0)-len(tids1))
}

func TestCache_LinkTXsWithBlock_MultipleLayers(t *testing.T) {
	tc, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, tc, accounts, 10)
	lid0 := types.NewLayerID(97)
	bid0 := types.BlockID{1, 2, 3}
	// take the first tx out of each account for block 0
	tids0 := make([]types.TransactionID, 0, len(mtxsByAccount))
	totalNumTXs := 0
	for _, mtxs := range mtxsByAccount {
		totalNumTXs += len(mtxs)
		tids0 = append(tids0, mtxs[0].ID())
		mtxs[0].LayerID = lid0
		mtxs[0].BlockID = bid0
	}
	tc.mockTP.EXPECT().AddToBlock(lid0, bid0, tids0).Return(nil)
	require.NoError(t, tc.LinkTXsWithBlock(lid0, bid0, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, tc.cache, mtxs[0])
	}

	lid1 := lid0.Add(1)
	bid1 := types.BlockID{2, 3, 4}
	// take the same set of txs in block 0
	tc.mockTP.EXPECT().AddToBlock(lid1, bid1, tids0).Return(nil)
	require.NoError(t, tc.LinkTXsWithBlock(lid1, bid1, tids0))
	for _, mtxs := range mtxsByAccount {
		// all txs should still be at lid0
		checkTX(t, tc.cache, mtxs[0])
	}
	checkMempoolSize(t, tc.cache, totalNumTXs-len(tids0))
}

func TestCache_ApplyLayerAndRevert(t *testing.T) {
	tc, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, tc, accounts, 10)
	lid := types.NewLayerID(97)
	bid := types.BlockID{1, 2, 3}
	allApplied := make([]*types.Transaction, 0, len(mtxsByAccount)*2)
	for principal, mtxs := range mtxsByAccount {
		lastNonce := mtxs[0].AccountNonce
		newBalance := accounts[principal].balance
		newBalance -= mtxs[0].Spending()
		applied := []*types.Transaction{&mtxs[0].Transaction}
		byNonce := map[uint64]types.TransactionID{mtxs[0].AccountNonce: mtxs[0].ID()}
		var pending []*types.MeshTransaction
		if len(mtxs) >= 2 {
			applied = append(applied, &mtxs[1].Transaction)
			byNonce[mtxs[1].AccountNonce] = mtxs[1].ID()
			lastNonce = mtxs[1].AccountNonce
			newBalance -= mtxs[1].Spending()
			pending = mtxs[2:]
		}
		// adjust state
		accounts[principal].nonce = lastNonce + 1
		accounts[principal].balance = newBalance
		allApplied = append(allApplied, applied...)
		tc.mockTP.EXPECT().ApplyLayer(lid, bid, principal, byNonce)
		tc.mockTP.EXPECT().DiscardNonceBelow(principal, mtxs[0].AccountNonce)
		tc.mockTP.EXPECT().GetAcctPendingFromNonce(principal, lastNonce+1).Return(pending, nil)
		for _, mtx := range pending {
			tc.mockTP.EXPECT().SetNextLayerBlock(mtx.ID(), lid).Return(mtx.LayerID, mtx.BlockID, nil)
		}
	}
	tc.mockTP.EXPECT().LastAppliedLayer().Return(lid.Sub(1), nil)
	warns, errs := tc.ApplyLayer(lid, bid, allApplied)
	require.Empty(t, warns)
	require.Empty(t, errs)

	// now revert
	allPending := make([]*types.MeshTransaction, 0, 10*len(mtxsByAccount))
	expectedMempool := make(map[types.Address][]*txtypes.NanoTX)
	for principal, mtxs := range mtxsByAccount {
		allPending = append(allPending, mtxs...)
		expectedMempool[principal] = toNanoTXs(mtxs)
		// adjust state

		accounts[principal].nonce--
		accounts[principal].balance += mtxs[0].Spending()
		if len(mtxs) >= 2 {
			accounts[principal].nonce--
			accounts[principal].balance += mtxs[1].Spending()
		}
	}
	tc.mockTP.EXPECT().UndoLayers(lid).Return(nil)
	tc.mockTP.EXPECT().GetAllPending().Return(allPending, nil)
	require.NoError(t, tc.RevertToLayer(lid.Sub(1)))
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_ApplyLayer_OutOfOrder(t *testing.T) {
	tc, accounts := createCache(t, 100)
	buildSmallCache(t, tc, accounts, 10)
	lid := types.NewLayerID(97)
	tc.mockTP.EXPECT().LastAppliedLayer().Return(lid.Sub(2), nil).Times(1)
	warns, errs := tc.ApplyLayer(lid, types.BlockID{1, 2, 3}, nil)
	require.Empty(t, warns)
	require.NotEmpty(t, errs)
	require.ErrorIs(t, errs[0], errLayerNotInOrder)
}

func TestCache_GetMempool(t *testing.T) {
	tc, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, tc, accounts, 10)
	lid0 := types.NewLayerID(97)
	bid := types.BlockID{1, 2, 3}
	tids0 := make([]types.TransactionID, 0, len(mtxsByAccount))
	for _, mtxs := range mtxsByAccount {
		tids0 = append(tids0, mtxs[0].ID())
		mtxs[0].LayerID = lid0
		mtxs[0].BlockID = bid
	}
	tc.mockTP.EXPECT().AddToBlock(lid0, bid, tids0).Return(nil)
	require.NoError(t, tc.LinkTXsWithBlock(lid0, bid, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, tc.cache, mtxs[0])
	}

	// mark some txs with layer after the first empty layer
	lid1 := lid0.Add(1)
	pid := types.ProposalID{3, 4, 5}
	tids1 := make([]types.TransactionID, 0, len(mtxsByAccount))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) >= 3 {
			tids1 = append(tids1, mtxs[2].ID())
			mtxs[2].LayerID = lid1
		}
	}
	tc.mockTP.EXPECT().AddToProposal(lid1, pid, tids1).Return(nil)
	require.NoError(t, tc.LinkTXsWithProposal(lid1, pid, tids1))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) >= 3 {
			checkTX(t, tc.cache, mtxs[2])
		}
	}
	expectedMempool := make(map[types.Address][]*txtypes.NanoTX)
	for principal, mtxs := range mtxsByAccount {
		if len(mtxs) > 1 {
			expectedMempool[principal] = toNanoTXs(mtxs[1:])
		}
	}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_GetProjection(t *testing.T) {
	tc, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, tc, accounts, 10)
	for principal, mtxs := range mtxsByAccount {
		expectedNonce := accounts[principal].nonce + uint64(len(mtxs))
		expectedBalance := accounts[principal].balance
		for _, mtx := range mtxs {
			expectedBalance -= mtx.Spending()
		}
		nonce, balance := tc.GetProjection(principal)
		require.Equal(t, expectedNonce, nonce)
		require.Equal(t, expectedBalance, balance)
	}
}
