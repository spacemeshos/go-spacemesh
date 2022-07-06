package txs

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	txtypes "github.com/spacemeshos/go-spacemesh/txs/types"
)

type testAcct struct {
	signer         *signing.EdSigner
	principal      types.Address
	nonce, balance uint64
}

func makeResults(lid types.LayerID, bid types.BlockID, txs ...types.Transaction) []types.TransactionWithResult {
	var results []types.TransactionWithResult
	for _, tx := range txs {
		results = append(results, types.TransactionWithResult{
			Transaction: tx,
			TransactionResult: types.TransactionResult{
				Layer: lid,
				Block: bid,
			},
		})
	}
	return results
}

func getStateFunc(states map[types.Address]*testAcct) stateFunc {
	return func(addr types.Address) (uint64, uint64) {
		st := states[addr]
		return st.nonce, st.balance
	}
}

func newMeshTX(t *testing.T, nonce uint64, signer *signing.EdSigner, amt uint64, received time.Time) *types.MeshTransaction {
	t.Helper()
	return &types.MeshTransaction{
		Transaction: *newTx(t, nonce, amt, defaultFee, signer),
		Received:    received,
	}
}

func genAndSaveTXs(t *testing.T, db *sql.Database, signer *signing.EdSigner, from, to uint64) []*types.MeshTransaction {
	t.Helper()
	mtxs := genTXs(t, signer, from, to)
	saveTXs(t, db, mtxs)
	return mtxs
}

func genTXs(t *testing.T, signer *signing.EdSigner, from, to uint64) []*types.MeshTransaction {
	t.Helper()
	now := time.Now()
	mtxs := make([]*types.MeshTransaction, 0, int(to-from+1))
	for i := from; i <= to; i++ {
		mtx := newMeshTX(t, i, signer, defaultAmount, now.Add(time.Second*time.Duration(i)))
		mtxs = append(mtxs, mtx)
	}
	return mtxs
}

func saveTXs(t *testing.T, db *sql.Database, mtxs []*types.MeshTransaction) {
	t.Helper()
	for _, mtx := range mtxs {
		require.NoError(t, transactions.Add(db, &mtx.Transaction, mtx.Received))
	}
}

func checkTXStateFromDB(t *testing.T, db *sql.Database, txs []*types.MeshTransaction, state types.TXState) {
	for _, mtx := range txs {
		got, err := transactions.Get(db, mtx.ID)
		require.NoError(t, err)
		require.Equal(t, state, got.State)
	}
}

func checkTX(t *testing.T, c *cache, mtx *types.MeshTransaction) {
	t.Helper()
	got := c.Get(mtx.ID)
	require.NotNil(t, got)
	require.Equal(t, mtx.ID, got.ID)
	require.Equal(t, mtx.LayerID, got.Layer)
	require.Equal(t, mtx.BlockID, got.Block)
}

func checkNoTX(t *testing.T, c *cache, tid types.TransactionID) {
	t.Helper()
	require.Nil(t, c.Get(tid))
}

func checkMempool(t *testing.T, c *cache, expected map[types.Address][]*txtypes.NanoTX) {
	t.Helper()
	mempool := c.GetMempool()
	require.Len(t, mempool, len(expected))
	for addr := range mempool {
		var exp, got txtypes.NanoTX
		for i, ntx := range mempool[addr] {
			got = *ntx
			exp = *expected[addr][i]
			require.EqualValues(t, exp.Received.UnixNano(), got.Received.UnixNano())
			got.Received = time.Time{}
			exp.Received = time.Time{}
			require.Equal(t, exp, got)
		}
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

func createState(t *testing.T, numAccounts int) map[types.Address]*testAcct {
	t.Helper()
	const minBalance = 1_000_000
	accounts := make(map[types.Address]*testAcct)
	for i := 0; i < numAccounts; i++ {
		signer := signing.NewEdSigner()
		principal := types.GenerateAddress(signer.PublicKey().Bytes())
		bal := uint64(rand.Int63n(100_000_000))
		if bal < minBalance {
			bal = minBalance
		}
		accounts[principal] = &testAcct{
			signer:    signer,
			principal: principal,
			nonce:     uint64(rand.Int63n(1000)),
			balance:   bal,
		}
	}
	return accounts
}

func createCache(t *testing.T, numAccounts int) (*cache, map[types.Address]*testAcct) {
	t.Helper()
	accounts := createState(t, numAccounts)
	return newCache(sql.InMemory(), getStateFunc(accounts), logtest.New(t)), accounts
}

func createSingleAccountTestCache(t *testing.T) (*cache, *testAcct) {
	t.Helper()
	signer := signing.NewEdSigner()
	principal := types.GenerateAddress(signer.PublicKey().Bytes())
	ta := &testAcct{signer: signer, principal: principal, nonce: uint64(rand.Int63n(1000)), balance: defaultBalance}
	states := map[types.Address]*testAcct{principal: ta}
	return newCache(sql.InMemory(), getStateFunc(states), logtest.New(t)), ta
}

func buildCache(t *testing.T, cache *cache, accounts map[types.Address]*testAcct, accountTXs map[types.Address][]*types.MeshTransaction, totalPending int) {
	t.Helper()
	allPending := make([]*types.MeshTransaction, 0, totalPending)
	for principal, ta := range accounts {
		if mtxs, ok := accountTXs[principal]; ok {
			checkProjection(t, cache, ta.principal, ta.nonce, ta.balance)
			allPending = append(allPending, mtxs...)
		}
	}
	require.NoError(t, cache.buildFromScratch())

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
				checkTX(t, cache, mtx)
				newBalance -= mtx.Spending()
			}
			checkProjection(t, cache, ta.principal, newNextNonce, newBalance)
			expectedMempool[principal] = toNanoTXs(mtxs[:num])
		}
	}
	checkMempool(t, cache, expectedMempool)
}

