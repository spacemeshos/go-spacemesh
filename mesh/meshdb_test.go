package mesh

import (
	"bytes"
	"context"
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
	Path = "../tmp/mdb"
)

func teardown() {
	_ = os.RemoveAll(Path)
}

func getMeshDB(tb testing.TB) *DB {
	tb.Helper()
	return NewMemMeshDB(logtest.New(tb))
}

func TestMeshDB_New(t *testing.T) {
	mdb := getMeshDB(t)
	bl := types.GenLayerBlock(types.NewLayerID(1), types.RandomTXSet(10))
	err := mdb.AddBlock(bl)
	assert.NoError(t, err)
	block, err := mdb.GetBlock(bl.ID())
	assert.NoError(t, err)
	assert.True(t, bl.ID() == block.ID())
}

func TestMeshDB_New_Proposal(t *testing.T) {
	mdb := getMeshDB(t)
	bl := types.GenLayerProposal(types.NewLayerID(1), nil)
	err := mdb.AddProposal(bl)
	assert.NoError(t, err)
	block, err := mdb.getProposal(bl.ID())
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

func TestMeshDB_AddBlockNotCached(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	mdb.blockCache = newBlockCache(1)
	defer mdb.Close()

	txIDs := types.RandomTXSet(100)
	block := types.GenLayerBlock(types.NewLayerID(10), txIDs)
	require.NoError(t, mdb.AddBlock(block))

	got, err := mdb.GetBlock(block.ID())
	require.NoError(t, err)

	assert.Equal(t, block.ID(), got.ID())
	assert.Equal(t, block.LayerIndex, got.LayerIndex)
	assert.Equal(t, block.SmesherID(), got.SmesherID())
	assert.Equal(t, txIDs, got.TxIDs)
	assert.Equal(t, block.TxIDs, got.TxIDs)
	require.NotNil(t, got.EpochData)
	assert.Equal(t, block.EpochData.ActiveSet, got.EpochData.ActiveSet)

	gotP, err := mdb.getProposal(types.ProposalID(block.ID()))
	require.NoError(t, err)

	assert.Equal(t, block.ID().Bytes(), gotP.ID().Bytes())
	assert.Equal(t, block.LayerIndex, gotP.LayerIndex)
	assert.Equal(t, block.SmesherID(), gotP.SmesherID())
	assert.Equal(t, txIDs, gotP.TxIDs)
	assert.Equal(t, block.TxIDs, gotP.TxIDs)
	require.NotNil(t, gotP.EpochData)
	assert.Equal(t, block.EpochData.ActiveSet, gotP.EpochData.ActiveSet)

	// add second block should cause the first block to be evicted
	require.NoError(t, mdb.AddBlock(types.GenLayerBlock(types.NewLayerID(10), types.RandomTXSet(100))))

	got2, err := mdb.GetBlock(block.ID())
	require.NoError(t, err)

	assert.NotEqual(t, got, got2)
	assert.Equal(t, block.ID(), got2.ID())
	assert.Equal(t, block.LayerIndex, got2.LayerIndex)
	// Smesher ID is not restored for block
	assert.Nil(t, got2.SmesherID())
	assert.Equal(t, block.TxIDs, got2.TxIDs)
	assert.Nil(t, got2.EpochData)

	// proposal is unchanged
	gotP2, err := mdb.getProposal(types.ProposalID(block.ID()))
	require.NoError(t, err)
	assert.Equal(t, gotP, gotP2)
}

func TestMeshDB_AddProposal(t *testing.T) {
	mdb, err := NewPersistentMeshDB(Path+"/mesh_db/", 1, logtest.New(t))
	require.NoError(t, err)
	defer mdb.Close()

	layer := types.NewLayerID(123)
	proposal := types.GenLayerProposal(layer, types.RandomTXSet(199))
	require.False(t, mdb.HasProposal(proposal.ID()))
	require.False(t, mdb.HasBallot(proposal.Ballot.ID()))

	require.NoError(t, mdb.AddProposal(proposal))
	assert.True(t, mdb.HasProposal(proposal.ID()))
	assert.True(t, mdb.HasBallot(proposal.Ballot.ID()))

	got, err := mdb.GetBlock(types.BlockID(proposal.ID()))
	require.NoError(t, err)
	gotP := (*types.Proposal)(got)
	// from cache, so Smesher ID still exist
	assert.Equal(t, proposal, gotP)

	gotB, err := mdb.GetBallot(proposal.Ballot.ID())
	require.NoError(t, err)
	assert.Equal(t, proposal.Ballot, *gotB)

	// copy the proposal and change it slightly
	proposalNew := *proposal
	proposalNew.LayerIndex = layer.Add(100)
	proposal.TxIDs = proposal.TxIDs[1:]
	// the copied proposal still has the same ProposalID
	require.Equal(t, proposal.ID(), proposalNew.ID())
	require.Equal(t, proposal.Ballot.ID(), proposalNew.Ballot.ID())
	// this proposal ID already exist, will not overwrite the previous proposal
	require.NoError(t, mdb.AddProposal(&proposalNew))
	assert.True(t, mdb.HasProposal(proposal.ID()))
	gotNew, err := mdb.GetBlock(types.BlockID(proposal.ID()))
	require.NoError(t, err)
	gotPNew := (*types.Proposal)(gotNew)
	assert.Equal(t, gotP, gotPNew)
	// ballot is unchanged too
	gotBNew, err := mdb.GetBallot(proposal.Ballot.ID())
	require.NoError(t, err)
	assert.Equal(t, gotB, gotBNew)
}

func chooseRandomPattern(blocksInLayer int, patternSize int) []int {
	p := rand.Perm(blocksInLayer)
	indexes := make([]int, 0, patternSize)
	for _, r := range p[:patternSize] {
		indexes = append(indexes, r)
	}
	return indexes
}

func createLayerWithRandVoting(index types.LayerID, prev []*types.Layer, blocksInLayer int, patternSize int, lg log.Log) *types.Layer {
	l := types.NewLayer(index)
	var patterns [][]int
	for _, l := range prev {
		blocks := l.Blocks()
		blocksInPrevLayer := len(blocks)
		patterns = append(patterns, chooseRandomPattern(blocksInPrevLayer, int(math.Min(float64(blocksInPrevLayer), float64(patternSize)))))
	}
	layerProposals := make([]types.ProposalID, 0, blocksInLayer)
	for i := 0; i < blocksInLayer; i++ {
		p := types.GenLayerProposal(types.NewLayerID(0), nil)
		voted := make(map[types.ProposalID]struct{})
		layerProposals = append(layerProposals, p.ID())
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				p.ForDiff = append(p.ForDiff, b.ID())
				voted[p.ID()] = struct{}{}
			}
		}
		for _, prevProposal := range prev[0].Proposals() {
			if _, ok := voted[prevProposal.ID()]; !ok {
				p.AgainstDiff = append(p.AgainstDiff, (types.BlockID)(prevProposal.ID()))
			}
		}
		p.LayerIndex = index
		l.AddProposal(p)
	}
	lg.Info("Created mesh.LayerID %d with proposals %d", l.Index(), layerProposals)
	return l
}

