package transactions

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/vm/transaction"
)

func createTX(t *testing.T, principal *signing.EdSigner, dest types.Address, nonce, amount, gas, fee uint64) *types.Transaction {
	t.Helper()
	tx, err := transaction.GenerateCallTransaction(principal, dest, nonce, amount, gas, fee)
	require.NoError(t, err)
	return tx
}

func packedInProposal(t *testing.T, db *sql.Database, tid types.TransactionID, lid types.LayerID, pid types.ProposalID, expectUpdated int) {
	t.Helper()
	dbtx, err := db.Tx(context.Background())
	require.NoError(t, err)
	defer dbtx.Release()

	require.NoError(t, AddToProposal(dbtx, tid, lid, pid))
	updated, err := UpdateIfBetter(dbtx, tid, lid, types.EmptyBlockID)
	require.NoError(t, err)
	require.Equal(t, expectUpdated, updated)
	require.NoError(t, dbtx.Commit())
}

func packedInBlock(t *testing.T, db *sql.Database, tid types.TransactionID, lid types.LayerID, bid types.BlockID, expectUpdated int) {
	t.Helper()
	dbtx, err := db.Tx(context.Background())
	require.NoError(t, err)
	defer dbtx.Release()

	require.NoError(t, AddToBlock(dbtx, tid, lid, bid))
	updated, err := UpdateIfBetter(dbtx, tid, lid, bid)
	require.NoError(t, err)
	require.Equal(t, expectUpdated, updated)
	require.NoError(t, dbtx.Commit())
}

func makeMeshTX(tx *types.Transaction, lid types.LayerID, bid types.BlockID, received time.Time, state types.TXState) *types.MeshTransaction {
	return &types.MeshTransaction{
		Transaction: *tx,
		LayerID:     lid,
		BlockID:     bid,
		Received:    received,
		State:       state,
	}
}

func getAndCheckMeshTX(t *testing.T, db sql.Executor, tid types.TransactionID, expected *types.MeshTransaction) {
	t.Helper()
	got, err := Get(db, tid)
	require.NoError(t, err)
	checkMeshTXEqual(t, *expected, *got)
}

func checkMeshTXEqual(t *testing.T, expected, got types.MeshTransaction) {
	t.Helper()
	require.EqualValues(t, expected.Received.UnixNano(), got.Received.UnixNano())
	got.Received = time.Time{}
	expected.Received = time.Time{}
	require.Equal(t, expected, got)
}

func TestAddGetHas(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer1 := signing.NewEdSignerFromRand(rng)
	signer2 := signing.NewEdSignerFromRand(rng)
	txs := []*types.Transaction{
		createTX(t, signer1, types.Address{1}, 1, 191, 1, 1),
		createTX(t, signer2, types.Address{2}, 1, 191, 1, 1),
		createTX(t, signer1, types.Address{3}, 1, 191, 1, 1),
	}

	received := time.Now()
	for _, tx := range txs {
		require.NoError(t, Add(db, tx, received))
	}

	for _, tx := range txs {
		got, err := Get(db, tx.ID())
		require.NoError(t, err)
		expected := makeMeshTX(tx, types.LayerID{}, types.EmptyBlockID, received, types.MEMPOOL)
		checkMeshTXEqual(t, *expected, *got)

		has, err := Has(db, tx.ID())
		require.NoError(t, err)
		require.True(t, has)
	}

	tid := types.RandomTransactionID()
	_, err := Get(db, tid)
	require.ErrorIs(t, err, sql.ErrNotFound)

	has, err := Has(db, tid)
	require.NoError(t, err)
	require.False(t, has)
}