func buildSingleAccountCache(t *testing.T, cache *cache, ta *testAcct, mtxs []*types.MeshTransaction) (uint64, uint64) {
	t.Helper()
	checkProjection(t, cache, ta.principal, ta.nonce, ta.balance)

	newNextNonce := ta.nonce + uint64(len(mtxs))
	newBalance := ta.balance
	for _, mtx := range mtxs {
		newBalance -= mtx.Spending()
	}

	require.NoError(t, cache.buildFromScratch())
	for _, mtx := range mtxs {
		checkTX(t, cache, mtx)
	}
	checkProjection(t, cache, ta.principal, newNextNonce, newBalance)
	var expectedMempool map[types.Address][]*txtypes.NanoTX
	if len(mtxs) > 0 {
		expectedMempool = map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	}
	checkMempool(t, cache, expectedMempool)
	return newNextNonce, newBalance
}

func TestCache_Account_HappyFlow(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	// nothing in the cache yet
	checkProjection(t, cash, ta.principal, ta.nonce, ta.balance)

	mtxs := genAndSaveTXs(t, cash.db, ta.signer, ta.nonce, ta.nonce+4)
	sameNonces := genAndSaveTXs(t, cash.db, ta.signer, ta.nonce, ta.nonce+1)
	oldNonces := genAndSaveTXs(t, cash.db, ta.signer, ta.nonce-2, ta.nonce-1)
	newNextNonce := ta.nonce + uint64(len(mtxs))
	newBalance := ta.balance
	for _, mtx := range mtxs {
		newBalance -= mtx.Spending()
	}

	// build the cache from DB
	require.NoError(t, cash.buildFromScratch())
	for _, mtx := range mtxs {
		checkTX(t, cash, mtx)
	}
	for _, mtx := range append(oldNonces, sameNonces...) {
		checkNoTX(t, cash, mtx.ID)
	}
	checkProjection(t, cash, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, cash, expectedMempool)
	checkTXStateFromDB(t, cash.db, mtxs, types.MEMPOOL)
	checkTXStateFromDB(t, cash.db, oldNonces, types.MEMPOOL)
	checkTXStateFromDB(t, cash.db, sameNonces, types.MEMPOOL)

	// tx0 and tx1 got packed into a block
	// tx1 and tx2 got packed into a proposal
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(cash.db, lid.Sub(1), types.RandomBlockID()))
	pid := types.ProposalID{1, 2, 3}
	bid := types.BlockID{3, 2, 1}
	addedToBlock := []types.TransactionID{mtxs[0].ID, mtxs[1].ID}
	for _, mtx := range mtxs[:2] {
		mtx.LayerID = lid
		mtx.BlockID = bid
	}
	addedToProposal := []types.TransactionID{mtxs[1].ID, mtxs[2].ID}
	mtxs[2].LayerID = lid.Add(1)
	require.NoError(t, cash.LinkTXsWithBlock(lid, bid, addedToBlock))
	require.NoError(t, cash.LinkTXsWithProposal(lid.Add(1), pid, addedToProposal))
	for _, mtx := range mtxs {
		checkTX(t, cash, mtx)
	}
	checkProjection(t, cash, ta.principal, newNextNonce, newBalance)
	// mempool will only include transactions that are not in proposals/blocks
	expectedMempool = map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs[3:])}
	checkMempool(t, cash, expectedMempool)
	checkTXStateFromDB(t, cash.db, mtxs[:2], types.BLOCK)
	checkTXStateFromDB(t, cash.db, mtxs[2:2], types.PROPOSAL)
	checkTXStateFromDB(t, cash.db, mtxs[3:], types.MEMPOOL)
	checkTXStateFromDB(t, cash.db, oldNonces, types.MEMPOOL)
	checkTXStateFromDB(t, cash.db, sameNonces, types.MEMPOOL)

	// the block with tx0 and tx1 is applied.
	// there is also an incoming fund of `income` to the principal's account
	income := defaultAmount * 100
	ta.nonce += 2
	for _, mtx := range mtxs[:2] {
		ta.balance -= mtx.Spending()
	}
	ta.balance += income
	applied := makeResults(lid, bid, mtxs[0].Transaction, mtxs[1].Transaction)
	warns, errs := cash.ApplyLayer(lid, bid, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)

	for _, mtx := range mtxs[:2] {
		checkNoTX(t, cash, mtx.ID)
	}
	for _, mtx := range mtxs[2:] {
		checkTX(t, cash, mtx)
	}
	checkProjection(t, cash, ta.principal, newNextNonce, newBalance+income)
	// mempool is unchanged
	checkMempool(t, cash, expectedMempool)
	for _, mtx := range append(oldNonces, sameNonces...) {
		got, err := transactions.Get(cash.db, mtx.ID)
		require.NoError(t, err)
		require.Equal(t, types.DISCARDED, got.State)
	}

	// revert to one layer before lid
	revertTo := lid.Sub(1)
	ta.nonce -= 2
	ta.balance = defaultBalance
	require.NoError(t, cash.RevertToLayer(revertTo))
	for _, mtx := range mtxs {
		checkTX(t, cash, mtx)
	}
	for _, mtx := range append(oldNonces, sameNonces...) {
		checkNoTX(t, cash, mtx.ID)
	}
	checkProjection(t, cash, ta.principal, newNextNonce, newBalance)
	checkTXStateFromDB(t, cash.db, mtxs[:2], types.BLOCK)
	checkTXStateFromDB(t, cash.db, mtxs[2:2], types.PROPOSAL)
	checkTXStateFromDB(t, cash.db, mtxs[3:], types.MEMPOOL)
	checkTXStateFromDB(t, cash.db, oldNonces, types.DISCARDED)
	checkTXStateFromDB(t, cash.db, sameNonces, types.MEMPOOL)
}

