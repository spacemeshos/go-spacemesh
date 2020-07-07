package mesh

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/pendingtxs"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math"
	"math/big"
	"os"
	"path"
	"sort"
	"testing"
	"time"
)

const (
	Path = "../tmp/mdb"
)

func teardown() {
	_ = os.RemoveAll(Path)
}

func getMeshDB() *DB {
	return NewMemMeshDB(log.New("mdb", "", ""))
}

func TestNewMeshDB(t *testing.T) {
	mdb := getMeshDB()
	bl := types.NewExistingBlock(1, []byte(rand.String(8)))
	err := mdb.AddBlock(bl)
	assert.NoError(t, err)
	block, err := mdb.GetBlock(bl.ID())
	assert.NoError(t, err)
	assert.True(t, bl.ID() == block.ID())
}

func TestMeshDB_AddBlock(t *testing.T) {

	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	defer mdb.Close()
	coinbase := types.HexToAddress("aaaa")

	block1 := types.NewExistingBlock(1, []byte("data1"))

	addTransactionsWithFee(t, mdb, block1, 4, rand.Int63n(100))

	poetRef := []byte{0xba, 0x05}
	atx := newActivationTx(types.NodeID{Key: "aaaa", VRFPublicKey: []byte("bbb")}, 1, types.ATXID{}, 5, 1, types.ATXID{}, coinbase, 5, []types.BlockID{}, &types.NIPST{
		Space:          0,
		NipstChallenge: &types.Hash32{},
		PostProof: &types.PostProof{
			Challenge:    poetRef,
			MerkleRoot:   []byte(nil),
			ProofNodes:   [][]byte(nil),
			ProvenLeaves: [][]byte(nil),
		},
	})
	var atxs []types.ATXID
	atxs = append(atxs, atx.ID())
	block1.ActiveSet = &atxs
	err := mdb.AddBlock(block1)
	assert.NoError(t, err)

	rBlock1, err := mdb.GetBlock(block1.ID())
	assert.NoError(t, err)

	assert.True(t, len(rBlock1.TxIDs) == len(block1.TxIDs), "block content was wrong")
	assert.True(t, len(*rBlock1.ActiveSet) == len(*block1.ActiveSet), "block content was wrong")
	// assert.True(t, bytes.Compare(rBlock2.Data, []byte("data2")) == 0, "block content was wrong")
}

func chooseRandomPattern(blocksInLayer int, patternSize int) []int {
	rand.Seed(time.Now().UnixNano())
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
	layerBlocks := make([]types.BlockID, 0, blocksInLayer)
	for i := 0; i < blocksInLayer; i++ {
		bl := types.NewExistingBlock(0, []byte(rand.String(8)))
		layerBlocks = append(layerBlocks, bl.ID())
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				bl.AddVote(b.ID())
			}
		}
		for _, prevBloc := range prev[0].Blocks() {
			bl.AddView(prevBloc.ID())
		}
		bl.LayerIndex = index
		l.AddBlock(bl)
	}
	lg.Info("Created mesh.LayerID %d with blocks %d", l.Index(), layerBlocks)
	return l
}

func TestForEachInView_Persistent(t *testing.T) {
	mdb, err := NewPersistentMeshDB(Path+"/mesh_db/", 5, log.New("TestForEachInView", "", ""))
	require.NoError(t, err)
	defer mdb.Close()
	defer teardown()
	testForeachInView(mdb, t)
}

func TestForEachInView_InMem(t *testing.T) {
	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	testForeachInView(mdb, t)
}

func testForeachInView(mdb *DB, t *testing.T) {
	blocks := make(map[types.BlockID]*types.Block)
	l := GenesisLayer()
	/*gen := l.Blocks()[0]
	blocks[gen.ID()] = gen

	if err := mdb.AddBlock(gen); err != nil {
		t.Fail()
	}*/

	for i := 0; i < 4; i++ {
		lyr := createLayerWithRandVoting(l.Index()+1, []*types.Layer{l}, 2, 2, log.NewDefault("msh"))
		for _, b := range lyr.Blocks() {
			blocks[b.ID()] = b
			err := mdb.AddBlock(b)
			assert.NoError(t, err)
		}
		l = lyr
	}
	mp := map[types.BlockID]struct{}{}
	foo := func(nb *types.Block) (bool, error) {
		fmt.Println("process block", "layer", nb.ID(), nb.LayerIndex)
		mp[nb.ID()] = struct{}{}
		return false, nil
	}
	ids := map[types.BlockID]struct{}{}
	for _, b := range l.Blocks() {
		ids[b.ID()] = struct{}{}
	}
	err := mdb.ForBlockInView(ids, 0, foo)
	assert.NoError(t, err)
	for _, bl := range blocks {
		_, found := mp[bl.ID()]
		assert.True(t, found, "did not process block  ", bl)
	}
}

