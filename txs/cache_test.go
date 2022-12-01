package txs

import (
	"context"
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

type testCache struct {
	*cache
	db *sql.Database
}

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
		if st == nil {
			return 0, 0
		}
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

func genAndSaveTXs(t *testing.T, db *sql.Database, signer *signing.EdSigner, from, to uint64, startTime time.Time) []*types.MeshTransaction {
	t.Helper()
	mtxs := genTXs(t, signer, from, to, startTime)
	saveTXs(t, db, mtxs)
	return mtxs
}

func genTXs(t *testing.T, signer *signing.EdSigner, from, to uint64, startTime time.Time) []*types.MeshTransaction {
	t.Helper()
	mtxs := make([]*types.MeshTransaction, 0, int(to-from+1))
	for i := from; i <= to; i++ {
		mtx := newMeshTX(t, i, signer, defaultAmount, startTime.Add(time.Second*time.Duration(i)))
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

func checkTXNotInDB(t *testing.T, db *sql.Database, tid types.TransactionID) {
	_, err := transactions.Get(db, tid)
	require.ErrorIs(t, err, sql.ErrNotFound)
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
	mempool := c.GetMempool(c.logger)
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

func createCache(t *testing.T, numAccounts int) (*testCache, map[types.Address]*testAcct) {
	t.Helper()
	accounts := createState(t, numAccounts)
	db := sql.InMemory()
	return &testCache{
		cache: newCache(getStateFunc(accounts), logtest.New(t)),
		db:    db,
	}, accounts
}

func createSingleAccountTestCache(t *testing.T) (*testCache, *testAcct) {
	t.Helper()
	signer := signing.NewEdSigner()
	principal := types.GenerateAddress(signer.PublicKey().Bytes())
	ta := &testAcct{signer: signer, principal: principal, nonce: uint64(rand.Int63n(1000)), balance: defaultBalance}
	states := map[types.Address]*testAcct{principal: ta}
	db := sql.InMemory()
	return &testCache{
		cache: newCache(getStateFunc(states), logtest.New(t)),
		db:    db,
	}, ta
}

func buildCache(t *testing.T, tc *testCache, accounts map[types.Address]*testAcct, accountTXs map[types.Address][]*types.MeshTransaction) {
	t.Helper()
	for principal, ta := range accounts {
		if _, ok := accountTXs[principal]; ok {
			checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
		}
	}
	require.NoError(t, tc.cache.buildFromScratch(tc.db))

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

	require.NoError(t, tc.cache.buildFromScratch(tc.db))
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

	startTime := time.Now()
	mtxs := genAndSaveTXs(t, tc.db, ta.signer, ta.nonce, ta.nonce+4, startTime)
	sameNonces := genAndSaveTXs(t, tc.db, ta.signer, ta.nonce, ta.nonce+1, startTime.Add(time.Hour))
	oldNonces := genAndSaveTXs(t, tc.db, ta.signer, ta.nonce-2, ta.nonce-1, startTime.Add(time.Hour*2))
	newNextNonce := ta.nonce + uint64(len(mtxs))
	newBalance := ta.balance
	for _, mtx := range mtxs {
		newBalance -= mtx.Spending()
	}

	// build the cache from DB
	require.NoError(t, tc.buildFromScratch(tc.db))
	for _, mtx := range mtxs {
		checkTX(t, tc.cache, mtx)
	}
	for _, mtx := range append(oldNonces, sameNonces...) {
		checkNoTX(t, tc.cache, mtx.ID)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, tc.cache, expectedMempool)
	checkTXStateFromDB(t, tc.db, mtxs, types.MEMPOOL)
	checkTXStateFromDB(t, tc.db, oldNonces, types.MEMPOOL)
	checkTXStateFromDB(t, tc.db, sameNonces, types.MEMPOOL)

	// tx0 and tx1 got packed into a block
	// tx1 and tx2 got packed into a proposal
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(tc.db, lid.Sub(1), types.RandomBlockID()))
	pid := types.ProposalID{1, 2, 3}
	bid := types.BlockID{3, 2, 1}
	addedToBlock := []types.TransactionID{mtxs[0].ID, mtxs[1].ID}
	for _, mtx := range mtxs[:2] {
		mtx.LayerID = lid
		mtx.BlockID = bid
	}
	addedToProposal := []types.TransactionID{mtxs[1].ID, mtxs[2].ID}
	mtxs[2].LayerID = lid.Add(1)
	require.NoError(t, tc.LinkTXsWithBlock(tc.db, lid, bid, addedToBlock))
	require.NoError(t, tc.LinkTXsWithProposal(tc.db, lid.Add(1), pid, addedToProposal))
	for _, mtx := range mtxs {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	// mempool will only include transactions that are not in proposals/blocks
	expectedMempool = map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs[3:])}
	checkMempool(t, tc.cache, expectedMempool)
	checkTXStateFromDB(t, tc.db, mtxs[:2], types.MEMPOOL)
	checkTXStateFromDB(t, tc.db, mtxs[2:2], types.MEMPOOL)
	checkTXStateFromDB(t, tc.db, mtxs[3:], types.MEMPOOL)
	checkTXStateFromDB(t, tc.db, oldNonces, types.MEMPOOL)
	checkTXStateFromDB(t, tc.db, sameNonces, types.MEMPOOL)

	// the block with tx0 and tx1 is applied.
	// there is also an incoming fund of `income` to the principal's account
	income := defaultAmount * 100
	ta.nonce += 2
	for _, mtx := range mtxs[:2] {
		ta.balance -= mtx.Spending()
	}
	ta.balance += income
	applied := makeResults(lid, bid, mtxs[0].Transaction, mtxs[1].Transaction)
	require.NoError(t, tc.ApplyLayer(context.Background(), tc.db, lid, bid, applied, []types.Transaction{}))

	for _, mtx := range mtxs[:2] {
		checkNoTX(t, tc.cache, mtx.ID)
	}
	for _, mtx := range mtxs[2:] {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance+income)
	// mempool is unchanged
	checkMempool(t, tc.cache, expectedMempool)
	for _, mtx := range append(oldNonces, sameNonces...) {
		got, err := transactions.Get(tc.db, mtx.ID)
		require.NoError(t, err)
		require.Equal(t, types.MEMPOOL, got.State)
	}

	// revert to one layer before lid
	revertTo := lid.Sub(1)
	ta.nonce -= 2
	ta.balance = defaultBalance
	require.NoError(t, tc.RevertToLayer(tc.db, revertTo))
	for _, mtx := range mtxs {
		checkTX(t, tc.cache, mtx)
	}
	for _, mtx := range append(oldNonces, sameNonces...) {
		checkNoTX(t, tc.cache, mtx.ID)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	checkTXStateFromDB(t, tc.db, mtxs[:2], types.MEMPOOL)
	checkTXStateFromDB(t, tc.db, mtxs[2:2], types.MEMPOOL)
	checkTXStateFromDB(t, tc.db, mtxs[3:], types.MEMPOOL)
	checkTXStateFromDB(t, tc.db, oldNonces, types.MEMPOOL)
	checkTXStateFromDB(t, tc.db, sameNonces, types.MEMPOOL)
}

func TestCache_Account_TXInMultipleLayers(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtxs := genAndSaveTXs(t, tc.db, ta.signer, ta.nonce, ta.nonce+4, time.Now())
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, mtxs)

	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(tc.db, lid.Sub(1), types.RandomBlockID()))
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
	require.NoError(t, tc.LinkTXsWithBlock(tc.db, lid, bid0, addedToBlock0))
	require.NoError(t, tc.LinkTXsWithBlock(tc.db, lid, bid1, addedToBlock1))
	require.NoError(t, tc.LinkTXsWithProposal(tc.db, lid.Add(1), pid, addedToBlock1))
	for _, mtx := range mtxs {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)

	// mempool will only include transactions that are not in proposals/blocks
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs[2:])}
	checkMempool(t, tc.cache, expectedMempool)
	checkTXStateFromDB(t, tc.db, mtxs[:2], types.MEMPOOL)
	checkTXStateFromDB(t, tc.db, mtxs[2:], types.MEMPOOL)

	// block0 is applied.
	// there is also an incoming fund of `income` to the principal's account
	income := defaultAmount * 100
	ta.nonce++
	ta.balance = ta.balance - mtxs[0].Spending() + income
	applied := makeResults(lid, bid0, mtxs[0].Transaction)
	require.NoError(t, tc.ApplyLayer(context.Background(), tc.db, lid, bid0, applied, []types.Transaction{}))
	checkNoTX(t, tc.cache, mtxs[0].ID)
	mtxs[1].BlockID = types.EmptyBlockID
	mtxs[1].LayerID = lid.Add(1)
	for _, mtx := range mtxs[1:] {
		checkTX(t, tc.cache, mtx)
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance+income)
	// mempool is unchanged
	checkMempool(t, tc.cache, expectedMempool)
	checkTXStateFromDB(t, tc.db, mtxs[:1], types.APPLIED)
	checkTXStateFromDB(t, tc.db, mtxs[1:2], types.MEMPOOL)
	checkTXStateFromDB(t, tc.db, mtxs[2:], types.MEMPOOL)
}