func BenchmarkNewPersistentMeshDB(b *testing.B) {
	const batchSize = 50

	r := require.New(b)

	mdb, err := NewPersistentMeshDB(path.Join(Path, "mesh_db"), 5, logtest.New(b))
	require.NoError(b, err)
	defer mdb.Close()
	defer teardown()

	l := types.GenesisLayer()
	gen := l.Blocks()[0]

	err = mdb.AddBlock(gen)
	r.NoError(err)

	start := time.Now()
	lStart := time.Now()
	for i := 0; i < 10*batchSize; i++ {
		lyr := createLayerWithRandVoting(l.Index().Add(1), []*types.Layer{l}, 200, 20, logtest.New(b))
		for _, b := range lyr.Blocks() {
			err := mdb.AddBlock(b)
			r.NoError(err)
		}
		l = lyr
		if i%batchSize == batchSize-1 {
			b.Logf("layers %3d-%3d took %12v\t", i-(batchSize-1), i, time.Since(lStart))
			lStart = time.Now()
			for i := 0; i < 100; i++ {
				for _, b := range lyr.Proposals() {
					block, err := mdb.getProposal(b.ID())
					r.NoError(err)
					r.NotNil(block)
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
	var addr [types.AddressLength]byte
	copy(addr[:], "12345678901234567890")
	return addr
}

func newTx(r *require.Assertions, signer *signing.EdSigner, nonce, totalAmount uint64) *types.Transaction {
	feeAmount := uint64(1)
	tx, err := types.GenerateCallTransaction(signer, types.Address{}, nonce, totalAmount-feeAmount, 3, feeAmount)
	r.NoError(err)
	return tx
}

func newTxWithDest(r *require.Assertions, signer *signing.EdSigner, dest types.Address, nonce, totalAmount uint64) *types.Transaction {
	feeAmount := uint64(1)
	tx, err := types.GenerateCallTransaction(signer, dest, nonce, totalAmount-feeAmount, 3, feeAmount)
	r.NoError(err)
	return tx
}

func newSignerAndAddress(r *require.Assertions, seedStr string) (*signing.EdSigner, types.Address) {
	seed := make([]byte, 32)
	copy(seed, seedStr)
	_, privKey, err := ed25519.GenerateKey(bytes.NewReader(seed))
	r.NoError(err)
	signer, err := signing.NewEdSignerFromBuffer(privKey)
	r.NoError(err)
	return signer, types.GenerateAddress(signer.PublicKey().Bytes())
}

func TestMeshDB_GetStateProjection(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(logtest.New(t))
	signer, origin := newSignerAndAddress(r, "123")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(r, signer, 0, 10),
		newTx(r, signer, 1, 20),
	}, types.NewLayerID(1))
	r.NoError(err)

	nonce, balance, err := mdb.GetProjection(origin, initialNonce, initialBalance)
	r.NoError(err)
	r.Equal(initialNonce+2, int(nonce))
	r.Equal(initialBalance-30, int(balance))
}

func TestMeshDB_GetStateProjection_WrongNonce(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(logtest.New(t))
	signer, origin := newSignerAndAddress(r, "123")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(r, signer, 1, 10),
		newTx(r, signer, 2, 20),
	}, types.NewLayerID(1))
	r.NoError(err)

	nonce, balance, err := mdb.GetProjection(origin, initialNonce, initialBalance)
	r.NoError(err)
	r.Equal(initialNonce, int(nonce))
	r.Equal(initialBalance, int(balance))
}

