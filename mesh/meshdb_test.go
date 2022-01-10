package mesh

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"path"
	"sort"
	"testing"
	"time"

	"github.com/spacemeshos/ed25519"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	dbPath          = "../tmp/mdb"
	unitReward      = 10000
	unitLayerReward = 3000
)

func teardown() {
	_ = os.RemoveAll(dbPath)
}

func getMeshDB(tb testing.TB) *DB {
	tb.Helper()
	return NewMemMeshDB(logtest.New(tb))
}

func TestMeshDB_New(t *testing.T) {
	mdb := getMeshDB(t)
	defer mdb.Close()

	bl := types.GenLayerBlock(types.NewLayerID(1), types.RandomTXSet(10))
	err := mdb.AddBlock(bl)
	assert.NoError(t, err)
	block, err := mdb.GetBlock(bl.ID())
	assert.NoError(t, err)
	assert.True(t, bl.ID() == block.ID())
}

func TestMeshDB_AddBallot(t *testing.T) {
	mdb, err := NewPersistentMeshDB(t.TempDir(), 1, logtest.New(t))
	require.NoError(t, err)
	defer mdb.Close()

	layer := types.NewLayerID(123)
	ballot := types.GenLayerBallot(layer)
	require.False(t, mdb.HasBallot(ballot.ID()))

	require.NoError(t, mdb.AddBallot(ballot))
	assert.True(t, mdb.HasBallot(ballot.ID()))
	got, err := mdb.GetBallot(ballot.ID())
	require.NoError(t, err)
	assert.Equal(t, ballot, got)

	// copy the ballot and change it slightly
	ballotNew := *ballot
	ballotNew.LayerIndex = layer.Add(100)
	// the copied ballot still has the same BallotID
	require.Equal(t, ballot.ID(), ballotNew.ID())
	// this ballot ID already exist, will not overwrite the previous ballot
	require.NoError(t, mdb.AddBallot(&ballotNew))
	assert.True(t, mdb.HasBallot(ballot.ID()))
	gotNew, err := mdb.GetBallot(ballot.ID())
	require.NoError(t, err)
	assert.Equal(t, got, gotNew)

	ballots, err := mdb.LayerBallots(gotNew.LayerIndex)
	require.NoError(t, err)
	require.Len(t, ballots, 1)
	require.Equal(t, gotNew, ballots[0])
}

func TestMeshDB_AddBlock(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	mdb.blockCache = newBlockCache(1)
	defer mdb.Close()

	block := types.GenLayerBlock(types.NewLayerID(10), types.RandomTXSet(100))
	require.NoError(t, mdb.AddBlock(block))

	got, err := mdb.GetBlock(block.ID())
	require.NoError(t, err)
	assert.Equal(t, block, got)

	// add second block should cause the first block to be evicted
	require.NoError(t, mdb.AddBlock(types.GenLayerBlock(types.NewLayerID(10), types.RandomTXSet(100))))

	got2, err := mdb.GetBlock(block.ID())
	require.NoError(t, err)
	assert.Equal(t, got, got2)
}

func chooseRandomPattern(blocksInLayer int, patternSize int) []int {
	p := rand.Perm(blocksInLayer)
	indexes := make([]int, 0, patternSize)
	for _, r := range p[:patternSize] {
		indexes = append(indexes, r)
	}
	return indexes
}

func createLayerWithRandVoting(layerID types.LayerID, prev []*types.Layer, ballotsInLayer int, patternSize int, lg log.Log) *types.Layer {
	l := types.NewLayer(layerID)
	var patterns [][]int
	for _, l := range prev {
		blocks := l.Blocks()
		blocksInPrevLayer := len(blocks)
		patterns = append(patterns, chooseRandomPattern(blocksInPrevLayer, int(math.Min(float64(blocksInPrevLayer), float64(patternSize)))))
	}
	for i := 0; i < ballotsInLayer; i++ {
		ballot := types.RandomBallot()
		voted := make(map[types.BallotID]struct{})
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				ballot.Votes.Support = append(ballot.Votes.Support, b.ID())
				voted[ballot.ID()] = struct{}{}
			}
		}
		for _, prevBallot := range prev[0].Ballots() {
			if _, ok := voted[prevBallot.ID()]; !ok {
				ballot.Votes.Against = append(ballot.Votes.Against, prev[0].BlocksIDs()[0])
			}
		}
		ballot.LayerIndex = layerID
		l.AddBallot(ballot)
	}
	for i := 0; i < numBlocks; i++ {
		block := types.GenLayerBlock(layerID, types.RandomTXSet(1999999))
		l.AddBlock(block)
	}
	return l
}