func TestCache_Account_TooManyNonce(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	ta.balance = uint64(1000000)
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)

	mtxs := genAndSaveTXs(t, tc.db, ta.signer, ta.nonce, ta.nonce+maxTXsPerAcct, time.Now())
	require.NoError(t, tc.buildFromScratch(tc.db))
	require.True(t, tc.MoreInDB(ta.principal))
	last := len(mtxs) - 1
	for _, mtx := range mtxs[:last] {
		checkTX(t, tc.cache, mtx)
	}
	// the last one is not in the cache
	checkNoTX(t, tc.cache, mtxs[last].ID)

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

	now := time.Now()
	mtxs := make([]*types.MeshTransaction, 0, maxTXsPerNonce+1)
	for i := 0; i <= maxTXsPerNonce; i++ {
		mtx := newMeshTX(t, ta.nonce, ta.signer, defaultAmount, now.Add(time.Second*time.Duration(i)))
		mtx.GasPrice = defaultFee + uint64(i)
		mtx.MaxGas = 1
		mtxs = append(mtxs, mtx)
		require.NoError(t, transactions.Add(tc.db, &mtx.Transaction, mtx.Received))
	}
	require.NoError(t, tc.buildFromScratch(tc.db))
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

	mtxs := genAndSaveTXs(t, tc.db, ta.signer, ta.nonce-3, ta.nonce-1, time.Now())
	require.NoError(t, tc.buildFromScratch(tc.db))
	for _, mtx := range mtxs {
		checkNoTX(t, tc.cache, mtx.ID)
	}

	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.False(t, tc.MoreInDB(ta.principal))
}