func TestMeshDB_GetStateProjection_DetectNegativeBalance(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(logtest.New(t))
	signer, origin := newSignerAndAddress(r, "123")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(r, signer, 0, 10),
		newTx(r, signer, 1, 95),
	}, types.NewLayerID(1))
	r.NoError(err)

	nonce, balance, err := mdb.GetProjection(origin, initialNonce, initialBalance)
	r.NoError(err)
	r.Equal(1, int(nonce))
	r.Equal(initialBalance-10, int(balance))
}

func TestMeshDB_GetStateProjection_NothingToApply(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(logtest.New(t))

	nonce, balance, err := mdb.GetProjection(address(), initialNonce, initialBalance)
	r.NoError(err)
	r.Equal(uint64(initialNonce), nonce)
	r.Equal(uint64(initialBalance), balance)
}

func TestMeshDB_UnappliedTxs(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(logtest.New(t))

	signer1, origin1 := newSignerAndAddress(r, "thc")
	signer2, origin2 := newSignerAndAddress(r, "cbd")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(r, signer1, 420, 240),
		newTx(r, signer1, 421, 241),
		newTx(r, signer2, 0, 100),
		newTx(r, signer2, 1, 101),
	}, types.NewLayerID(1))
	r.NoError(err)

	txns1 := getTxns(r, mdb, origin1)
	r.Len(txns1, 2)
	r.Equal(420, int(txns1[0].Nonce))
	r.Equal(421, int(txns1[1].Nonce))
	r.Equal(240, int(txns1[0].TotalAmount))
	r.Equal(241, int(txns1[1].TotalAmount))

	txns2 := getTxns(r, mdb, origin2)
	r.Len(txns2, 2)
	r.Equal(0, int(txns2[0].Nonce))
	r.Equal(1, int(txns2[1].Nonce))
	r.Equal(100, int(txns2[0].TotalAmount))
	r.Equal(101, int(txns2[1].TotalAmount))

	mdb.removeFromUnappliedTxs([]*types.Transaction{
		newTx(r, signer2, 0, 100),
	})

	txns1 = getTxns(r, mdb, origin1)
	r.Len(txns1, 2)
	r.Equal(420, int(txns1[0].Nonce))
	r.Equal(421, int(txns1[1].Nonce))
	r.Equal(240, int(txns1[0].TotalAmount))
	r.Equal(241, int(txns1[1].TotalAmount))

	txns2 = getTxns(r, mdb, origin2)
	r.Len(txns2, 1)
	r.Equal(1, int(txns2[0].Nonce))
	r.Equal(101, int(txns2[0].TotalAmount))
}