func TestAddToProposal(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer := signing.NewEdSignerFromRand(rng)
	tx := createTX(t, signer, types.Address{1}, 1, 191, 1, 1)
	require.NoError(t, Add(db, tx, time.Now()))

	lid := types.NewLayerID(10)
	pid := types.ProposalID{1, 1}
	require.NoError(t, AddToProposal(db, tx.ID(), lid, pid))

	has, err := HasProposalTX(db, pid, tx.ID())
	require.NoError(t, err)
	require.True(t, has)

	has, err = HasProposalTX(db, types.ProposalID{2, 2}, tx.ID())
	require.NoError(t, err)
	require.False(t, has)
}

func TestAddToBlock(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer := signing.NewEdSignerFromRand(rng)
	tx := createTX(t, signer, types.Address{1}, 1, 191, 1, 1)
	require.NoError(t, Add(db, tx, time.Now()))

	lid := types.NewLayerID(10)
	bid := types.BlockID{1, 1}
	require.NoError(t, AddToBlock(db, tx.ID(), lid, bid))

	has, err := HasBlockTX(db, bid, tx.ID())
	require.NoError(t, err)
	require.True(t, has)

	has, err = HasBlockTX(db, types.BlockID{2, 2}, tx.ID())
	require.NoError(t, err)
	require.False(t, has)
}

func TestUpdateIfBetter(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer := signing.NewEdSignerFromRand(rng)
	tx := createTX(t, signer, types.Address{1}, 1, 191, 1, 1)
	received := time.Now()
	require.NoError(t, Add(db, tx, received))
	expected := makeMeshTX(tx, types.LayerID{}, types.EmptyBlockID, received, types.MEMPOOL)
	getAndCheckMeshTX(t, db, tx.ID(), expected)

	// tx is included in a proposal -> updated
	lid := types.NewLayerID(10)
	updated, err := UpdateIfBetter(db, tx.ID(), lid, types.EmptyBlockID)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
	expected = makeMeshTX(tx, lid, types.EmptyBlockID, received, types.PROPOSAL)
	getAndCheckMeshTX(t, db, tx.ID(), expected)

	// tx is included in another proposal at a later layer -> not updated
	updated, err = UpdateIfBetter(db, tx.ID(), lid.Add(1), types.EmptyBlockID)
	require.NoError(t, err)
	require.Equal(t, 0, updated)
	getAndCheckMeshTX(t, db, tx.ID(), expected)

	// tx is included in a proposal at an earlier layer -> updated
	lower := lid.Sub(1)
	updated, err = UpdateIfBetter(db, tx.ID(), lower, types.EmptyBlockID)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
	expected.LayerID = lower
	getAndCheckMeshTX(t, db, tx.ID(), expected)

	// tx is packed into a block of higher layer -> not updated
	bid0 := types.BlockID{2, 3, 4}
	updated, err = UpdateIfBetter(db, tx.ID(), lid, bid0)
	require.NoError(t, err)
	require.Equal(t, 0, updated)
	getAndCheckMeshTX(t, db, tx.ID(), expected)

	// tx is packed into a block in an earlier layer -> updated
	bid1 := types.BlockID{3, 4, 5}
	expected.State = types.BLOCK
	updated, err = UpdateIfBetter(db, tx.ID(), lower, bid1)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
	expected.BlockID = bid1
	getAndCheckMeshTX(t, db, tx.ID(), expected)

	// apply the tx -> updated
	updated, err = Apply(db, tx.ID(), lower, bid1)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
	expected.State = types.APPLIED
	getAndCheckMeshTX(t, db, tx.ID(), expected)

	// tx is packed into a block of even earlier layer -> not updated
	evenLower := lower.Sub(1)
	bid2 := types.BlockID{4, 5, 6}
	updated, err = UpdateIfBetter(db, tx.ID(), evenLower, bid2)
	require.NoError(t, err)
	require.Equal(t, 0, updated)
	getAndCheckMeshTX(t, db, tx.ID(), expected)
}