func TestCache_Account_InsufficientBalance_AllPendingTXs(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)

	now := time.Now()
	mtxs := make([]*types.MeshTransaction, 0, 3)
	for i := 0; i <= 2; i++ {
		mtx := newMeshTX(t, ta.nonce+uint64(i), ta.signer, defaultAmount, now.Add(time.Second*time.Duration(i)))
		// make it so none of the txs is feasible
		mtx.MaxSpend = ta.balance
		mtxs = append(mtxs, mtx)
		require.NoError(t, transactions.Add(tc.db, &mtx.Transaction, mtx.Received))
	}

	require.NoError(t, tc.buildFromScratch(tc.db))
	for _, mtx := range mtxs {
		checkNoTX(t, tc.cache, mtx.ID)
	}

	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.True(t, tc.MoreInDB(ta.principal))
}

func TestCache_Account_Add_TooManyNonce_OK(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	ta.balance = uint64(1000000)
	mtxs := genAndSaveTXs(t, tc.db, ta.signer, ta.nonce, ta.nonce+maxTXsPerAcct-1, time.Now())
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, mtxs)

	oneTooMany := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce+maxTXsPerAcct, defaultAmount, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, tc.Add(context.TODO(), tc.db, &oneTooMany.Transaction, oneTooMany.Received, false))
	require.True(t, tc.MoreInDB(ta.principal))
	checkNoTX(t, tc.cache, oneTooMany.ID)
	checkTXStateFromDB(t, tc.db, append(mtxs, oneTooMany), types.MEMPOOL)

	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, tc.cache, expectedMempool)
}

func TestCache_Account_Add_SuperiorReplacesInferior(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	oldOne := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, defaultAmount, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, transactions.Add(tc.db, &oldOne.Transaction, oldOne.Received))
	buildSingleAccountCache(t, tc, ta, []*types.MeshTransaction{oldOne})

	// now add a superior tx
	higherFee := defaultFee + 1
	better := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, defaultAmount, higherFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, tc.Add(context.TODO(), tc.db, &better.Transaction, better.Received, false))
	checkTX(t, tc.cache, better)
	checkNoTX(t, tc.cache, oldOne.ID)
	checkProjection(t, tc.cache, ta.principal, ta.nonce+1, ta.balance-better.Spending())
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(better)}}
	checkMempool(t, tc.cache, expectedMempool)
	checkTXStateFromDB(t, tc.db, []*types.MeshTransaction{oldOne, better}, types.MEMPOOL)
}

func TestCache_Account_Add_SuperiorReplacesInferior_EvictLaterNonce(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtxs := genAndSaveTXs(t, tc.db, ta.signer, ta.nonce, ta.nonce+4, time.Now())
	buildSingleAccountCache(t, tc, ta, mtxs)

	// now add a tx at the next nonce that cause all later nonce transactions to be infeasible
	higherFee := defaultFee + 1
	bigAmount := ta.balance - higherFee*defaultGas
	better := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, bigAmount, higherFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, tc.Add(context.TODO(), tc.db, &better.Transaction, better.Received, false))
	checkTX(t, tc.cache, better)
	for _, mtx := range mtxs {
		checkNoTX(t, tc.cache, mtx.ID)
	}
	checkProjection(t, tc.cache, ta.principal, ta.nonce+1, 0)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(better)}}
	checkMempool(t, tc.cache, expectedMempool)
	require.True(t, tc.MoreInDB(ta.principal))
	checkTXStateFromDB(t, tc.db, append(mtxs, better), types.MEMPOOL)
}

func TestCache_Account_Add_UpdateHeader(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	buildSingleAccountCache(t, tc, ta, nil)

	tx := newTx(t, ta.nonce-1, defaultAmount, defaultFee, ta.signer)
	hdrless := *tx
	hdrless.TxHeader = nil

	// the hdrless tx is saved via syncing from blocks
	require.NoError(t, transactions.Add(tc.db, &hdrless, time.Now()))
	got, err := transactions.Get(tc.db, tx.ID)
	require.NoError(t, err)
	require.Nil(t, got.TxHeader)

	// update header and cache during execution
	require.ErrorIs(t, tc.Add(context.TODO(), tc.db, tx, time.Now(), true), errBadNonce)
	got, err = transactions.Get(tc.db, tx.ID)
	require.NoError(t, err)
	require.NotNil(t, got.TxHeader)
}

func TestCache_Account_Add_NonceTooSmall(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	buildSingleAccountCache(t, tc, ta, nil)

	tx := newTx(t, ta.nonce-1, defaultAmount, defaultFee, ta.signer)
	require.ErrorIs(t, tc.Add(context.TODO(), tc.db, tx, time.Now(), false), errBadNonce)
	checkNoTX(t, tc.cache, tx.ID)
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.False(t, tc.MoreInDB(ta.principal))
	checkTXNotInDB(t, tc.db, tx.ID)
}