func TestCache_Account_TXInMultipleLayers(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	mtxs := genAndSaveTXs(t, cash.db, ta.signer, ta.nonce, ta.nonce+4)
	newNextNonce, newBalance := buildSingleAccountCache(t, cash, ta, mtxs)

	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(cash.db, lid.Sub(1), types.RandomBlockID()))
	// tx0 got packed into block0 at lid
	// tx1 got packed into block1 at lid and a proposal at lid+1
	bid0 := types.BlockID{1, 2, 3}
	bid1 := types.BlockID{3, 2, 1}
	addedToBlock0 := []types.TransactionID{mtxs[0].ID}
	mtxs[0].LayerID = lid
	mtxs[0].BlockID = bid0
	addedToBlock1 := []types.TransactionID{mtxs[1].ID}
	mtxs[1].LayerID = lid
	mtxs[1].BlockID = bid1
	pid := types.ProposalID{3, 3, 3}
	// tc.mockTP.EXPECT().AddToProposal(lid.Add(1), pid, addedToBlock1).Return(nil)
	require.NoError(t, cash.LinkTXsWithBlock(lid, bid0, addedToBlock0))
	require.NoError(t, cash.LinkTXsWithBlock(lid, bid1, addedToBlock1))
	require.NoError(t, cash.LinkTXsWithProposal(lid.Add(1), pid, addedToBlock1))
	for _, mtx := range mtxs {
		checkTX(t, cash, mtx)
	}
	checkProjection(t, cash, ta.principal, newNextNonce, newBalance)

	// mempool will only include transactions that are not in proposals/blocks
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs[2:])}
	checkMempool(t, cash, expectedMempool)
	checkTXStateFromDB(t, cash.db, mtxs[:2], types.BLOCK)
	checkTXStateFromDB(t, cash.db, mtxs[2:], types.MEMPOOL)

	// block0 is applied.
	// there is also an incoming fund of `income` to the principal's account
	income := defaultAmount * 100
	ta.nonce++
	ta.balance = ta.balance - mtxs[0].Spending() + income
	applied := makeResults(lid, bid0, mtxs[0].Transaction)
	warns, errs := cash.ApplyLayer(lid, bid0, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)
	checkNoTX(t, cash, mtxs[0].ID)
	mtxs[1].BlockID = types.EmptyBlockID
	mtxs[1].LayerID = lid.Add(1)
	for _, mtx := range mtxs[1:] {
		checkTX(t, cash, mtx)
	}
	checkProjection(t, cash, ta.principal, newNextNonce, newBalance+income)
	// mempool is unchanged
	checkMempool(t, cash, expectedMempool)
	checkTXStateFromDB(t, cash.db, mtxs[:1], types.APPLIED)
	checkTXStateFromDB(t, cash.db, mtxs[1:2], types.PROPOSAL)
	checkTXStateFromDB(t, cash.db, mtxs[2:], types.MEMPOOL)
}

func TestCache_Account_TooManyNonce(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	ta.balance = uint64(1000000)
	checkProjection(t, cash, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, cash, nil)

	mtxs := genAndSaveTXs(t, cash.db, ta.signer, ta.nonce, ta.nonce+maxTXsPerAcct)
	require.NoError(t, cash.buildFromScratch())
	require.True(t, cash.MoreInDB(ta.principal))
	last := len(mtxs) - 1
	for _, mtx := range mtxs[:last] {
		checkTX(t, cash, mtx)
	}
	// the last one is not in the cache
	checkNoTX(t, cash, mtxs[last].ID)

	newNextNonce := ta.nonce + maxTXsPerAcct
	newBalance := ta.balance
	for _, mtx := range mtxs[:last] {
		newBalance -= mtx.Spending()
	}
	checkProjection(t, cash, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs[:last])}
	checkMempool(t, cash, expectedMempool)
}

func TestCache_Account_TooManySameNonceTXs(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	ta.balance = uint64(1000000)
	checkProjection(t, cash, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, cash, nil)

	now := time.Now()
	mtxs := make([]*types.MeshTransaction, 0, maxTXsPerNonce+1)
	for i := 0; i <= maxTXsPerAcct; i++ {
		mtx := newMeshTX(t, ta.nonce, ta.signer, defaultAmount, now.Add(time.Second*time.Duration(i)))
		mtx.GasPrice = defaultFee + uint64(i)
		mtx.MaxGas = 1
		mtxs = append(mtxs, mtx)
		require.NoError(t, transactions.Add(cash.db, &mtx.Transaction, mtx.Received))
	}

	require.NoError(t, cash.buildFromScratch())
	cutoff := len(mtxs) - 2

	best := mtxs[cutoff]
	checkProjection(t, cash, ta.principal, ta.nonce+1, ta.balance-best.Spending())
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(best)}}
	checkMempool(t, cash, expectedMempool)
}

func TestCache_Account_NonceTooSmall_AllPendingTXs(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	checkProjection(t, cash, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, cash, nil)

	mtxs := genAndSaveTXs(t, cash.db, ta.signer, ta.nonce-3, ta.nonce-1)
	require.NoError(t, cash.buildFromScratch())
	for _, mtx := range mtxs {
		checkNoTX(t, cash, mtx.ID)
	}

	checkProjection(t, cash, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, cash, nil)
	require.False(t, cash.MoreInDB(ta.principal))
}

func TestCache_Account_InsufficientBalance_AllPendingTXs(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	checkProjection(t, cash, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, cash, nil)

	now := time.Now()
	mtxs := make([]*types.MeshTransaction, 0, 3)
	for i := 0; i <= 2; i++ {
		mtx := newMeshTX(t, ta.nonce, ta.signer, defaultAmount, now.Add(time.Second*time.Duration(i)))
		// make it so none of the txs is feasible
		mtx.MaxSpend = ta.balance
		mtxs = append(mtxs, mtx)
		require.NoError(t, transactions.Add(cash.db, &mtx.Transaction, mtx.Received))
	}

	require.NoError(t, cash.buildFromScratch())
	for _, mtx := range mtxs {
		checkNoTX(t, cash, mtx.ID)
	}

	checkProjection(t, cash, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, cash, nil)
	require.True(t, cash.MoreInDB(ta.principal))
}

func TestCache_Account_Add_TooManyNonce_OK(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	ta.balance = uint64(1000000)
	mtxs := genAndSaveTXs(t, cash.db, ta.signer, ta.nonce, ta.nonce+maxTXsPerAcct-1)
	newNextNonce, newBalance := buildSingleAccountCache(t, cash, ta, mtxs)

	oneTooMany := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce+maxTXsPerAcct, defaultAmount, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, cash.Add(&oneTooMany.Transaction, oneTooMany.Received, nil))
	require.True(t, cash.MoreInDB(ta.principal))
	checkNoTX(t, cash, oneTooMany.ID)

	checkProjection(t, cash, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, cash, expectedMempool)
}