func TestApply_AlreadyApplied(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	lid := types.NewLayerID(10)
	signer := signing.NewEdSignerFromRand(rng)
	tx := createTX(t, signer, types.Address{1}, 1, 191, 1, 1)
	require.NoError(t, Add(db, tx, time.Now()))

	bid := types.RandomBlockID()
	updated, err := Apply(db, tx.ID(), lid, bid)
	require.NoError(t, err)
	require.Equal(t, 1, updated)

	// same block applied again
	updated, err = Apply(db, tx.ID(), lid, bid)
	require.NoError(t, err)
	require.Equal(t, 0, updated)

	// different block applied again
	updated, err = Apply(db, tx.ID(), lid.Add(1), types.RandomBlockID())
	require.NoError(t, err)
	require.Equal(t, 0, updated)
}

func TestUndoLayers_Empty(t *testing.T) {
	db := sql.InMemory()

	undone, err := UndoLayers(db, types.NewLayerID(199))
	require.NoError(t, err)
	require.Len(t, undone, 0)
}

func TestApplyAndUndoLayers(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	firstLayer := types.NewLayerID(10)
	numLayers := uint32(5)
	applied := make([]types.TransactionID, 0, numLayers)
	for lid := firstLayer; lid.Before(firstLayer.Add(numLayers)); lid = lid.Add(1) {
		signer := signing.NewEdSignerFromRand(rng)
		tx := createTX(t, signer, types.Address{1}, uint64(lid.Value), 191, 1, 1)
		require.NoError(t, Add(db, tx, time.Now()))
		bid := types.RandomBlockID()
		updated, err := Apply(db, tx.ID(), lid, bid)
		require.NoError(t, err)
		require.Equal(t, 1, updated)
		applied = append(applied, tx.ID())
	}

	for _, tid := range applied {
		mtx, err := Get(db, tid)
		require.NoError(t, err)
		require.Equal(t, types.APPLIED, mtx.State)
	}

	// revert to firstLayer
	numTXsUndone := int(numLayers - 1)
	undone, err := UndoLayers(db, firstLayer.Add(1))
	require.NoError(t, err)
	require.Len(t, undone, numTXsUndone)
	require.ElementsMatch(t, applied[1:], undone)

	for i, tid := range applied {
		mtx, err := Get(db, tid)
		require.NoError(t, err)
		if i == 0 {
			require.Equal(t, types.APPLIED, mtx.State)
		} else {
			require.Equal(t, types.MEMPOOL, mtx.State)
		}
	}
}

func TestDiscardNonceBelow(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer := signing.NewEdSignerFromRand(rng)
	principal := types.BytesToAddress(signer.PublicKey().Bytes())
	numTXs := 10
	received := time.Now()
	txs := make([]*types.Transaction, 0, numTXs*2)
	for nonce := uint64(0); nonce < uint64(numTXs); nonce++ {
		tx := createTX(t, signer, types.Address{1}, nonce, 191, 1, 1)
		require.NoError(t, Add(db, tx, received))
		txs = append(txs, tx)
	}

	// create tx for different accounts
	for i := 0; i < numTXs; i++ {
		s := signing.NewEdSignerFromRand(rng)
		tx := createTX(t, s, types.Address{1}, 1, 191, 1, 1)
		require.NoError(t, Add(db, tx, received))
		txs = append(txs, tx)
	}

	// apply nonce 0
	lid := types.NewLayerID(71)
	bid := types.RandomBlockID()
	updated, err := Apply(db, txs[0].ID(), lid, bid)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
	cutoff := uint64(numTXs) / 2
	require.NoError(t, DiscardNonceBelow(db, principal, cutoff))
	for _, tx := range txs {
		expected := makeMeshTX(tx, types.LayerID{}, types.EmptyBlockID, received, types.MEMPOOL)
		if tx.Origin() == principal {
			if tx.AccountNonce == 0 {
				expected = makeMeshTX(tx, lid, bid, received, types.APPLIED)
			} else if tx.AccountNonce < cutoff {
				expected.State = types.DISCARDED
			}
		}
		getAndCheckMeshTX(t, db, tx.ID(), expected)
	}
}