func TestCache_Account_Add_RandomOrder(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	buildSingleAccountCache(t, tc, ta, nil)

	mtxs := genTXs(t, ta.signer, ta.nonce, ta.nonce+9, time.Now())
	sorted := make([]*types.MeshTransaction, len(mtxs))
	copy(sorted, mtxs)
	rand.Shuffle(len(sorted), func(i, j int) {
		sorted[i], sorted[j] = sorted[j], sorted[i]
	})
	for _, mtx := range sorted {
		require.NoError(t, tc.Add(context.TODO(), tc.db, &mtx.Transaction, mtx.Received, false))
	}
	newBalance := ta.balance
	for _, mtx := range mtxs {
		checkTX(t, tc.cache, mtx)
		newBalance -= mtx.Spending()
	}
	checkProjection(t, tc.cache, ta.principal, ta.nonce+uint64(len(mtxs)), newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, tc.cache, expectedMempool)
	checkTXStateFromDB(t, tc.db, mtxs, types.MEMPOOL)
}

func TestCache_Account_Add_InsufficientBalance_ResetAfterApply(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	buildSingleAccountCache(t, tc, ta, nil)

	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, ta.balance, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, tc.Add(context.TODO(), tc.db, &mtx.Transaction, mtx.Received, false))
	checkNoTX(t, tc.cache, mtx.ID)
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.True(t, tc.MoreInDB(ta.principal))
	checkTXStateFromDB(t, tc.db, []*types.MeshTransaction{mtx}, types.MEMPOOL)

	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(tc.db, lid.Sub(1), types.RandomBlockID()))
	// the account will receive funds in layer 97 (via rewards or incoming transfer)
	ta.balance += ta.balance
	require.NoError(t, tc.cache.ApplyLayer(context.TODO(), tc.db, lid, types.BlockID{1, 2, 3}, nil, nil))

	checkTX(t, tc.cache, mtx)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(mtx)}}
	checkMempool(t, tc.cache, expectedMempool)
	require.False(t, tc.MoreInDB(ta.principal))
	checkTXStateFromDB(t, tc.db, []*types.MeshTransaction{mtx}, types.MEMPOOL)
}

func TestCache_Account_Add_InsufficientBalance_HigherNonceFeasibleFirst(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	buildSingleAccountCache(t, tc, ta, nil)

	mtx0 := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, ta.balance*2, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	mtx1 := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce+10, ta.balance, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, tc.Add(context.TODO(), tc.db, &mtx0.Transaction, mtx0.Received, false))
	require.NoError(t, tc.Add(context.TODO(), tc.db, &mtx1.Transaction, mtx1.Received, false))
	checkNoTX(t, tc.cache, mtx0.ID)
	checkNoTX(t, tc.cache, mtx1.ID)
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.True(t, tc.MoreInDB(ta.principal))
	checkTXStateFromDB(t, tc.db, []*types.MeshTransaction{mtx0, mtx1}, types.MEMPOOL)

	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(tc.db, lid.Sub(1), types.RandomBlockID()))
	// the account receive enough funds in layer 97 (via rewards or incoming transfer) for mtx1
	ta.balance = mtx1.Spending()
	require.NoError(t, tc.cache.ApplyLayer(context.TODO(), tc.db, lid, types.BlockID{1, 2, 3}, nil, nil))
	checkNoTX(t, tc.cache, mtx0.ID)
	checkTX(t, tc.cache, mtx1)
	checkProjection(t, tc.cache, ta.principal, mtx1.Nonce.Counter+1, 0)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(mtx1)}}
	checkMempool(t, tc.cache, expectedMempool)
	require.True(t, tc.MoreInDB(ta.principal))
	checkTXStateFromDB(t, tc.db, []*types.MeshTransaction{mtx0, mtx1}, types.MEMPOOL)

	lid = lid.Add(1)
	require.NoError(t, layers.SetApplied(tc.db, lid.Sub(1), types.RandomBlockID()))
	// for some reasons this account wasn't applied in layer 98.
	// but the account receive enough funds in layer 98 (via rewards or incoming transfer) for both mtx0 and mtx1
	ta.balance = mtx0.Spending() + mtx1.Spending()
	require.NoError(t, tc.cache.ApplyLayer(context.TODO(), tc.db, lid, types.BlockID{2, 3, 4}, nil, nil))
	checkTX(t, tc.cache, mtx0)
	checkTX(t, tc.cache, mtx1)
	checkProjection(t, tc.cache, ta.principal, mtx1.Nonce.Counter+1, 0)
	expectedMempool = map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs([]*types.MeshTransaction{mtx0, mtx1})}
	checkMempool(t, tc.cache, expectedMempool)
	require.False(t, tc.MoreInDB(ta.principal))
	checkTXStateFromDB(t, tc.db, []*types.MeshTransaction{mtx0, mtx1}, types.MEMPOOL)
}