func TestCache_Account_Add_SuperiorReplacesInferior(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	oldOne := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, defaultAmount, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, transactions.Add(cash.db, &oldOne.Transaction, oldOne.Received))
	buildSingleAccountCache(t, cash, ta, []*types.MeshTransaction{oldOne})

	// now add a superior tx
	higherFee := defaultFee + 1
	better := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, defaultAmount, higherFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, transactions.Add(cash.db, &better.Transaction, better.Received))
	require.NoError(t, cash.Add(&better.Transaction, better.Received, nil))
	checkTX(t, cash, better)
	checkNoTX(t, cash, oldOne.ID)
	checkProjection(t, cash, ta.principal, ta.nonce+1, ta.balance-better.Spending())
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(better)}}
	checkMempool(t, cash, expectedMempool)
}

func TestCache_Account_Add_SuperiorReplacesInferior_EvictLaterNonce(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	mtxs := genAndSaveTXs(t, cash.db, ta.signer, ta.nonce, ta.nonce+4)
	buildSingleAccountCache(t, cash, ta, mtxs)

	// now add a tx at the next nonce that cause all later nonce transactions to be infeasible
	higherFee := defaultFee + 1
	bigAmount := ta.balance - higherFee*defaultGas
	better := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, bigAmount, higherFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, transactions.Add(cash.db, &better.Transaction, better.Received))
	require.NoError(t, cash.Add(&better.Transaction, better.Received, nil))
	checkTX(t, cash, better)
	for _, mtx := range mtxs {
		checkNoTX(t, cash, mtx.ID)
	}
	checkProjection(t, cash, ta.principal, ta.nonce+1, 0)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(better)}}
	checkMempool(t, cash, expectedMempool)
	require.True(t, cash.MoreInDB(ta.principal))
}

func TestCache_Account_Add_NonceTooSmall(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	buildSingleAccountCache(t, cash, ta, nil)

	tx := newTx(t, ta.nonce-1, defaultAmount, defaultFee, ta.signer)
	require.ErrorIs(t, cash.Add(tx, time.Now(), nil), errBadNonce)
	checkNoTX(t, cash, tx.ID)
	checkProjection(t, cash, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, cash, nil)
	require.False(t, cash.MoreInDB(ta.principal))
}

func TestCache_Account_Add_NonceTooBig(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	buildSingleAccountCache(t, cash, ta, nil)

	mtxs := genAndSaveTXs(t, cash.db, ta.signer, ta.nonce, ta.nonce+1)
	// adding the larger nonce tx first
	require.ErrorIs(t, cash.Add(&mtxs[1].Transaction, mtxs[1].Received, nil), errNonceTooBig)
	checkNoTX(t, cash, mtxs[1].ID)
	checkProjection(t, cash, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, cash, nil)
	require.True(t, cash.MoreInDB(ta.principal))

	// now add the tx that bridge the nonce gap
	require.NoError(t, cash.Add(&mtxs[0].Transaction, mtxs[0].Received, nil))
	for _, mtx := range mtxs {
		checkTX(t, cash, mtx)
	}
	newBalance := ta.balance
	for _, mtx := range mtxs {
		newBalance -= mtx.Spending()
	}
	checkProjection(t, cash, ta.principal, ta.nonce+2, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, cash, expectedMempool)
}

func TestCache_Account_Add_InsufficientBalance_NewNonce(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	buildSingleAccountCache(t, cash, ta, nil)

	tx := newTx(t, ta.nonce, defaultBalance, defaultFee, ta.signer)
	require.ErrorIs(t, cash.Add(tx, time.Now(), nil), errInsufficientBalance)
	checkNoTX(t, cash, tx.ID)
	checkProjection(t, cash, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, cash, nil)
	require.True(t, cash.MoreInDB(ta.principal))
}

func TestCache_Account_Add_InsufficientBalance_ExistingNonce(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, defaultAmount, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, transactions.Add(cash.db, &mtx.Transaction, mtx.Received))
	buildSingleAccountCache(t, cash, ta, []*types.MeshTransaction{mtx})

	spender := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, ta.balance, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, transactions.Add(cash.db, &spender.Transaction, spender.Received))
	require.ErrorIs(t, cash.Add(&spender.Transaction, spender.Received, nil), errInsufficientBalance)
	checkNoTX(t, cash, spender.ID)
	checkProjection(t, cash, ta.principal, ta.nonce+1, ta.balance-mtx.Spending())
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(mtx)}}
	checkMempool(t, cash, expectedMempool)
}

func TestCache_Account_Add_OutOfOrder(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	mtxs := genTXs(t, ta.signer, ta.nonce, ta.nonce+2)

	// txs were received via gossip in this order: mtxs[2], mtxs[0], mtxs[1]
	require.NoError(t, transactions.Add(cash.db, &mtxs[2].Transaction, mtxs[2].Received))
	require.ErrorIs(t, cash.Add(&mtxs[2].Transaction, mtxs[2].Received, nil), errNonceTooBig)
	checkNoTX(t, cash, mtxs[2].ID)
	require.True(t, cash.MoreInDB(ta.principal))
	checkProjection(t, cash, ta.principal, ta.nonce, ta.balance)

	require.NoError(t, transactions.Add(cash.db, &mtxs[0].Transaction, mtxs[0].Received))
	require.NoError(t, cash.Add(&mtxs[0].Transaction, mtxs[0].Received, nil))
	checkTX(t, cash, mtxs[0])
	checkNoTX(t, cash, mtxs[2].ID)
	require.True(t, cash.MoreInDB(ta.principal))
	checkProjection(t, cash, ta.principal, mtxs[0].Nonce.Counter+1, ta.balance-mtxs[0].Spending())
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(mtxs[0])}}
	checkMempool(t, cash, expectedMempool)

	require.NoError(t, transactions.Add(cash.db, &mtxs[1].Transaction, mtxs[1].Received))
	require.NoError(t, cash.Add(&mtxs[1].Transaction, mtxs[1].Received, nil))
	checkTX(t, cash, mtxs[1])
	checkTX(t, cash, mtxs[2])
	require.False(t, cash.MoreInDB(ta.principal))
	newBalance := ta.balance
	for _, mtx := range mtxs {
		newBalance -= mtx.Spending()
	}
	checkProjection(t, cash, ta.principal, ta.nonce+uint64(len(mtxs)), newBalance)
	expectedMempool = map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, cash, expectedMempool)
}