func TestDiscardByAcctNonce(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer := signing.NewEdSignerFromRand(rng)
	numTXs := 100
	sameNonceTXs := 10
	txs := make([]*types.Transaction, 0, 2*numTXs+sameNonceTXs)
	nonce := uint64(10000)
	received := time.Now()
	for i := 0; i < sameNonceTXs; i++ {
		tx := createTX(t, signer, types.Address{1}, nonce, 191, 1+uint64(i), 1)
		require.NoError(t, Add(db, tx, received))
		txs = append(txs, tx)
	}
	for i := 1; i <= numTXs/2; i++ {
		tx := createTX(t, signer, types.Address{1}, nonce-uint64(i), 191, 1, 1)
		require.NoError(t, Add(db, tx, received))
		txs = append(txs, tx)
		tx = createTX(t, signer, types.Address{1}, nonce+uint64(i), 191, 1, 1)
		require.NoError(t, Add(db, tx, received))
		txs = append(txs, tx)
	}

	// create tx for different accounts
	for i := 0; i < numTXs; i++ {
		s := signing.NewEdSignerFromRand(rng)
		tx := createTX(t, s, types.Address{1}, 1, 191, 1, 1)
		require.NoError(t, Add(db, tx, received))
		txs = append(txs, tx)
	}

	principal := types.BytesToAddress(signer.PublicKey().Bytes())
	applied := txs[0].ID()
	lid := types.NewLayerID(99)
	require.NoError(t, DiscardByAcctNonce(db, applied, lid, principal, nonce))
	for _, tx := range txs {
		mtx, err := Get(db, tx.ID())
		require.NoError(t, err)
		if tx.Origin() == principal && tx.AccountNonce == nonce && tx.ID() != applied {
			require.Equal(t, types.DISCARDED, mtx.State)
		} else {
			require.Equal(t, types.MEMPOOL, mtx.State)
		}
	}
}

func TestSetNextLayer(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer := signing.NewEdSignerFromRand(rng)
	tx := createTX(t, signer, types.Address{1}, 1, 191, 1, 1)
	received := time.Now()
	require.NoError(t, Add(db, tx, received))
	expected := makeMeshTX(tx, types.LayerID{}, types.EmptyBlockID, received, types.MEMPOOL)
	getAndCheckMeshTX(t, db, tx.ID(), expected)

	lid9 := types.NewLayerID(9)
	lid10 := types.NewLayerID(10)
	lid11 := types.NewLayerID(11)
	lid12 := types.NewLayerID(12)
	p9 := types.RandomProposalID()
	p10 := types.RandomProposalID()
	p11 := types.RandomProposalID()
	p12 := types.RandomProposalID()
	packedInProposal(t, db, tx.ID(), lid9, p9, 1)
	packedInProposal(t, db, tx.ID(), lid10, p10, 0)
	packedInProposal(t, db, tx.ID(), lid11, p11, 0)
	packedInProposal(t, db, tx.ID(), lid12, p12, 0)
	expected = makeMeshTX(tx, lid9, types.EmptyBlockID, received, types.PROPOSAL)
	getAndCheckMeshTX(t, db, tx.ID(), expected)

	b10 := types.RandomBlockID()
	b11 := types.RandomBlockID()
	packedInBlock(t, db, tx.ID(), lid10, b10, 0)
	packedInBlock(t, db, tx.ID(), lid11, b11, 0)
	expected = makeMeshTX(tx, lid9, types.EmptyBlockID, received, types.PROPOSAL)
	getAndCheckMeshTX(t, db, tx.ID(), expected)

	next, bid, err := SetNextLayer(db, tx.ID(), lid9)
	require.NoError(t, err)
	require.Equal(t, lid10, next)
	require.Equal(t, b10, bid)
	expected = makeMeshTX(tx, lid10, b10, received, types.BLOCK)
	getAndCheckMeshTX(t, db, tx.ID(), expected)

	next, bid, err = SetNextLayer(db, tx.ID(), types.NewLayerID(10))
	require.NoError(t, err)
	require.Equal(t, lid11, next)
	require.Equal(t, b11, bid)
	expected = makeMeshTX(tx, lid11, b11, received, types.BLOCK)
	getAndCheckMeshTX(t, db, tx.ID(), expected)

	next, bid, err = SetNextLayer(db, tx.ID(), types.NewLayerID(11))
	require.NoError(t, err)
	require.Equal(t, lid12, next)
	require.Equal(t, types.EmptyBlockID, bid)
	expected = makeMeshTX(tx, lid12, types.EmptyBlockID, received, types.PROPOSAL)
	getAndCheckMeshTX(t, db, tx.ID(), expected)

	next, bid, err = SetNextLayer(db, tx.ID(), types.NewLayerID(12))
	require.NoError(t, err)
	require.Equal(t, types.LayerID{}, next)
	require.Equal(t, types.EmptyBlockID, bid)
	expected = makeMeshTX(tx, types.LayerID{}, types.EmptyBlockID, received, types.MEMPOOL)
	getAndCheckMeshTX(t, db, tx.ID(), expected)
}