func BenchmarkNewPersistentMeshDB(b *testing.B) {
	const batchSize = 50

	mdb, err := NewPersistentMeshDB(path.Join(dbPath, "mesh_db"), 5, logtest.New(b))
	require.NoError(b, err)
	defer mdb.Close()
	defer teardown()

	l := types.GenesisLayer()
	gen := l.Blocks()[0]

	err = mdb.AddBlock(gen)
	require.NoError(b, err)

	start := time.Now()
	lStart := time.Now()
	for i := 0; i < 10*batchSize; i++ {
		lyr := createLayerWithRandVoting(l.Index().Add(1), []*types.Layer{l}, 200, 20, logtest.New(b))
		for _, blk := range lyr.Blocks() {
			err := mdb.AddBlock(blk)
			require.NoError(b, err)
		}
		l = lyr
		if i%batchSize == batchSize-1 {
			b.Logf("layers %3d-%3d took %12v\t", i-(batchSize-1), i, time.Since(lStart))
			lStart = time.Now()
			for i := 0; i < 100; i++ {
				for _, blk := range lyr.Blocks() {
					block, err := mdb.getBlock(blk.ID())
					require.NoError(b, err)
					require.NotNil(b, block)
				}
			}
			b.Logf("reading last layer 100 times took %v\n", time.Since(lStart))
			lStart = time.Now()
		}
	}
	b.Logf("\n>>> Total time: %v\n\n", time.Since(start))
}

const (
	initialNonce   = 0
	initialBalance = 100
)

func address() types.Address {
	var addr [20]byte
	copy(addr[:], "12345678901234567890")
	return addr
}

func newTx(t *testing.T, signer *signing.EdSigner, nonce, totalAmount uint64) *types.Transaction {
	t.Helper()
	feeAmount := uint64(1)
	tx, err := types.NewSignedTx(nonce, types.Address{}, totalAmount-feeAmount, 3, feeAmount, signer)
	require.NoError(t, err)
	return tx
}

func newTxWithDest(t *testing.T, signer *signing.EdSigner, dest types.Address, nonce, totalAmount uint64) *types.Transaction {
	t.Helper()
	feeAmount := uint64(1)
	tx, err := types.NewSignedTx(nonce, dest, totalAmount-feeAmount, 3, feeAmount, signer)
	require.NoError(t, err)
	return tx
}

func newSignerAndAddress(t *testing.T, seedStr string) (*signing.EdSigner, types.Address) {
	t.Helper()
	seed := make([]byte, 32)
	copy(seed, seedStr)
	_, privKey, err := ed25519.GenerateKey(bytes.NewReader(seed))
	require.NoError(t, err)
	signer, err := signing.NewEdSignerFromBuffer(privKey)
	require.NoError(t, err)
	var addr types.Address
	addr.SetBytes(signer.PublicKey().Bytes())
	return signer, addr
}

func TestMeshDB_GetStateProjection(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()
	signer, origin := newSignerAndAddress(t, "123")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(t, signer, 0, 10),
		newTx(t, signer, 1, 20),
	}, types.NewLayerID(1))
	require.NoError(t, err)

	nonce, balance, err := mdb.GetProjection(origin, initialNonce, initialBalance)
	require.NoError(t, err)
	require.Equal(t, initialNonce+2, int(nonce))
	require.Equal(t, initialBalance-30, int(balance))
}