func TestForEachInView_InMem_WithStop(t *testing.T) {
	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	blocks := make(map[types.BlockID]*types.Block)
	l := GenesisLayer()
	gen := l.Blocks()[0]
	blocks[gen.ID()] = gen

	for i := 0; i < 4; i++ {
		lyr := createLayerWithRandVoting(l.Index()+1, []*types.Layer{l}, 2, 2, log.NewDefault("msh"))
		for _, b := range lyr.Blocks() {
			blocks[b.ID()] = b
			err := mdb.AddBlock(b)
			assert.NoError(t, err)
		}
		l = lyr
	}
	mp := map[types.BlockID]struct{}{}
	i := 0
	foo := func(nb *types.Block) (bool, error) {
		fmt.Println("process block", "layer", nb.ID(), nb.LayerIndex)
		mp[nb.ID()] = struct{}{}
		i++
		return i == 5, nil
	}
	ids := map[types.BlockID]struct{}{}
	for _, b := range l.Blocks() {
		ids[b.ID()] = struct{}{}
	}
	err := mdb.ForBlockInView(ids, 0, foo)
	assert.NoError(t, err)
	assert.Equal(t, 5, i)
}

func TestForEachInView_InMem_WithLimitedLayer(t *testing.T) {
	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	blocks := make(map[types.BlockID]*types.Block)
	l := GenesisLayer()

	for i := 0; i < 4; i++ {
		lyr := createLayerWithRandVoting(l.Index()+1, []*types.Layer{l}, 2, 2, log.NewDefault("msh"))
		for _, b := range lyr.Blocks() {
			blocks[b.ID()] = b
			err := mdb.AddBlock(b)
			assert.NoError(t, err)
		}
		l = lyr
	}
	mp := map[types.BlockID]struct{}{}
	i := 0
	foo := func(nb *types.Block) (bool, error) {
		fmt.Println("process block", "layer", nb.ID(), nb.LayerIndex)
		mp[nb.ID()] = struct{}{}
		i++
		return false, nil
	}
	ids := map[types.BlockID]struct{}{}
	for _, b := range l.Blocks() {
		ids[b.ID()] = struct{}{}
	}
	// traverse until (and including) layer 2
	err := mdb.ForBlockInView(ids, 2, foo)
	assert.NoError(t, err)
	assert.Equal(t, 6, i)
}

func BenchmarkNewPersistentMeshDB(b *testing.B) {
	const batchSize = 50

	r := require.New(b)

	mdb, err := NewPersistentMeshDB(path.Join(Path, "mesh_db"), 5, log.NewDefault("meshDb"))
	require.NoError(b, err)
	defer mdb.Close()
	defer teardown()

	l := GenesisLayer()
	gen := l.Blocks()[0]

	err = mdb.AddBlock(gen)
	r.NoError(err)

	start := time.Now()
	lStart := time.Now()
	for i := 0; i < 10*batchSize; i++ {
		lyr := createLayerWithRandVoting(l.Index()+1, []*types.Layer{l}, 200, 20, log.NewDefault("msh").WithOptions(log.Nop))
		for _, b := range lyr.Blocks() {
			err := mdb.AddBlock(b)
			r.NoError(err)
		}
		l = lyr
		if i%batchSize == batchSize-1 {
			fmt.Printf("layers %3d-%3d took %12v\t", i-(batchSize-1), i, time.Since(lStart))
			lStart = time.Now()
			for i := 0; i < 100; i++ {
				for _, b := range lyr.Blocks() {
					block, err := mdb.GetBlock(b.ID())
					r.NoError(err)
					r.NotNil(block)
				}
			}
			fmt.Printf("reading last layer 100 times took %v\n", time.Since(lStart))
			lStart = time.Now()
		}
	}
	fmt.Printf("\n>>> Total time: %v\n\n", time.Since(start))
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

func newTx(r *require.Assertions, signer *signing.EdSigner, nonce, totalAmount uint64) *types.Transaction {
	feeAmount := uint64(1)
	tx, err := types.NewSignedTx(nonce, types.Address{}, totalAmount-feeAmount, 3, feeAmount, signer)
	r.NoError(err)
	return tx
}

func newTxWithDest(r *require.Assertions, signer *signing.EdSigner, dest types.Address, nonce, totalAmount uint64) *types.Transaction {
	feeAmount := uint64(1)
	tx, err := types.NewSignedTx(nonce, dest, totalAmount-feeAmount, 3, feeAmount, signer)
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
	var addr types.Address
	addr.SetBytes(signer.PublicKey().Bytes())
	return signer, addr
}

func TestMeshDB_GetStateProjection(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(log.NewDefault("DB.GetStateProjection"))
	signer, origin := newSignerAndAddress(r, "123")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(r, signer, 0, 10),
		newTx(r, signer, 1, 20),
	}, 1)
	r.NoError(err)

	nonce, balance, err := mdb.GetProjection(origin, initialNonce, initialBalance)
	r.NoError(err)
	r.Equal(initialNonce+2, int(nonce))
	r.Equal(initialBalance-30, int(balance))
}

