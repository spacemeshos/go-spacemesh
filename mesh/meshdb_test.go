package mesh

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
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
	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	unitReward      = 10000
	unitLayerReward = 3000
)

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
	mdb, err := NewPersistentMeshDB(sql.InMemory(), logtest.New(t))
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
}

func TestMeshDB_AddBlock(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
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

	mdb, err := NewPersistentMeshDB(sql.InMemory(), logtest.New(b))
	require.NoError(b, err)
	defer mdb.Close()

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

func newSignerAndAddress(t *testing.T, seedStr string) (*signing.EdSigner, types.Address) {
	t.Helper()
	seed := make([]byte, 32)
	copy(seed, seedStr)
	_, privKey, err := ed25519.GenerateKey(bytes.NewReader(seed))
	require.NoError(t, err)
	signer, err := signing.NewEdSignerFromBuffer(privKey)
	require.NoError(t, err)
	return signer, types.GenerateAddress(signer.PublicKey().Bytes())
}

func writeRewards(t *testing.T, mdb *DB) ([]types.Address, []types.NodeID) {
	t.Helper()
	signer1, addr1 := newSignerAndAddress(t, "123")
	signer2, addr2 := newSignerAndAddress(t, "456")
	signer3, addr3 := newSignerAndAddress(t, "789")

	smesher1, err := types.BytesToNodeID(signer1.PublicKey().Bytes())
	require.NoError(t, err)
	smesher2, err := types.BytesToNodeID(signer2.PublicKey().Bytes())
	require.NoError(t, err)
	smesher3, err := types.BytesToNodeID(signer3.PublicKey().Bytes())
	require.NoError(t, err)

	rewards1 := []types.AnyReward{
		{
			Address:     addr1,
			SmesherID:   *smesher1,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
		{
			Address:     addr1,
			SmesherID:   *smesher1,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
		{
			Address:     addr2,
			SmesherID:   *smesher2,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
		{
			Address:     addr3,
			SmesherID:   *smesher3,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
	}

	rewards2 := []types.AnyReward{
		{
			Address:     addr2,
			SmesherID:   *smesher2,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
		{
			Address:     addr2,
			SmesherID:   *smesher2,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
		{
			Address:     addr3,
			SmesherID:   *smesher3,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
	}

	rewards3 := []types.AnyReward{
		{
			Address:     addr3,
			SmesherID:   *smesher3,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
		{
			Address:     addr3,
			SmesherID:   *smesher3,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
		{
			Address:     addr1,
			SmesherID:   *smesher1,
			Amount:      unitReward,
			LayerReward: unitLayerReward,
		},
	}

	require.NoError(t, mdb.writeTransactionRewards(types.NewLayerID(1), rewards1))
	require.NoError(t, mdb.writeTransactionRewards(types.NewLayerID(2), rewards2))
	require.NoError(t, mdb.writeTransactionRewards(types.NewLayerID(3), rewards3))

	return []types.Address{addr1, addr2, addr3}, []types.NodeID{*smesher1, *smesher2, *smesher3}
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
	mdb2, err := NewPersistentMeshDB(sql.InMemory(), logtest.New(t))
	require.NoError(t, err)
	defer mdb2.Close()
	testCoinflip(mdb2)
}

func TestBlocksBallotsOverlap(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	lid := []byte{'L', 0, 0, 0}
	bid := types.BlockID{1, 2, 3}
	block := types.NewExistingBlock(bid,
		types.InnerBlock{LayerIndex: types.NewLayerID(binary.LittleEndian.Uint32(lid))})
	require.NoError(t, mdb.AddBlock(block))

	// bL is consumed as prefix.
	// layer is will read first by of the block id, hence 0001 in thi example
	ids, err := mdb.LayerBallotIDs(types.NewLayerID(binary.LittleEndian.Uint32([]byte{bid[0], 0, 0, 0})))
	require.NoError(t, err)
	require.Empty(t, ids)
}

func TestMaliciousBallots(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	lid := types.NewLayerID(1)
	pub := []byte{1, 1, 1}

	ballots := []types.Ballot{
		types.NewExistingBallot(types.BallotID{1}, nil, pub, types.InnerBallot{LayerIndex: lid}),
		types.NewExistingBallot(types.BallotID{2}, nil, pub, types.InnerBallot{LayerIndex: lid}),
		types.NewExistingBallot(types.BallotID{3}, nil, pub, types.InnerBallot{LayerIndex: lid}),
	}
	require.NoError(t, mdb.AddBallot(&ballots[0]))
	require.False(t, ballots[0].IsMalicious())
	for _, ballot := range ballots[1:] {
		require.NoError(t, mdb.AddBallot(&ballot))
		require.True(t, ballot.IsMalicious())
	}
}

func BenchmarkGetBlock(b *testing.B) {
	// cache is set to be twice as large as cache to avoid hitting the cache
	blocks := make([]*types.Block, 200*2)
	db, err := NewPersistentMeshDB(sql.InMemory(),
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