func TestMeshDB_GetStateProjection_WrongNonce(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	signer, origin := newSignerAndAddress(t, "123")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(t, signer, 1, 10),
		newTx(t, signer, 2, 20),
	}, types.NewLayerID(1))
	require.NoError(t, err)

	nonce, balance, err := mdb.GetProjection(origin, initialNonce, initialBalance)
	require.NoError(t, err)
	require.Equal(t, initialNonce, int(nonce))
	require.Equal(t, initialBalance, int(balance))
}

func TestMeshDB_GetStateProjection_DetectNegativeBalance(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	signer, origin := newSignerAndAddress(t, "123")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(t, signer, 0, 10),
		newTx(t, signer, 1, 95),
	}, types.NewLayerID(1))
	require.NoError(t, err)

	nonce, balance, err := mdb.GetProjection(origin, initialNonce, initialBalance)
	require.NoError(t, err)
	require.Equal(t, 1, int(nonce))
	require.Equal(t, initialBalance-10, int(balance))
}

func TestMeshDB_GetStateProjection_NothingToApply(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	nonce, balance, err := mdb.GetProjection(address(), initialNonce, initialBalance)
	require.NoError(t, err)
	require.Equal(t, uint64(initialNonce), nonce)
	require.Equal(t, uint64(initialBalance), balance)
}

func TestMeshDB_UnappliedTxs(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	signer1, origin1 := newSignerAndAddress(t, "thc")
	signer2, origin2 := newSignerAndAddress(t, "cbd")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(t, signer1, 420, 240),
		newTx(t, signer1, 421, 241),
		newTx(t, signer2, 0, 100),
		newTx(t, signer2, 1, 101),
	}, types.NewLayerID(1))
	require.NoError(t, err)

	txns1 := getTxns(t, mdb, origin1)
	require.Len(t, txns1, 2)
	require.Equal(t, 420, int(txns1[0].Nonce))
	require.Equal(t, 421, int(txns1[1].Nonce))
	require.Equal(t, 240, int(txns1[0].TotalAmount))
	require.Equal(t, 241, int(txns1[1].TotalAmount))

	txns2 := getTxns(t, mdb, origin2)
	require.Len(t, txns2, 2)
	require.Equal(t, 0, int(txns2[0].Nonce))
	require.Equal(t, 1, int(txns2[1].Nonce))
	require.Equal(t, 100, int(txns2[0].TotalAmount))
	require.Equal(t, 101, int(txns2[1].TotalAmount))

	mdb.removeFromUnappliedTxs([]*types.Transaction{
		newTx(t, signer2, 0, 100),
	})

	txns1 = getTxns(t, mdb, origin1)
	require.Len(t, txns1, 2)
	require.Equal(t, 420, int(txns1[0].Nonce))
	require.Equal(t, 421, int(txns1[1].Nonce))
	require.Equal(t, 240, int(txns1[0].TotalAmount))
	require.Equal(t, 241, int(txns1[1].TotalAmount))

	txns2 = getTxns(t, mdb, origin2)
	require.Len(t, txns2, 1)
	require.Equal(t, 1, int(txns2[0].Nonce))
	require.Equal(t, 101, int(txns2[0].TotalAmount))
}

func TestMeshDB_testGetTransactions(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	signer1, addr1 := newSignerAndAddress(t, "thc")
	signer2, _ := newSignerAndAddress(t, "cbd")
	_, addr3 := newSignerAndAddress(t, "cbe")
	require.NoError(t, mdb.writeTransactions(types.NewLayerID(1), types.EmptyBlockID,
		newTx(t, signer1, 420, 240),
		newTx(t, signer1, 421, 241),
		newTxWithDest(t, signer2, addr1, 0, 100),
		newTxWithDest(t, signer2, addr1, 1, 101),
	))

	txs, err := mdb.GetTransactionsByOrigin(types.NewLayerID(1), addr1)
	require.NoError(t, err)
	require.Equal(t, 2, len(txs))

	txs, err = mdb.GetTransactionsByDestination(types.NewLayerID(1), addr1)
	require.NoError(t, err)
	require.Equal(t, 2, len(txs))

	// test negative case
	txs, err = mdb.GetTransactionsByOrigin(types.NewLayerID(1), addr3)
	require.NoError(t, err)
	require.Equal(t, 0, len(txs))

	txs, err = mdb.GetTransactionsByDestination(types.NewLayerID(1), addr3)
	require.NoError(t, err)
	require.Equal(t, 0, len(txs))
}