func TestCache_Account_Add_InsufficientBalance_NewNonce(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	buildSingleAccountCache(t, tc, ta, nil)

	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, defaultBalance, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, tc.Add(context.TODO(), tc.db, &mtx.Transaction, mtx.Received, false))
	checkNoTX(t, tc.cache, mtx.ID)
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	require.True(t, tc.MoreInDB(ta.principal))
	checkTXStateFromDB(t, tc.db, []*types.MeshTransaction{mtx}, types.MEMPOOL)
}

func TestCache_Account_Add_InsufficientBalance_ExistingNonce(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, defaultAmount, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, transactions.Add(tc.db, &mtx.Transaction, mtx.Received))
	buildSingleAccountCache(t, tc, ta, []*types.MeshTransaction{mtx})

	spender := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, ta.balance, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, tc.Add(context.TODO(), tc.db, &spender.Transaction, spender.Received, false))
	checkNoTX(t, tc.cache, spender.ID)
	checkProjection(t, tc.cache, ta.principal, ta.nonce+1, ta.balance-mtx.Spending())
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(mtx)}}
	checkMempool(t, tc.cache, expectedMempool)
	checkTXStateFromDB(t, tc.db, []*types.MeshTransaction{mtx, spender}, types.MEMPOOL)
}

func TestCache_Account_AppliedTXsNotInCache(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtxs := genTXs(t, ta.signer, ta.nonce, ta.nonce+2, time.Now())
	saveTXs(t, tc.db, mtxs[:1])
	// only add the first TX to cache
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, mtxs[:1])
	for _, mtx := range mtxs[1:] {
		checkNoTX(t, tc.cache, mtx.ID)
	}

	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(tc.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}

	applied := makeResults(lid, bid, mtxs[0].Transaction, mtxs[1].Transaction, mtxs[2].Transaction)
	// now the rest of the txs are fetched as part of a block
	saveTXs(t, tc.db, mtxs[1:])
	ta.nonce = newNextNonce + 2
	ta.balance = newBalance - mtxs[1].Spending() - mtxs[2].Spending()

	require.NoError(t, tc.ApplyLayer(context.Background(), tc.db, lid, bid, applied, []types.Transaction{}))
	checkProjection(t, tc.cache, ta.principal, ta.nonce, ta.balance)
	checkMempool(t, tc.cache, nil)
	checkTXStateFromDB(t, tc.db, mtxs, types.APPLIED)
}

func TestCache_Account_TooManyNonceAfterApply(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	ta.balance = uint64(1000000)
	mtxs := genTXs(t, ta.signer, ta.nonce, ta.nonce+maxTXsPerAcct+1, time.Now())
	saveTXs(t, tc.db, mtxs[:1])
	// build the cache with just one tx
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, mtxs[:1])

	ta.nonce = newNextNonce
	ta.balance = newBalance
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(tc.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}
	applied := makeResults(lid, bid, mtxs[0].Transaction)
	// more txs arrived
	saveTXs(t, tc.db, mtxs[1:])
	require.NoError(t, tc.ApplyLayer(context.Background(), tc.db, lid, bid, applied, []types.Transaction{}))

	pending := mtxs[1:]
	// cache can only accommodate maxTXsPerAcct nonce
	for i := 0; i < maxTXsPerAcct; i++ {
		newNextNonce++
		newBalance -= pending[i].Spending()
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(pending[:maxTXsPerAcct])}
	checkMempool(t, tc.cache, expectedMempool)
	require.True(t, tc.MoreInDB(ta.principal))
	checkTXStateFromDB(t, tc.db, mtxs[:1], types.APPLIED)
	checkTXStateFromDB(t, tc.db, pending, types.MEMPOOL)
}

func TestCache_Account_BalanceRelaxedAfterApply(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, defaultAmount, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	saveTXs(t, tc.db, []*types.MeshTransaction{mtx})
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, []*types.MeshTransaction{mtx})

	pending := genTXs(t, ta.signer, ta.nonce+1, ta.nonce+4, time.Now())
	largeAmount := defaultBalance
	for _, p := range pending {
		p.MaxSpend = largeAmount
		require.NoError(t, tc.Add(context.TODO(), tc.db, &p.Transaction, p.Received, false))
		checkNoTX(t, tc.cache, p.ID)
	}
	checkTXStateFromDB(t, tc.db, pending, types.MEMPOOL)
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(mtx)}}
	checkMempool(t, tc.cache, expectedMempool)

	// apply lid
	// there is also an incoming fund of `income` to the principal's account, which will make
	// transactions in `pending` feasible now
	income := defaultBalance * 100
	ta.nonce++
	ta.balance = ta.balance - mtx.Spending() + income
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(tc.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}
	applied := makeResults(lid, bid, mtx.Transaction)
	require.NoError(t, tc.ApplyLayer(context.Background(), tc.db, lid, bid, applied, []types.Transaction{}))
	// all pending txs are added to cache now
	newNextNonce = ta.nonce + uint64(len(pending))
	newBalance = ta.balance
	for _, p := range pending {
		newBalance -= p.Spending()
	}
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool = map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(pending)}
	checkMempool(t, tc.cache, expectedMempool)
	checkTXStateFromDB(t, tc.db, []*types.MeshTransaction{mtx}, types.APPLIED)
	checkTXStateFromDB(t, tc.db, pending, types.MEMPOOL)
}