func TestCache_Account_AppliedTXsNotInCache(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	mtxs := genTXs(t, ta.signer, ta.nonce, ta.nonce+2)
	saveTXs(t, cash.db, mtxs[:1])
	// only add the first TX to cache
	newNextNonce, newBalance := buildSingleAccountCache(t, cash, ta, mtxs[:1])
	for _, mtx := range mtxs[1:] {
		checkNoTX(t, cash, mtx.ID)
	}

	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(cash.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}

	applied := makeResults(lid, bid, mtxs[0].Transaction, mtxs[1].Transaction, mtxs[2].Transaction)
	// now the rest of the txs are fetched
	saveTXs(t, cash.db, mtxs[1:])
	ta.nonce = newNextNonce + 2
	ta.balance = newBalance - mtxs[1].Spending() - mtxs[2].Spending()

	warns, errs := cash.ApplyLayer(lid, bid, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)
	checkProjection(t, cash, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, cash, nil)
	checkTXStateFromDB(t, cash.db, mtxs, types.APPLIED)
}

func TestCache_Account_TooManyNonceAfterApply(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	ta.balance = uint64(1000000)
	mtxs := genTXs(t, ta.signer, ta.nonce, ta.nonce+maxTXsPerAcct+1)
	saveTXs(t, cash.db, mtxs[:1])
	// build the cache with just one tx
	newNextNonce, newBalance := buildSingleAccountCache(t, cash, ta, mtxs[:1])

	ta.nonce = newNextNonce
	ta.balance = newBalance
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(cash.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}
	applied := makeResults(lid, bid, mtxs[0].Transaction)
	// more txs arrived
	saveTXs(t, cash.db, mtxs[1:])
	warns, errs := cash.ApplyLayer(lid, bid, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)

	pending := mtxs[1:]
	// cache can only accommodate maxTXsPerAcct nonce
	for i := 0; i < maxTXsPerAcct; i++ {
		newNextNonce++
		newBalance -= pending[i].Spending()
	}
	checkProjection(t, cash, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(pending[:maxTXsPerAcct])}
	checkMempool(t, cash, expectedMempool)
	require.True(t, cash.MoreInDB(ta.principal))
	checkTXStateFromDB(t, cash.db, mtxs[:1], types.APPLIED)
	checkTXStateFromDB(t, cash.db, pending, types.MEMPOOL)
}

func TestCache_Account_BalanceRelaxedAfterApply(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, defaultAmount, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	saveTXs(t, cash.db, []*types.MeshTransaction{mtx})
	newNextNonce, newBalance := buildSingleAccountCache(t, cash, ta, []*types.MeshTransaction{mtx})

	pending := genTXs(t, ta.signer, ta.nonce+1, ta.nonce+4)
	largeAmount := defaultBalance
	for _, p := range pending {
		p.MaxSpend = largeAmount
		require.NoError(t, transactions.Add(cash.db, &p.Transaction, p.Received))
		require.Error(t, cash.Add(&p.Transaction, p.Received, nil))
		checkNoTX(t, cash, p.ID)
	}
	checkProjection(t, cash, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(mtx)}}
	checkMempool(t, cash, expectedMempool)

	// apply lid
	// there is also an incoming fund of `income` to the principal's account, which will make
	// transactions in `pending` feasible now
	income := defaultBalance * 100
	ta.nonce++
	ta.balance = ta.balance - mtx.Spending() + income
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(cash.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}
	applied := makeResults(lid, bid, mtx.Transaction)
	warns, errs := cash.ApplyLayer(lid, bid, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)
	// all pending txs are added to cache now
	newNextNonce = ta.nonce + uint64(len(pending))
	newBalance = ta.balance
	for _, p := range pending {
		newBalance -= p.Spending()
	}
	checkProjection(t, cash, ta.principal, newNextNonce, newBalance)
	expectedMempool = map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(pending)}
	checkMempool(t, cash, expectedMempool)
	checkTXStateFromDB(t, cash.db, []*types.MeshTransaction{mtx}, types.APPLIED)
	checkTXStateFromDB(t, cash.db, pending, types.MEMPOOL)
}

func TestCache_Account_BalanceRelaxedAfterApply_EvictLaterNonce(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	mtxs := genAndSaveTXs(t, cash.db, ta.signer, ta.nonce, ta.nonce+4)
	newNextNonce, newBalance := buildSingleAccountCache(t, cash, ta, mtxs)

	higherFee := defaultFee + 1
	largeAmount := defaultBalance - higherFee*defaultGas
	better := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce+1, largeAmount, higherFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, transactions.Add(cash.db, &better.Transaction, better.Received))

	require.ErrorIs(t, cash.Add(&better.Transaction, better.Received, nil), errInsufficientBalance)
	checkNoTX(t, cash, better.ID)
	checkProjection(t, cash, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, cash, expectedMempool)
	checkTXStateFromDB(t, cash.db, append(mtxs, better), types.MEMPOOL)

	// apply lid
	// there is also an incoming fund of `income` to the principal's account
	// the income is just enough to allow `better` to be feasible
	income := mtxs[0].Spending()
	ta.nonce++
	ta.balance = ta.balance - mtxs[0].Spending() + income
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(cash.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}
	applied := makeResults(lid, bid, mtxs[0].Transaction)
	warns, errs := cash.ApplyLayer(lid, bid, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)
	checkProjection(t, cash, ta.principal, ta.nonce+1, 0)
	expectedMempool = map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(better)}}
	checkMempool(t, cash, expectedMempool)
	checkTXStateFromDB(t, cash.db, mtxs[:1], types.APPLIED)
	checkTXStateFromDB(t, cash.db, append(mtxs[1:], better), types.MEMPOOL)
}