func TestMeshDB_updateTransaction(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	signer1, _ := newSignerAndAddress(t, "thc")
	tx := newTx(t, signer1, 420, 240)
	layerID := types.NewLayerID(10)
	require.NoError(t, mdb.writeTransactions(layerID, types.EmptyBlockID, tx))

	got, err := mdb.GetMeshTransaction(tx.ID())
	require.NoError(t, err)
	assert.Equal(t, layerID, got.LayerID)
	assert.Equal(t, types.EmptyBlockID, got.BlockID)
	assert.Equal(t, tx.ID(), got.Transaction.ID()) // this cause got.Transaction() to populate id
	assert.Equal(t, *tx, got.Transaction)

	block := types.GenLayerBlock(layerID, nil)
	require.NoError(t, mdb.updateDBTXWithBlockID(block, tx))
	got, err = mdb.GetMeshTransaction(tx.ID())
	require.NoError(t, err)
	assert.Equal(t, layerID, got.LayerID)
	assert.Equal(t, block.ID(), got.BlockID)
	assert.Equal(t, tx.ID(), got.Transaction.ID()) // this cause got.Transaction() to populate id
	assert.Equal(t, *tx, got.Transaction)
}

type tinyTX struct {
	ID          types.TransactionID
	Nonce       uint64
	TotalAmount uint64
}

func getTxns(t *testing.T, mdb *DB, origin types.Address) []tinyTX {
	t.Helper()
	txns, err := mdb.getAccountPendingTxs(origin)
	require.NoError(t, err)
	var ret []tinyTX
	for nonce, nonceTxs := range txns.PendingTxs {
		for id, tx := range nonceTxs {
			ret = append(ret, tinyTX{ID: id, Nonce: nonce, TotalAmount: tx.Amount + tx.Fee})
		}
	}
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].Nonce < ret[j].Nonce
	})
	return ret
}