func TestCache_Account_BalanceRelaxedAfterApply_EvictLaterNonce(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtxs := genAndSaveTXs(t, tc.db, ta.signer, ta.nonce, ta.nonce+4, time.Now())
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, mtxs)

	higherFee := defaultFee + 1
	largeAmount := defaultBalance - higherFee*defaultGas
	better := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce+1, largeAmount, higherFee, ta.signer),
		Received:    time.Now(),
	}

	require.NoError(t, tc.Add(context.TODO(), tc.db, &better.Transaction, better.Received, false))
	checkNoTX(t, tc.cache, better.ID)
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	expectedMempool := map[types.Address][]*txtypes.NanoTX{ta.principal: toNanoTXs(mtxs)}
	checkMempool(t, tc.cache, expectedMempool)
	checkTXStateFromDB(t, tc.db, append(mtxs, better), types.MEMPOOL)

	// apply lid
	// there is also an incoming fund of `income` to the principal's account
	// the income is just enough to allow `better` to be feasible
	income := mtxs[0].Spending()
	ta.nonce++
	ta.balance = ta.balance - mtxs[0].Spending() + income
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(tc.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}
	applied := makeResults(lid, bid, mtxs[0].Transaction)
	require.NoError(t, tc.ApplyLayer(context.Background(), tc.db, lid, bid, applied, []types.Transaction{}))
	checkProjection(t, tc.cache, ta.principal, ta.nonce+1, 0)
	expectedMempool = map[types.Address][]*txtypes.NanoTX{ta.principal: {txtypes.NewNanoTX(better)}}
	checkMempool(t, tc.cache, expectedMempool)
	checkTXStateFromDB(t, tc.db, mtxs[:1], types.APPLIED)
	checkTXStateFromDB(t, tc.db, append(mtxs[1:], better), types.MEMPOOL)
}

func TestCache_Account_EvictedAfterApply(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, defaultAmount, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, transactions.Add(tc.db, &mtx.Transaction, mtx.Received))
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, []*types.MeshTransaction{mtx})

	ta.nonce++
	ta.balance = ta.balance - mtx.Spending()
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(tc.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}
	applied := makeResults(lid, bid, mtx.Transaction)
	require.NoError(t, tc.ApplyLayer(context.Background(), tc.db, lid, bid, applied, []types.Transaction{}))
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	checkMempool(t, tc.cache, nil)
	require.False(t, tc.MoreInDB(ta.principal))
	checkTXStateFromDB(t, tc.db, []*types.MeshTransaction{mtx}, types.APPLIED)
}

func TestCache_Account_NotEvictedAfterApplyDueToNonceGap(t *testing.T) {
	tc, ta := createSingleAccountTestCache(t)
	mtx := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, defaultAmount, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	require.NoError(t, transactions.Add(tc.db, &mtx.Transaction, mtx.Received))
	newNextNonce, newBalance := buildSingleAccountCache(t, tc, ta, []*types.MeshTransaction{mtx})

	ta.nonce++
	ta.balance = ta.balance - mtx.Spending()
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(tc.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}
	applied := makeResults(lid, bid, mtx.Transaction)
	pendingInsufficient := &types.MeshTransaction{
		Transaction: *newTx(t, ta.nonce, ta.balance, defaultFee, ta.signer),
		Received:    time.Now(),
	}
	saveTXs(t, tc.db, []*types.MeshTransaction{pendingInsufficient})
	require.NoError(t, tc.ApplyLayer(context.Background(), tc.db, lid, bid, applied, []types.Transaction{}))
	checkProjection(t, tc.cache, ta.principal, newNextNonce, newBalance)
	checkMempool(t, tc.cache, nil)
	require.True(t, tc.MoreInDB(ta.principal))
	checkTXStateFromDB(t, tc.db, []*types.MeshTransaction{mtx}, types.APPLIED)
	checkTXStateFromDB(t, tc.db, []*types.MeshTransaction{pendingInsufficient}, types.MEMPOOL)
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
		minBalance := numTXs * (defaultAmount + defaultFee*defaultGas)
		if ta.balance < minBalance {
			ta.balance = minBalance
		}
		mtxs[principal] = genAndSaveTXs(t, tc.db, ta.signer, ta.nonce, ta.nonce+numTXs-1, time.Now())
		totalNumTXs += int(numTXs)
	}
	buildCache(t, tc, accounts, mtxs)
}

func TestCache_BuildFromScratch_AllHaveTooManyNonce_OK(t *testing.T) {
	tc, accounts := createCache(t, 10)
	// create too many nonce for each account
	numTXsEach := maxTXsPerAcct + 1
	byAddrAndNonce := make(map[types.Address][]*types.MeshTransaction)
	for principal, ta := range accounts {
		minBalance := uint64(numTXsEach) * (defaultAmount + defaultFee)
		if ta.balance < minBalance {
			ta.balance = minBalance
		}
		byAddrAndNonce[principal] = genAndSaveTXs(t, tc.db, ta.signer, ta.nonce, ta.nonce+uint64(numTXsEach)-1, time.Now())
	}
	buildCache(t, tc, accounts, byAddrAndNonce)
	for principal := range accounts {
		require.True(t, tc.MoreInDB(principal))
	}
}