func TestMeshDB_testGetTransactions(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(logtest.New(t))

	signer1, addr1 := newSignerAndAddress(r, "thc")
	signer2, _ := newSignerAndAddress(r, "cbd")
	_, addr3 := newSignerAndAddress(r, "cbe")
	p := &types.Proposal{}
	p.LayerIndex = types.NewLayerID(1)
	err := mdb.writeTransactions(p,
		newTx(r, signer1, 420, 240),
		newTx(r, signer1, 421, 241),
		newTxWithDest(r, signer2, addr1, 0, 100),
		newTxWithDest(r, signer2, addr1, 1, 101),
	)
	r.NoError(err)

	txs, err := mdb.GetTransactionsByOrigin(types.NewLayerID(1), addr1)
	r.NoError(err)
	r.Equal(2, len(txs))

	txs, err = mdb.GetTransactionsByDestination(types.NewLayerID(1), addr1)
	r.NoError(err)
	r.Equal(2, len(txs))

	// test negative case
	txs, err = mdb.GetTransactionsByOrigin(types.NewLayerID(1), addr3)
	r.NoError(err)
	r.Equal(0, len(txs))

	txs, err = mdb.GetTransactionsByDestination(types.NewLayerID(1), addr3)
	r.NoError(err)
	r.Equal(0, len(txs))
}

type TinyTx struct {
	ID          types.TransactionID
	Nonce       uint64
	TotalAmount uint64
}

func getTxns(r *require.Assertions, mdb *DB, origin types.Address) []TinyTx {
	txns, err := mdb.getAccountPendingTxs(origin)
	r.NoError(err)
	var ret []TinyTx
	for nonce, nonceTxs := range txns.PendingTxs {
		for id, tx := range nonceTxs {
			ret = append(ret, TinyTx{ID: id, Nonce: nonce, TotalAmount: tx.Amount + tx.Fee})
		}
	}
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].Nonce < ret[j].Nonce
	})
	return ret
}

func TestMeshDB_testGetRewards(t *testing.T) {
	r := require.New(t)
	mdb := NewMemMeshDB(logtest.New(t))
	signer1, addr1 := newSignerAndAddress(r, "123")
	signer2, addr2 := newSignerAndAddress(r, "456")
	signer3, addr3 := newSignerAndAddress(r, "789")
	_, addr4 := newSignerAndAddress(r, "999")

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

	smesher1String := smesher1.String()
	smesher2String := smesher2.String()
	smesher3String := smesher3.String()

	test1Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
		addr3: {
			smesher3String: 1,
		},
	}

	test2Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
	}

	test3Map := map[types.Address]map[string]uint64{
		addr2: {
			smesher2String: 2,
		},
	}

	err := mdb.writeTransactionRewards(types.NewLayerID(1), test1Map, 10000, 9000)
	r.NoError(err)

	err = mdb.writeTransactionRewards(types.NewLayerID(2), test2Map, 20000, 19000)
	r.NoError(err)

	err = mdb.writeTransactionRewards(types.NewLayerID(3), test3Map, 15000, 14500)
	r.NoError(err)

	rewards, err := mdb.GetRewards(addr2)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher2, Coinbase: addr2},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher2, Coinbase: addr2},
		{Layer: types.NewLayerID(3), TotalReward: 30000, LayerRewardEstimate: 29000, SmesherID: smesher2, Coinbase: addr2},
	}, rewards)

	rewards, err = mdb.GetRewards(addr1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher1, Coinbase: addr1},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher1, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewards(addr4)
	r.NoError(err)
	r.Nil(rewards)
}

func TestMeshDB_testGetRewardsBySmesher(t *testing.T) {
	r := require.New(t)
	mdb := NewMemMeshDB(logtest.New(t))
	signer1, addr1 := newSignerAndAddress(r, "123")
	signer2, addr2 := newSignerAndAddress(r, "456")
	signer3, addr3 := newSignerAndAddress(r, "789")
	signer4, _ := newSignerAndAddress(r, "999")

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
	smesher4 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer4.PublicKey().Bytes(),
	}

	smesher1String := smesher1.String()
	smesher2String := smesher2.String()
	smesher3String := smesher3.String()

	test1Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
		addr3: {
			smesher3String: 1,
		},
	}

	test2Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
	}

	test3Map := map[types.Address]map[string]uint64{
		addr2: {
			smesher2String: 2,
		},
	}

	err := mdb.writeTransactionRewards(types.NewLayerID(1), test1Map, 10000, 9000)
	r.NoError(err)

	err = mdb.writeTransactionRewards(types.NewLayerID(2), test2Map, 20000, 19000)
	r.NoError(err)

	err = mdb.writeTransactionRewards(types.NewLayerID(3), test3Map, 15000, 14500)
	r.NoError(err)

	rewards, err := mdb.GetRewardsBySmesherID(smesher2)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher2, Coinbase: addr2},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher2, Coinbase: addr2},
		{Layer: types.NewLayerID(3), TotalReward: 30000, LayerRewardEstimate: 29000, SmesherID: smesher2, Coinbase: addr2},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher1, Coinbase: addr1},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher1, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher4)
	r.NoError(err)
	r.Nil(rewards)
}