func writeRewards(t *testing.T, mdb *DB) ([]types.Address, []types.NodeID) {
	t.Helper()
	signer1, addr1 := newSignerAndAddress(t, "123")
	signer2, addr2 := newSignerAndAddress(t, "456")
	signer3, addr3 := newSignerAndAddress(t, "789")

	smesher1 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer1.PublicKey().Bytes(),
	}
	smesher2 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer2.PublicKey().Bytes(),
	}
	smesher3 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer3.PublicKey().Bytes(),
	}

	rewards1 := []types.AnyReward{
		{
			Address:     addr1,
			SmesherID:   smesher1,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
		{
			Address:     addr1,
			SmesherID:   smesher1,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
		{
			Address:     addr2,
			SmesherID:   smesher2,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
		{
			Address:     addr3,
			SmesherID:   smesher3,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
	}

	rewards2 := []types.AnyReward{
		{
			Address:     addr2,
			SmesherID:   smesher2,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
		{
			Address:     addr2,
			SmesherID:   smesher2,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
		{
			Address:     addr3,
			SmesherID:   smesher3,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
	}

	rewards3 := []types.AnyReward{
		{
			Address:     addr3,
			SmesherID:   smesher3,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
		{
			Address:     addr3,
			SmesherID:   smesher3,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
		{
			Address:     addr1,
			SmesherID:   smesher1,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
	}

	err := mdb.writeTransactionRewards(types.NewLayerID(1), rewards1)
	require.NoError(t, err)

	err = mdb.writeTransactionRewards(types.NewLayerID(2), rewards2)
	require.NoError(t, err)

	err = mdb.writeTransactionRewards(types.NewLayerID(3), rewards3)
	require.NoError(t, err)
	return []types.Address{addr1, addr2, addr3}, []types.NodeID{smesher1, smesher2, smesher3}
}

func TestMeshDB_testGetRewards(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	addrs, smeshers := writeRewards(t, mdb)
	rewards, err := mdb.GetRewards(addrs[0])
	require.NoError(t, err)
	require.Equal(t, []types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: unitReward * 2, LayerRewardEstimate: unitLayerReward * 2, SmesherID: smeshers[0], Coinbase: addrs[0]},
		{Layer: types.NewLayerID(3), TotalReward: unitReward, LayerRewardEstimate: unitLayerReward, SmesherID: smeshers[0], Coinbase: addrs[0]},
	}, rewards)

	rewards, err = mdb.GetRewards(addrs[1])
	require.NoError(t, err)
	require.Equal(t, []types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: unitReward, LayerRewardEstimate: unitLayerReward, SmesherID: smeshers[1], Coinbase: addrs[1]},
		{Layer: types.NewLayerID(2), TotalReward: unitReward * 2, LayerRewardEstimate: unitLayerReward * 2, SmesherID: smeshers[1], Coinbase: addrs[1]},
	}, rewards)

	rewards, err = mdb.GetRewards(addrs[2])
	require.NoError(t, err)
	require.Equal(t, []types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: unitReward, LayerRewardEstimate: unitLayerReward, SmesherID: smeshers[2], Coinbase: addrs[2]},
		{Layer: types.NewLayerID(2), TotalReward: unitReward, LayerRewardEstimate: unitLayerReward, SmesherID: smeshers[2], Coinbase: addrs[2]},
		{Layer: types.NewLayerID(3), TotalReward: unitReward * 2, LayerRewardEstimate: unitLayerReward * 2, SmesherID: smeshers[2], Coinbase: addrs[2]},
	}, rewards)

	_, addr4 := newSignerAndAddress(t, "999")
	rewards, err = mdb.GetRewards(addr4)
	require.NoError(t, err)
	require.Nil(t, rewards)
}

func TestMeshDB_testGetRewardsBySmesher(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	addrs, smeshers := writeRewards(t, mdb)

	rewards, err := mdb.GetRewardsBySmesherID(smeshers[0])
	require.NoError(t, err)
	require.Equal(t, []types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: unitReward * 2, LayerRewardEstimate: unitLayerReward * 2, SmesherID: smeshers[0], Coinbase: addrs[0]},
		{Layer: types.NewLayerID(3), TotalReward: unitReward, LayerRewardEstimate: unitLayerReward, SmesherID: smeshers[0], Coinbase: addrs[0]},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smeshers[1])
	require.NoError(t, err)
	require.Equal(t, []types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: unitReward, LayerRewardEstimate: unitLayerReward, SmesherID: smeshers[1], Coinbase: addrs[1]},
		{Layer: types.NewLayerID(2), TotalReward: unitReward * 2, LayerRewardEstimate: unitLayerReward * 2, SmesherID: smeshers[1], Coinbase: addrs[1]},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smeshers[2])
	require.NoError(t, err)
	require.Equal(t, []types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: unitReward, LayerRewardEstimate: unitLayerReward, SmesherID: smeshers[2], Coinbase: addrs[2]},
		{Layer: types.NewLayerID(2), TotalReward: unitReward, LayerRewardEstimate: unitLayerReward, SmesherID: smeshers[2], Coinbase: addrs[2]},
		{Layer: types.NewLayerID(3), TotalReward: unitReward * 2, LayerRewardEstimate: unitLayerReward * 2, SmesherID: smeshers[2], Coinbase: addrs[2]},
	}, rewards)

	signer4, _ := newSignerAndAddress(t, "999")
	smesher4 := types.NodeID{
		Key:          signer4.PublicKey().String(),
		VRFPublicKey: signer4.PublicKey().Bytes(),
	}
	rewards, err = mdb.GetRewardsBySmesherID(smesher4)
	require.NoError(t, err)
	require.Nil(t, rewards)
}