func TestGetBlob(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	numTXs := 5
	txs := make([]*types.Transaction, 0, numTXs)
	for i := 0; i < numTXs; i++ {
		tx := createTX(t, signing.NewEdSignerFromRand(rng), types.Address{1}, 1, 191, 1, 1)
		require.NoError(t, Add(db, tx, time.Now()))
		txs = append(txs, tx)
	}
	for _, tx := range txs {
		buf, err := GetBlob(db, tx.ID())
		require.NoError(t, err)
		encoded, err := codec.Encode(tx)
		require.NoError(t, err)
		require.Equal(t, encoded, buf)
	}
}

func TestGetByAddress(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer1 := signing.NewEdSignerFromRand(rng)
	signer2 := signing.NewEdSignerFromRand(rng)
	signer2Address := types.BytesToAddress(signer2.PublicKey().Bytes())
	lid := types.NewLayerID(10)
	pid := types.ProposalID{1, 1}
	bid := types.BlockID{2, 2}
	txs := []*types.Transaction{
		createTX(t, signer1, types.Address{1}, 1, 191, 1, 1),
		createTX(t, signer2, types.Address{2}, 1, 191, 1, 1),
		createTX(t, signer1, signer2Address, 1, 191, 1, 1),
	}
	received := time.Now()
	for _, tx := range txs {
		require.NoError(t, Add(db, tx, received))
		packedInProposal(t, db, tx.ID(), lid, pid, 1)
	}
	packedInBlock(t, db, txs[2].ID(), lid, bid, 1)

	// should be nothing before lid
	got, err := GetByAddress(db, types.NewLayerID(1), lid.Sub(1), signer2Address)
	require.NoError(t, err)
	require.Empty(t, got)

	got, err = GetByAddress(db, types.LayerID{}, lid, signer2Address)
	require.NoError(t, err)
	require.Len(t, got, 2)
	expected1 := makeMeshTX(txs[1], lid, types.EmptyBlockID, received, types.PROPOSAL)
	checkMeshTXEqual(t, *expected1, *got[0])
	expected2 := makeMeshTX(txs[2], lid, bid, received, types.BLOCK)
	checkMeshTXEqual(t, *expected2, *got[1])
}