func TestMeshDB_testGetRewardsBySmesherChangingLayer(t *testing.T) {
	r := require.New(t)
	mdb := NewMemMeshDB(logtest.New(t))
	signer1, addr1 := newSignerAndAddress(r, "123")
	signer2, addr2 := newSignerAndAddress(r, "456")
	signer3, addr3 := newSignerAndAddress(r, "789")
	signer4, _ := newSignerAndAddress(r, "999")

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
	smesher4 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer4.PublicKey().Bytes(),
	}

	smesher1String := smesher1.String()
	smesher2String := smesher2.String()
	smesher3String := smesher3.String()

	test1Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
		addr3: {
			smesher3String: 1,
		},
	}

	test2Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher2String: 1,
		},
		addr2: {
			smesher3String: 1,
		},
	}

	test3Map := map[types.Address]map[string]uint64{
		addr2: {
			smesher2String: 2,
		},
	}

	err := mdb.writeTransactionRewards(types.NewLayerID(1), test1Map, 10000, 9000)
	r.NoError(err)

	err = mdb.writeTransactionRewards(types.NewLayerID(2), test2Map, 20000, 19000)
	r.NoError(err)

	err = mdb.writeTransactionRewards(types.NewLayerID(3), test3Map, 15000, 14500)
	r.NoError(err)

	rewards, err := mdb.GetRewardsBySmesherID(smesher2)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher2, Coinbase: addr2},
		{Layer: types.NewLayerID(3), TotalReward: 30000, LayerRewardEstimate: 29000, SmesherID: smesher2, Coinbase: addr2},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher2, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher1, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher3)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher3, Coinbase: addr2},
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher3, Coinbase: addr3},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher4)
	r.NoError(err)
	r.Nil(rewards)
}

func TestMeshDB_testGetRewardsBySmesherMultipleSmeshers(t *testing.T) {
	r := require.New(t)
	mdb := NewMemMeshDB(logtest.New(t))
	signer1, addr1 := newSignerAndAddress(r, "123")
	signer2, addr2 := newSignerAndAddress(r, "456")
	signer3, addr3 := newSignerAndAddress(r, "789")
	signer4, _ := newSignerAndAddress(r, "999")

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
	smesher4 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer4.PublicKey().Bytes(),
	}

	smesher1String := smesher1.String()
	smesher2String := smesher2.String()
	smesher3String := smesher3.String()
	smesher4String := smesher4.String()

	test1Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
			smesher4String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
		addr3: {
			smesher3String: 1,
		},
	}

	err := mdb.writeTransactionRewards(types.NewLayerID(1), test1Map, 10000, 9000)
	r.NoError(err)

	rewards, err := mdb.GetRewardsBySmesherID(smesher2)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher2, Coinbase: addr2},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher1, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher3)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher3, Coinbase: addr3},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher4)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher4, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewards(addr1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher1, Coinbase: addr1},
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher4, Coinbase: addr1},
	}, rewards)
}