func TestCache_Add(t *testing.T) {
	tc, accounts := createCache(t, 1000)
	buildCache(t, tc, accounts, nil)

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
		mtxs := genTXs(t, ta.signer, ta.nonce, ta.nonce+numTXs-1, time.Now())

		newNextNonce := ta.nonce + uint64(len(mtxs))
		newBalance := ta.balance
		for _, mtx := range mtxs {
			require.NoError(t, tc.Add(context.TODO(), tc.db, &mtx.Transaction, mtx.Received, false))
			checkTX(t, tc.cache, mtx)
			newBalance -= mtx.Spending()
		}
		checkProjection(t, tc.cache, principal, newNextNonce, newBalance)
		expectedMempool[principal] = toNanoTXs(mtxs)
		checkTXStateFromDB(t, tc.db, mtxs, types.MEMPOOL)
	}
	checkMempool(t, tc.cache, expectedMempool)
}

func buildSmallCache(t *testing.T, tc *testCache, accounts map[types.Address]*testAcct, maxTX int) map[types.Address][]*types.MeshTransaction {
	t.Helper()
	mtxsByAccount := make(map[types.Address][]*types.MeshTransaction)
	for principal, ta := range accounts {
		numTXs := uint64(rand.Intn(maxTX))
		if numTXs == 0 {
			continue
		}
		minBalance := numTXs * (defaultAmount + defaultFee)
		if ta.balance < minBalance {
			ta.balance = minBalance
		}
		mtxsByAccount[principal] = genAndSaveTXs(t, tc.db, ta.signer, ta.nonce, ta.nonce+numTXs-1, time.Now())
	}
	buildCache(t, tc, accounts, mtxsByAccount)
	for _, mtxs := range mtxsByAccount {
		checkTXStateFromDB(t, tc.db, mtxs, types.MEMPOOL)
	}
	return mtxsByAccount
}

func checkMempoolSize(t *testing.T, c *cache, expected int) {
	t.Helper()
	mempool := c.GetMempool(c.logger)
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
	txs0 := make([]*types.MeshTransaction, 0, len(mtxsByAccount))
	tids0 := make([]types.TransactionID, 0, len(mtxsByAccount))
	totalNumTXs := 0
	for _, mtxs := range mtxsByAccount {
		totalNumTXs += len(mtxs)
		txs0 = append(txs0, mtxs[0])
		tids0 = append(tids0, mtxs[0].ID)
		mtxs[0].LayerID = lid0
	}
	require.NoError(t, tc.LinkTXsWithProposal(tc.db, lid0, pid0, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, tc.cache, mtxs[0])
	}
	checkTXStateFromDB(t, tc.db, txs0, types.MEMPOOL)

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
	require.NoError(t, tc.LinkTXsWithProposal(tc.db, lid1, pid1, tids1))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) > 1 {
			checkTX(t, tc.cache, mtxs[1])
		}
	}
	checkTXStateFromDB(t, tc.db, txs1, types.MEMPOOL)
	checkMempoolSize(t, tc.cache, totalNumTXs-len(txs0)-len(tids1))
}

func TestCache_LinkTXsWithProposal_MultipleLayers(t *testing.T) {
	tc, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, tc, accounts, 10)
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
	require.NoError(t, tc.LinkTXsWithProposal(tc.db, lid0, pid0, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, tc.cache, mtxs[0])
	}
	checkTXStateFromDB(t, tc.db, txs0, types.MEMPOOL)

	lid1 := lid0.Add(1)
	pid1 := types.ProposalID{2, 3, 4}
	// take the same set of txs in proposal 0
	require.NoError(t, tc.LinkTXsWithProposal(tc.db, lid1, pid1, tids0))
	for _, mtxs := range mtxsByAccount {
		// all txs should still be at lid0
		checkTX(t, tc.cache, mtxs[0])
	}
	checkMempoolSize(t, tc.cache, totalNumTXs-len(tids0))
	checkTXStateFromDB(t, tc.db, txs0, types.MEMPOOL)
}

func TestCache_LinkTXsWithBlock(t *testing.T) {
	tc, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, tc, accounts, 10)
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
	require.NoError(t, tc.LinkTXsWithBlock(tc.db, lid0, bid0, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, tc.cache, mtxs[0])
	}
	checkTXStateFromDB(t, tc.db, txs0, types.MEMPOOL)

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
	require.NoError(t, tc.LinkTXsWithBlock(tc.db, lid1, bid1, tids1))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) > 1 {
			checkTX(t, tc.cache, mtxs[1])
		}
	}
	checkTXStateFromDB(t, tc.db, txs1, types.MEMPOOL)
	checkMempoolSize(t, tc.cache, totalNumTXs-len(tids0)-len(tids1))
}

func TestCache_LinkTXsWithBlock_MultipleLayers(t *testing.T) {
	tc, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, tc, accounts, 10)
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
	require.NoError(t, tc.LinkTXsWithBlock(tc.db, lid0, bid0, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, tc.cache, mtxs[0])
	}
	checkTXStateFromDB(t, tc.db, txs0, types.MEMPOOL)

	lid1 := lid0.Add(1)
	bid1 := types.BlockID{2, 3, 4}
	// take the same set of txs in block 0
	require.NoError(t, tc.LinkTXsWithBlock(tc.db, lid1, bid1, tids0))
	for _, mtxs := range mtxsByAccount {
		// all txs should still be at lid0
		checkTX(t, tc.cache, mtxs[0])
	}
	checkTXStateFromDB(t, tc.db, txs0, types.MEMPOOL)
	checkMempoolSize(t, tc.cache, totalNumTXs-len(tids0))
}