func TestGetAllPending(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	numAccts := 100
	numTXs := 13
	received := time.Now()
	totalApplied := 0
	lid := types.NewLayerID(99)
	bid := types.BlockID{1, 2, 3}
	inBlock := make(map[types.TransactionID]*types.Transaction)
	inMempool := make(map[types.TransactionID]*types.Transaction)
	for i := 0; i < numAccts; i++ {
		signer := signing.NewEdSignerFromRand(rng)
		numApplied := rand.Intn(numTXs)
		for j := 0; j < numTXs; j++ {
			tx := createTX(t, signer, types.Address{1}, uint64(j), 191, 1, 1)
			require.NoError(t, Add(db, tx, received.Add(time.Duration(i+j))))
			// causing some txs to be applied, some packed in a block, and some in mempool
			if j < numApplied {
				updated, err := Apply(db, tx.ID(), lid, bid)
				require.NoError(t, err)
				require.Equal(t, updated, 1)
			} else if j < numTXs-1 {
				inBlock[tx.ID()] = tx
				updated, err := UpdateIfBetter(db, tx.ID(), lid, bid)
				require.NoError(t, err)
				require.Equal(t, updated, 1)
			} else {
				inMempool[tx.ID()] = tx
			}
		}
		totalApplied += numApplied
	}

	got, err := GetAllPending(db)
	require.NoError(t, err)
	require.Len(t, got, numTXs*numAccts-totalApplied)
	for _, mtx := range got {
		if _, ok := inBlock[mtx.ID()]; ok {
			require.Equal(t, types.BLOCK, mtx.State)
		} else if _, ok = inMempool[mtx.ID()]; ok {
			require.Equal(t, types.MEMPOOL, mtx.State)
		}
	}
}

func TestGetAcctPendingFromNonce(t *testing.T) {
	db := sql.InMemory()

	rng := rand.New(rand.NewSource(1001))
	signer := signing.NewEdSignerFromRand(rng)
	numTXs := 13
	nonce := uint64(987)
	received := time.Now()
	for i := 0; i < numTXs; i++ {
		tx := createTX(t, signer, types.Address{1}, nonce+uint64(i), 191, 1, 1)
		require.NoError(t, Add(db, tx, received.Add(time.Duration(i))))
		if i > 0 {
			tx = createTX(t, signer, types.Address{1}, nonce-uint64(i), 191, 1, 1)
			require.NoError(t, Add(db, tx, received.Add(time.Duration(i))))
		}
	}

	// create tx for different accounts
	for i := 0; i < numTXs; i++ {
		s := signing.NewEdSignerFromRand(rng)
		tx := createTX(t, s, types.Address{1}, 1, 191, 1, 1)
		require.NoError(t, Add(db, tx, received))
	}

	principal := types.BytesToAddress(signer.PublicKey().Bytes())
	for i := 0; i < numTXs; i++ {
		got, err := GetAcctPendingFromNonce(db, principal, nonce+uint64(i))
		require.NoError(t, err)
		require.Len(t, got, numTXs-i)
	}
}

func TestAppliedLayer(t *testing.T) {
	db := sql.InMemory()
	rng := rand.New(rand.NewSource(1001))
	signer := signing.NewEdSignerFromRand(rng)
	txs := []*types.Transaction{
		createTX(t, signer, types.Address{1}, 1, 191, 1, 1),
		createTX(t, signer, types.Address{1}, 2, 191, 1, 1),
	}
	lid := types.NewLayerID(10)

	for _, tx := range txs {
		require.NoError(t, Add(db, tx, time.Now()))
	}
	_, err := Apply(db, txs[0].ID(), lid, types.BlockID{1, 1})
	require.NoError(t, err)
	applied, err := GetAppliedLayer(db, txs[0].ID())
	require.NoError(t, err)
	require.Equal(t, lid, applied)

	_, err = GetAppliedLayer(db, txs[1].ID())
	require.ErrorIs(t, err, sql.ErrNotFound)

	undone, err := UndoLayers(db, lid)
	require.NoError(t, err)
	require.Len(t, undone, 1)
	require.Equal(t, txs[0].ID(), undone[0])
	_, err = GetAppliedLayer(db, txs[0].ID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}