func TestMeshDB_testGetRewardsBySmesherMultipleSmeshersAndLayers(t *testing.T) {
	r := require.New(t)
	mdb := NewMemMeshDB(logtest.New(t))
	signer1, addr1 := newSignerAndAddress(r, "123")
	signer2, addr2 := newSignerAndAddress(r, "456")
	signer3, addr3 := newSignerAndAddress(r, "789")
	signer4, _ := newSignerAndAddress(r, "999")

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
	smesher4 := types.NodeID{
		Key:          signer1.PublicKey().String(),
		VRFPublicKey: signer4.PublicKey().Bytes(),
	}

	smesher1String := smesher1.String()
	smesher2String := smesher2.String()
	smesher3String := smesher3.String()
	smesher4String := smesher4.String()

	test1Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
			smesher4String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
		addr3: {
			smesher3String: 1,
		},
	}

	test2Map := map[types.Address]map[string]uint64{
		addr1: {
			smesher1String: 1,
			smesher4String: 1,
		},
		addr2: {
			smesher2String: 1,
		},
		addr3: {
			smesher3String: 1,
		},
	}

	err := mdb.writeTransactionRewards(types.NewLayerID(1), test1Map, 10000, 9000)
	r.NoError(err)

	err = mdb.writeTransactionRewards(types.NewLayerID(2), test2Map, 20000, 19000)
	r.NoError(err)

	rewards, err := mdb.GetRewardsBySmesherID(smesher2)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher2, Coinbase: addr2},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher2, Coinbase: addr2},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher1, Coinbase: addr1},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher1, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher3)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher3, Coinbase: addr3},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher3, Coinbase: addr3},
	}, rewards)

	rewards, err = mdb.GetRewardsBySmesherID(smesher4)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher4, Coinbase: addr1},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher4, Coinbase: addr1},
	}, rewards)

	rewards, err = mdb.GetRewards(addr1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher1, Coinbase: addr1},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher1, Coinbase: addr1},
		{Layer: types.NewLayerID(1), TotalReward: 10000, LayerRewardEstimate: 9000, SmesherID: smesher4, Coinbase: addr1},
		{Layer: types.NewLayerID(2), TotalReward: 20000, LayerRewardEstimate: 19000, SmesherID: smesher4, Coinbase: addr1},
	}, rewards)
}

func TestMeshDB_RecordCoinFlip(t *testing.T) {
	r := require.New(t)
	layerID := types.NewLayerID(123)

	testCoinflip := func(mdb *DB) {
		_, exists := mdb.GetCoinflip(context.TODO(), layerID)
		r.False(exists, "coin value should not exist before being inserted")
		mdb.RecordCoinflip(context.TODO(), layerID, true)
		coin, exists := mdb.GetCoinflip(context.TODO(), layerID)
		r.True(exists, "expected coin value to exist")
		r.True(coin, "expected true coin value")
		mdb.RecordCoinflip(context.TODO(), layerID, false)
		coin, exists = mdb.GetCoinflip(context.TODO(), layerID)
		r.True(exists, "expected coin value to exist")
		r.False(coin, "expected false coin value on overwrite")
	}

	mdb1 := NewMemMeshDB(logtest.New(t))
	defer mdb1.Close()
	testCoinflip(mdb1)
	mdb2, err := NewPersistentMeshDB(Path+"/mesh_db/", 5, logtest.New(t))
	require.NoError(t, err)
	defer mdb2.Close()
	defer teardown()
	testCoinflip(mdb2)
}

func TestMeshDB_GetMeshTransactions(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(logtest.New(t))

	signer1, _ := newSignerAndAddress(r, "thc")

	p := &types.Proposal{}
	p.LayerIndex = types.NewLayerID(1)
	var (
		nonce  uint64
		ids    []types.TransactionID
		layers = 10
	)
	for i := 1; i <= layers; i++ {
		nonce++
		p.LayerIndex = types.NewLayerID(uint32(i))
		tx := newTx(r, signer1, nonce, 240)
		ids = append(ids, tx.ID())
		r.NoError(mdb.writeTransactions(p, tx))
	}
	txs, missing := mdb.GetMeshTransactions(ids)
	r.Len(missing, 0)
	for i := 1; i < layers; i++ {
		r.Equal(ids[i-1], txs[i-1].ID())
		r.EqualValues(types.NewLayerID(uint32(i)), txs[i-1].LayerID)
	}
}

func TestMesh_FindOnce(t *testing.T) {
	mdb := NewMemMeshDB(logtest.New(t))
	defer mdb.Close()

	r := require.New(t)
	signer1, addr1 := newSignerAndAddress(r, "thc")
	signer2, _ := newSignerAndAddress(r, "cbd")

	p := &types.Proposal{}
	layers := []uint32{1, 10, 100}
	nonce := uint64(0)
	for _, layer := range layers {
		p.LayerIndex = types.NewLayerID(layer)
		nonce++
		err := mdb.writeTransactions(p,
			newTx(r, signer1, nonce, 100),
			newTxWithDest(r, signer2, addr1, nonce, 100),
		)
		r.NoError(err)
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

func BenchmarkGetBlock(b *testing.B) {
	// cache is set to be twice as large as cache to avoid hitting the cache
	blocks := make([]*types.Block, layerSize*2)
	db, err := NewPersistentMeshDB(b.TempDir(),
		1, /*size of the cache is multiplied by a constant (layerSize). for the benchmark it needs to be no more than layerSize*/
		logtest.New(b))
	require.NoError(b, err)
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