func TestCache_Account_EvictedAfterApply(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, defaultAmount, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, transactions.Add(cash.db, &mtx.Transaction, mtx.Received))
	newNextNonce, newBalance := buildSingleAccountCache(t, cash, ta, []*types.MeshTransaction{mtx})

	ta.nonce++
	ta.balance = ta.balance - mtx.Spending()
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(cash.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}
	applied := makeResults(lid, bid, mtx.Transaction)
	warns, errs := cash.ApplyLayer(lid, bid, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)
	checkProjection(t, cash, ta.principal, newNextNonce, newBalance)
	checkMempool(t, cash, nil)
	require.False(t, cash.MoreInDB(ta.principal))
	checkTXStateFromDB(t, cash.db, []*types.MeshTransaction{mtx}, types.APPLIED)
}

func TestCache_Account_NotEvictedAfterApplyDueToNonceGap(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, defaultAmount, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, transactions.Add(cash.db, &mtx.Transaction, mtx.Received))
	newNextNonce, newBalance := buildSingleAccountCache(t, cash, ta, []*types.MeshTransaction{mtx})

	ta.nonce++
	ta.balance = ta.balance - mtx.Spending()
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(cash.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}
	applied := makeResults(lid, bid, mtx.Transaction)
	pendingWithGap := genAndSaveTXs(t, cash.db, ta.signer, mtx.Nonce.Counter+2, mtx.Nonce.Counter+3)
	warns, errs := cash.ApplyLayer(lid, bid, applied)
	require.Empty(t, warns)
	require.Empty(t, errs)
	checkProjection(t, cash, ta.principal, newNextNonce, newBalance)
	checkMempool(t, cash, nil)
	require.True(t, cash.MoreInDB(ta.principal))
	checkTXStateFromDB(t, cash.db, []*types.MeshTransaction{mtx}, types.APPLIED)
	checkTXStateFromDB(t, cash.db, pendingWithGap, types.MEMPOOL)
}

func TestCache_Account_TXsAppliedOutOfOrder(t *testing.T) {
	cash, ta := createSingleAccountTestCache(t)
	mtxs := genAndSaveTXs(t, cash.db, ta.signer, ta.nonce, ta.nonce+1)
	newNextNonce, newBalance := buildSingleAccountCache(t, cash, ta, mtxs)

	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(cash.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}
	applied := makeResults(lid, bid, mtxs[1].Transaction)
	warns, errs := cash.ApplyLayer(lid, bid, applied)
	require.NotEmpty(t, warns)
	require.ErrorIs(t, warns[0], errNonceNotInOrder)
	require.Empty(t, errs)
	checkProjection(t, cash, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, cash, expectedMempool)
	checkTXStateFromDB(t, cash.db, mtxs, types.MEMPOOL)
}

func TestCache_BuildFromScratch(t *testing.T) {
	cash, accounts := createCache(t, 1000)
	mtxs := make(map[types.Address][]*types.MeshTransaction)
	totalNumTXs := 0
	for principal, ta := range accounts {
		numTXs := uint64(rand.Intn(100))
		if numTXs == 0 {
			continue
		}
		minBalance := numTXs * (defaultAmount + defaultFee*defaultGas)
		if ta.balance < minBalance {
			ta.balance = minBalance
		}
		mtxs[principal] = genAndSaveTXs(t, cash.db, ta.signer, ta.nonce, ta.nonce+numTXs-1)
		totalNumTXs += int(numTXs)
	}
	buildCache(t, cash, accounts, mtxs, totalNumTXs)
}

func TestCache_BuildFromScratch_AllHaveTooManyNonce_OK(t *testing.T) {
	numAccounts := 10
	cash, accounts := createCache(t, 10)
	// create too many nonce for each account
	numTXsEach := maxTXsPerAcct + 1
	totalNumTXs := numAccounts * numTXsEach
	byAddrAndNonce := make(map[types.Address][]*types.MeshTransaction)
	for principal, ta := range accounts {
		minBalance := uint64(numTXsEach) * (defaultAmount + defaultFee)
		if ta.balance < minBalance {
			ta.balance = minBalance
		}
		byAddrAndNonce[principal] = genAndSaveTXs(t, cash.db, ta.signer, ta.nonce, ta.nonce+uint64(numTXsEach)-1)
	}
	buildCache(t, cash, accounts, byAddrAndNonce, totalNumTXs)
	for principal := range accounts {
		require.True(t, cash.MoreInDB(principal))
	}
}

func TestCache_Add(t *testing.T) {
	cash, accounts := createCache(t, 1000)
	buildCache(t, cash, accounts, nil, 0)

	expectedMempool := make(map[types.Address][]*txtypes.NanoTX)
	for principal, ta := range accounts {
		numTXs := uint64(rand.Intn(100))
		if numTXs == 0 {
			continue
		}
		minBalance := numTXs * (defaultAmount + defaultFee)
		if ta.balance < minBalance {
			ta.balance = minBalance
		}
		mtxs := genAndSaveTXs(t, cash.db, ta.signer, ta.nonce, ta.nonce+numTXs-1)

		newNextNonce := ta.nonce + uint64(len(mtxs))
		newBalance := ta.balance
		for _, mtx := range mtxs {
			require.NoError(t, cash.Add(&mtx.Transaction, mtx.Received, nil))
			checkTX(t, cash, mtx)
			newBalance -= mtx.Spending()
		}
		checkProjection(t, cash, principal, newNextNonce, newBalance)
		expectedMempool[principal] = toNanoTXs(mtxs)
	}
	checkMempool(t, cash, expectedMempool)
}