func TestCache_ApplyLayerAndRevert(t *testing.T) {
	tc, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, tc, accounts, 10)
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(tc.db, lid.Sub(1), types.RandomBlockID()))
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
		// adjust state
		accounts[principal].nonce = lastNonce + 1
		accounts[principal].balance = newBalance
		allApplied = append(allApplied, applied...)
	}
	require.NoError(t, tc.ApplyLayer(context.Background(), tc.db, lid, bid, allApplied, []types.Transaction{}))
	checkTXStateFromDB(t, tc.db, appliedMTXs, types.APPLIED)
	checkTXStateFromDB(t, tc.db, allPendingMTXs, types.MEMPOOL)

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
	require.NoError(t, tc.RevertToLayer(tc.db, lid.Sub(1)))
	checkMempool(t, tc.cache, expectedMempool)
	checkTXStateFromDB(t, tc.db, allPending, types.MEMPOOL)
}

func TestCache_ApplyLayerWithSkippedTXs(t *testing.T) {
	tc, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, tc, accounts, 10)
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(tc.db, lid.Sub(1), types.RandomBlockID()))
	bid := types.BlockID{1, 2, 3}
	var allSkipped []types.Transaction
	var addrs []types.Address
	allApplied := make([]types.TransactionWithResult, 0, len(mtxsByAccount)*2)
	appliedMTXs := make([]*types.MeshTransaction, 0, len(mtxsByAccount)*2)
	allPendingMTXs := make([]*types.MeshTransaction, 0, len(mtxsByAccount)*10)
	count := 0
	for principal, mtxs := range mtxsByAccount {
		lastNonce := mtxs[0].Nonce.Counter
		newBalance := accounts[principal].balance - mtxs[0].Spending()

		count++
		if count%10 == 0 {
			addrs = append(addrs, principal)
			allSkipped = append(allSkipped, mtxs[0].Transaction)
			// effectively make all pending txs invalid
			accounts[principal].nonce = mtxs[0].Nonce.Counter + uint64(len(mtxs))
		} else {
			applied := makeResults(lid, bid, mtxs[0].Transaction)
			allApplied = append(allApplied, applied...)
			allPendingMTXs = append(allPendingMTXs, mtxs[1:]...)
			appliedMTXs = append(appliedMTXs, mtxs[0])
			// adjust state
			accounts[principal].nonce = lastNonce + 1
			accounts[principal].balance = newBalance
		}
	}

	// create a new account that's not in cache
	signer := signing.NewEdSigner()
	skippedNotInCache := newTx(t, nonce, defaultAmount, defaultFee, signer)
	require.NoError(t, transactions.Add(tc.db, skippedNotInCache, time.Now()))
	allSkipped = append(allSkipped, *skippedNotInCache)

	require.NoError(t, tc.ApplyLayer(context.Background(), tc.db, lid, bid, allApplied, allSkipped))
	checkTXStateFromDB(t, tc.db, appliedMTXs, types.APPLIED)
	checkTXStateFromDB(t, tc.db, allPendingMTXs, types.MEMPOOL)
	for _, addr := range addrs {
		mtxs := mtxsByAccount[addr]
		for _, mtx := range mtxs {
			checkNoTX(t, tc.cache, mtx.ID)
		}
		require.False(t, tc.cache.MoreInDB(addr))
	}
	checkMempoolSize(t, tc.cache, len(allPendingMTXs))
}

func TestCache_ApplyLayer_OutOfOrder(t *testing.T) {
	tc, accounts := createCache(t, 100)
	buildSmallCache(t, tc, accounts, 10)
	lid := types.NewLayerID(97)
	require.NoError(t, layers.SetApplied(tc.db, lid.Sub(2), types.RandomBlockID()))
	err := tc.ApplyLayer(context.Background(), tc.db, lid, types.BlockID{1, 2, 3}, nil, []types.Transaction{})
	require.ErrorIs(t, err, errLayerNotInOrder)
}

func TestCache_GetMempool(t *testing.T) {
	tc, accounts := createCache(t, 100)
	mtxsByAccount := buildSmallCache(t, tc, accounts, 10)
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
	require.NoError(t, tc.LinkTXsWithBlock(tc.db, lid0, bid, tids0))
	for _, mtxs := range mtxsByAccount {
		checkTX(t, tc.cache, mtxs[0])
	}
	checkTXStateFromDB(t, tc.db, txs0, types.MEMPOOL)

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
	require.NoError(t, tc.LinkTXsWithProposal(tc.db, lid1, pid, tids1))
	for _, mtxs := range mtxsByAccount {
		if len(mtxs) >= 3 {
			checkTX(t, tc.cache, mtxs[2])
		}
	}
	checkTXStateFromDB(t, tc.db, txs1, types.MEMPOOL)
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