func TestMeshDB_GetStateProjection_WrongNonce(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	signer, origin := newSignerAndAddress(r, "123")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(r, signer, 1, 10),
		newTx(r, signer, 2, 20),
	}, 1)
	r.NoError(err)

	nonce, balance, err := mdb.GetProjection(origin, initialNonce, initialBalance)
	r.NoError(err)
	r.Equal(initialNonce, int(nonce))
	r.Equal(initialBalance, int(balance))
}

func TestMeshDB_GetStateProjection_DetectNegativeBalance(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	signer, origin := newSignerAndAddress(r, "123")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(r, signer, 0, 10),
		newTx(r, signer, 1, 95),
	}, 1)
	r.NoError(err)

	nonce, balance, err := mdb.GetProjection(origin, initialNonce, initialBalance)
	r.NoError(err)
	r.Equal(1, int(nonce))
	r.Equal(initialBalance-10, int(balance))
}

func TestMeshDB_GetStateProjection_NothingToApply(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))

	nonce, balance, err := mdb.GetProjection(address(), initialNonce, initialBalance)
	r.NoError(err)
	r.Equal(uint64(initialNonce), nonce)
	r.Equal(uint64(initialBalance), balance)
}

func TestMeshDB_UnappliedTxs(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))

	signer1, origin1 := newSignerAndAddress(r, "thc")
	signer2, origin2 := newSignerAndAddress(r, "cbd")
	err := mdb.addToUnappliedTxs([]*types.Transaction{
		newTx(r, signer1, 420, 240),
		newTx(r, signer1, 421, 241),
		newTx(r, signer2, 0, 100),
		newTx(r, signer2, 1, 101),
	}, 1)
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

	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))

	signer1, addr1 := newSignerAndAddress(r, "thc")
	signer2, _ := newSignerAndAddress(r, "cbd")
	_, addr3 := newSignerAndAddress(r, "cbe")
	err := mdb.writeTransactions(1, []*types.Transaction{
		newTx(r, signer1, 420, 240),
		newTx(r, signer1, 421, 241),
		newTxWithDest(r, signer2, addr1, 0, 100),
		newTxWithDest(r, signer2, addr1, 1, 101),
	})
	r.NoError(err)

	txs := mdb.GetTransactionsByOrigin(1, addr1)
	r.Equal(2, len(txs))

	txs = mdb.GetTransactionsByDestination(1, addr1)
	r.Equal(2, len(txs))

	// test negative case
	txs = mdb.GetTransactionsByOrigin(1, addr3)
	r.Equal(0, len(txs))

	txs = mdb.GetTransactionsByDestination(1, addr3)
	r.Equal(0, len(txs))
}

type TinyTx struct {
	ID          types.TransactionID
	Nonce       uint64
	TotalAmount uint64
}

func getTxns(r *require.Assertions, mdb *DB, origin types.Address) []TinyTx {
	txnsB, err := mdb.unappliedTxs.Get(origin.Bytes())
	if err == database.ErrNotFound {
		return []TinyTx{}
	}
	r.NoError(err)
	var txns pendingtxs.AccountPendingTxs
	err = types.BytesToInterface(txnsB, &txns)
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
	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	_, addr1 := newSignerAndAddress(r, "123")
	_, addr2 := newSignerAndAddress(r, "456")
	_, addr3 := newSignerAndAddress(r, "789")
	_, addr4 := newSignerAndAddress(r, "999")

	err := mdb.writeTransactionRewards(1, []types.Address{addr1, addr2, addr3}, big.NewInt(10000), big.NewInt(9000))
	r.NoError(err)

	err = mdb.writeTransactionRewards(2, []types.Address{addr1, addr2}, big.NewInt(20000), big.NewInt(19000))
	r.NoError(err)

	err = mdb.writeTransactionRewards(3, []types.Address{addr2, addr2}, big.NewInt(15000), big.NewInt(14500))
	r.NoError(err)

	rewards, err := mdb.GetRewards(addr2)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: 1, TotalReward: 10000, LayerRewardEstimate: 9000},
		{Layer: 2, TotalReward: 20000, LayerRewardEstimate: 19000},
		{Layer: 3, TotalReward: 30000, LayerRewardEstimate: 29000},
	}, rewards)

	rewards, err = mdb.GetRewards(addr1)
	r.NoError(err)
	r.Equal([]types.Reward{
		{Layer: 1, TotalReward: 10000, LayerRewardEstimate: 9000},
		{Layer: 2, TotalReward: 20000, LayerRewardEstimate: 19000},
	}, rewards)

	rewards, err = mdb.GetRewards(addr4)
	r.NoError(err)
	r.Nil(rewards)
}