func buildSmallCache(t *testing.T, cache *cache, accounts map[types.Address]*testAcct, maxTX int) map[types.Address][]*types.MeshTransaction {
	t.Helper()
	mtxsByAccount := make(map[types.Address][]*types.MeshTransaction)
	totalNumTXs := 0
	for principal, ta := range accounts {
		numTXs := uint64(rand.Intn(maxTX))
		if numTXs == 0 {
			continue
		}
		minBalance := numTXs * (defaultAmount + defaultFee)
		if ta.balance < minBalance {
			ta.balance = minBalance
		}
		mtxsByAccount[principal] = genAndSaveTXs(t, cache.db, ta.signer, ta.nonce, ta.nonce+numTXs-1)
		totalNumTXs += int(numTXs)
	}
	buildCache(t, cache, accounts, mtxsByAccount, totalNumTXs)
	for _, mtxs := range mtxsByAccount {
		checkTXStateFromDB(t, cache.db, mtxs, types.MEMPOOL)
	}
	return mtxsByAccount
}

func checkMempoolSize(t *testing.T, c *cache, expected int) {
	t.Helper()
	mempool := c.GetMempool()
	numTXs := 0
	for _, ntxs := range mempool {
		numTXs += len(ntxs)
	}
	require.Equal(t, expected, numTXs)
}

func TestCache_LinkTXsWithProposal(t *testing.T) {
	cash, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, cash, accounts, 10)
	lid0 := types.NewLayerID(97)
	pid0 := types.ProposalID{1, 2, 3}
	// take the first tx out of each account for proposal 0
	txs0 := make([]*types.MeshTransaction, 0, len(mtxsByAccount))
	tids0 := make([]types.TransactionID, 0, len(mtxsByAccount))
	totalNumTXs := 0
	for _, mtxs := range mtxsByAccount {
		totalNumTXs += len(mtxs)
		txs0 = append(txs0, mtxs[0])
		tids0 = append(tids0, mtxs[0].ID)
		mtxs[0].LayerID = lid0
	}
	require.NoError(t, cash.LinkTXsWithProposal(lid0, pid0, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, cash, mtxs[0])
	}
	checkTXStateFromDB(t, cash.db, txs0, types.PROPOSAL)

	lid1 := lid0.Add(1)
	pid1 := types.ProposalID{2, 3, 4}
	// take the second tx out of each account for proposal 1
	txs1 := make([]*types.MeshTransaction, 0, len(mtxsByAccount))
	tids1 := make([]types.TransactionID, 0, len(mtxsByAccount))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) > 1 {
			txs1 = append(txs1, mtxs[1])
			tids1 = append(tids1, mtxs[1].ID)
			mtxs[1].LayerID = lid1
		}
	}
	require.NoError(t, cash.LinkTXsWithProposal(lid1, pid1, tids1))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) > 1 {
			checkTX(t, cash, mtxs[1])
		}
	}
	checkTXStateFromDB(t, cash.db, txs1, types.PROPOSAL)
	checkMempoolSize(t, cash, totalNumTXs-len(txs0)-len(tids1))
}

func TestCache_LinkTXsWithProposal_MultipleLayers(t *testing.T) {
	cash, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, cash, accounts, 10)
	lid0 := types.NewLayerID(97)
	pid0 := types.ProposalID{1, 2, 3}
	// take the first tx out of each account for proposal 0
	txs0 := make([]*types.MeshTransaction, 0, len(mtxsByAccount))
	tids0 := make([]types.TransactionID, 0, len(mtxsByAccount))
	totalNumTXs := 0
	for _, mtxs := range mtxsByAccount {
		totalNumTXs += len(mtxs)
		txs0 = append(txs0, mtxs[0])
		tids0 = append(tids0, mtxs[0].ID)
		mtxs[0].LayerID = lid0
	}
	require.NoError(t, cash.LinkTXsWithProposal(lid0, pid0, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, cash, mtxs[0])
	}
	checkTXStateFromDB(t, cash.db, txs0, types.PROPOSAL)

	lid1 := lid0.Add(1)
	pid1 := types.ProposalID{2, 3, 4}
	// take the same set of txs in proposal 0
	require.NoError(t, cash.LinkTXsWithProposal(lid1, pid1, tids0))
	for _, mtxs := range mtxsByAccount {
		// all txs should still be at lid0
		checkTX(t, cash, mtxs[0])
	}
	checkMempoolSize(t, cash, totalNumTXs-len(tids0))
	checkTXStateFromDB(t, cash.db, txs0, types.PROPOSAL)
}

func TestCache_LinkTXsWithBlock(t *testing.T) {
	cash, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, cash, accounts, 10)
	lid0 := types.NewLayerID(97)
	bid0 := types.BlockID{1, 2, 3}
	// take the first tx out of each account for block 0
	txs0 := make([]*types.MeshTransaction, 0, len(mtxsByAccount))
	tids0 := make([]types.TransactionID, 0, len(mtxsByAccount))
	totalNumTXs := 0
	for _, mtxs := range mtxsByAccount {
		totalNumTXs += len(mtxs)
		txs0 = append(txs0, mtxs[0])
		tids0 = append(tids0, mtxs[0].ID)
		mtxs[0].LayerID = lid0
		mtxs[0].BlockID = bid0
	}
	require.NoError(t, cash.LinkTXsWithBlock(lid0, bid0, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, cash, mtxs[0])
	}
	checkTXStateFromDB(t, cash.db, txs0, types.BLOCK)

	lid1 := lid0.Add(1)
	bid1 := types.BlockID{2, 3, 4}
	// take the second tx out of each account for block 1
	txs1 := make([]*types.MeshTransaction, 0, len(mtxsByAccount))
	tids1 := make([]types.TransactionID, 0, len(mtxsByAccount))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) > 1 {
			txs1 = append(txs1, mtxs[1])
			tids1 = append(tids1, mtxs[1].ID)
			mtxs[1].LayerID = lid1
			mtxs[1].BlockID = bid1
		}
	}
	require.NoError(t, cash.LinkTXsWithBlock(lid1, bid1, tids1))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) > 1 {
			checkTX(t, cash, mtxs[1])
		}
	}
	checkTXStateFromDB(t, cash.db, txs1, types.BLOCK)
	checkMempoolSize(t, cash, totalNumTXs-len(tids0)-len(tids1))
}