func TestMeshDB_RecordCoinFlip(t *testing.T) {
	layerID := types.NewLayerID(123)

	testCoinflip := func(mdb *DB) {
		_, exists := mdb.GetCoinflip(context.TODO(), layerID)
		require.False(t, exists, "coin value should not exist before being inserted")
		mdb.RecordCoinflip(context.TODO(), layerID, true)
		coin, exists := mdb.GetCoinflip(context.TODO(), layerID)
		require.True(t, exists, "expected coin value to exist")
		require.True(t, coin, "expected true coin value")
		mdb.RecordCoinflip(context.TODO(), layerID, false)
		coin, exists = mdb.GetCoinflip(context.TODO(), layerID)
		require.True(t, exists, "expected coin value to exist")
		require.False(t, coin, "expected false coin value on overwrite")
	}

	mdb1 := NewMemMeshDB(logtest.New(t))
	defer mdb1.Close()
	testCoinflip(mdb1)
	mdb2, err := NewPersistentMeshDB(dbPath+"/mesh_db/", 5, logtest.New(t))
	require.NoError(t, err)
	defer mdb2.Close()
	defer teardown()
	testCoinflip(mdb2)
}

func TestMeshDB_GetMeshTransactions(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	signer1, _ := newSignerAndAddress(t, "thc")

	var (
		nonce  uint64
		ids    []types.TransactionID
		layers = 10
	)
	for i := 1; i <= layers; i++ {
		nonce++
		tx := newTx(t, signer1, nonce, 240)
		ids = append(ids, tx.ID())
		require.NoError(t, mdb.writeTransactions(types.NewLayerID(uint32(i)), types.EmptyBlockID, tx))
	}
	txs, missing := mdb.GetMeshTransactions(ids)
	require.Len(t, missing, 0)
	for i := 1; i < layers; i++ {
		require.Equal(t, ids[i-1], txs[i-1].ID())
		require.EqualValues(t, types.NewLayerID(uint32(i)), txs[i-1].LayerID)
	}
}

func TestMesh_FindOnce(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	signer1, addr1 := newSignerAndAddress(t, "thc")
	signer2, _ := newSignerAndAddress(t, "cbd")

	layers := []uint32{1, 10, 100}
	nonce := uint64(0)
	for _, layer := range layers {
		nonce++
		err := mdb.writeTransactions(types.NewLayerID(layer), types.EmptyBlockID,
			newTx(t, signer1, nonce, 100),
			newTxWithDest(t, signer2, addr1, nonce, 100),
		)
		require.NoError(t, err)
	}
	t.Run("ByDestination", func(t *testing.T) {
		for _, layer := range layers {
			txs, err := mdb.GetTransactionsByDestination(types.NewLayerID(layer), addr1)
			require.NoError(t, err)
			assert.Len(t, txs, 1)
		}
	})

	t.Run("ByOrigin", func(t *testing.T) {
		for _, layer := range layers {
			txs, err := mdb.GetTransactionsByOrigin(types.NewLayerID(layer), addr1)
			require.NoError(t, err)
			assert.Len(t, txs, 1)
		}
	})
}

func TestBlocksBallotsOverlap(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	lid := []byte{'L', 0, 0, 0}
	block := types.NewExistingBlock(types.BlockID{0, 2, 3},
		types.InnerBlock{LayerIndex: types.NewLayerID(binary.LittleEndian.Uint32(lid))})
	require.NoError(t, mdb.AddBlock(block))

	ids, err := mdb.LayerBallotIDs(types.NewLayerID(0))
	require.NoError(t, err)
	require.Len(t, ids, 1)
}

func BenchmarkGetBlock(b *testing.B) {
	// cache is set to be twice as large as cache to avoid hitting the cache
	blocks := make([]*types.Block, layerSize*2)
	db, err := NewPersistentMeshDB(b.TempDir(),
		1, /*size of the cache is multiplied by a constant (layerSize). for the benchmark it needs to be no more than layerSize*/
		logtest.New(b))
	require.NoError(b, err)
	defer db.Close()

	for i := range blocks {
		blocks[i] = types.GenLayerBlock(types.NewLayerID(1), nil)
		require.NoError(b, db.AddBlock(blocks[i]))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		block := blocks[i%len(blocks)]
		_, err = db.GetBlock(block.ID())
		require.NoError(b, err)
	}
}