func TestCache_LinkTXsWithBlock_MultipleLayers(t *testing.T) {
	cash, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, cash, accounts, 10)
	lid0 := types.NewLayerID(97)
	bid0 := types.BlockID{1, 2, 3}
	// take the first tx out of each account for block 0
	txs0 := make([]*types.MeshTransaction, 0, len(mtxsByAccount))
	tids0 := make([]types.TransactionID, 0, len(mtxsByAccount))
	totalNumTXs := 0
	for _, mtxs := range mtxsByAccount {
		totalNumTXs += len(mtxs)
		txs0 = append(txs0, mtxs[0])
		tids0 = append(tids0, mtxs[0].ID)
		mtxs[0].LayerID = lid0
		mtxs[0].BlockID = bid0
	}
	require.NoError(t, cash.LinkTXsWithBlock(lid0, bid0, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, cash, mtxs[0])
	}
	checkTXStateFromDB(t, cash.db, txs0, types.BLOCK)

	lid1 := lid0.Add(1)
	bid1 := types.BlockID{2, 3, 4}
	// take the same set of txs in block 0
	require.NoError(t, cash.LinkTXsWithBlock(lid1, bid1, tids0))
	for _, mtxs := range mtxsByAccount {
		// all txs should still be at lid0
		checkTX(t, cash, mtxs[0])
	}
	checkTXStateFromDB(t, cash.db, txs0, types.BLOCK)
	checkMempoolSize(t, cash, totalNumTXs-len(tids0))
}

func TestCache_ApplyLayerAndRevert(t *testing.T) {
	cash, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, cash, accounts, 10)
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(cash.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}
	allApplied := make([]types.TransactionWithResult, 0, len(mtxsByAccount)*2)
	appliedMTXs := make([]*types.MeshTransaction, 0, len(mtxsByAccount)*2)
	allPendingMTXs := make([]*types.MeshTransaction, 0, len(mtxsByAccount)*10)
	for principal, mtxs := range mtxsByAccount {
		lastNonce := mtxs[0].Nonce.Counter
		newBalance := accounts[principal].balance
		newBalance -= mtxs[0].Spending()
		applied := makeResults(lid, bid, mtxs[0].Transaction)
		appliedMTXs = append(appliedMTXs, mtxs[0])

		if len(mtxs) >= 2 {
			applied = append(applied, makeResults(lid, bid, mtxs[1].Transaction)...)
			appliedMTXs = append(appliedMTXs, mtxs[1])
			lastNonce = mtxs[1].Nonce.Counter
			newBalance -= mtxs[1].Spending()
			allPendingMTXs = append(allPendingMTXs, mtxs[2:]...)
		}
		// appliedByNonce := makeResultsByNonce(applied)
		// adjust state
		accounts[principal].nonce = lastNonce + 1
		accounts[principal].balance = newBalance
		allApplied = append(allApplied, applied...)
	}
	warns, errs := cash.ApplyLayer(lid, bid, allApplied)
	require.Empty(t, warns)
	require.Empty(t, errs)
	checkTXStateFromDB(t, cash.db, appliedMTXs, types.APPLIED)
	checkTXStateFromDB(t, cash.db, allPendingMTXs, types.MEMPOOL)

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
	require.NoError(t, cash.RevertToLayer(lid.Sub(1)))
	checkMempool(t, cash, expectedMempool)
	checkTXStateFromDB(t, cash.db, allPending, types.MEMPOOL)
}

func TestCache_ApplyLayer_OutOfOrder(t *testing.T) {
	cash, accounts := createCache(t, 100)
	buildSmallCache(t, cash, accounts, 10)
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(cash.db, lid.Sub(2), types.RandomBlockID()))
	warns, errs := cash.ApplyLayer(lid, types.BlockID{1, 2, 3}, nil)
	require.Empty(t, warns)
	require.NotEmpty(t, errs)
	require.ErrorIs(t, errs[0], errLayerNotInOrder)
}

func TestCache_GetMempool(t *testing.T) {
	cash, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, cash, accounts, 10)
	lid0 := types.NewLayerID(97)
	bid := types.BlockID{1, 2, 3}
	txs0 := make([]*types.MeshTransaction, 0, len(mtxsByAccount))
	tids0 := make([]types.TransactionID, 0, len(mtxsByAccount))
	for _, mtxs := range mtxsByAccount {
		txs0 = append(txs0, mtxs[0])
		tids0 = append(tids0, mtxs[0].ID)
		mtxs[0].LayerID = lid0
		mtxs[0].BlockID = bid
	}
	require.NoError(t, cash.LinkTXsWithBlock(lid0, bid, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, cash, mtxs[0])
	}
	checkTXStateFromDB(t, cash.db, txs0, types.BLOCK)

	// mark some txs with layer after the first empty layer
	lid1 := lid0.Add(1)
	pid := types.ProposalID{3, 4, 5}
	txs1 := make([]*types.MeshTransaction, 0, len(mtxsByAccount))
	tids1 := make([]types.TransactionID, 0, len(mtxsByAccount))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) >= 3 {
			txs1 = append(txs1, mtxs[2])
			tids1 = append(tids1, mtxs[2].ID)
			mtxs[2].LayerID = lid1
		}
	}
	require.NoError(t, cash.LinkTXsWithProposal(lid1, pid, tids1))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) >= 3 {
			checkTX(t, cash, mtxs[2])
		}
	}
	checkTXStateFromDB(t, cash.db, txs1, types.PROPOSAL)
	expectedMempool := make(map[types.Address][]*txtypes.NanoTX)
	for principal, mtxs := range mtxsByAccount {
		if len(mtxs) > 1 {
			expectedMempool[principal] = toNanoTXs(mtxs[1:])
		}
	}
	checkMempool(t, cash, expectedMempool)
}

func TestCache_GetProjection(t *testing.T) {
	cash, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, cash, accounts, 10)
	for principal, mtxs := range mtxsByAccount {
		expectedNonce := accounts[principal].nonce + uint64(len(mtxs))
		expectedBalance := accounts[principal].balance
		for _, mtx := range mtxs {
			expectedBalance -= mtx.Spending()
		}
		nonce, balance := cash.GetProjection(principal)
		require.Equal(t, expectedNonce, nonce)
		require.Equal(t, expectedBalance, balance)
	}
}
