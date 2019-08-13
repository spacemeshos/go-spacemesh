package mesh

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math"
	"math/big"
	"os"
	"path"
	"testing"
	"time"
)

const (
	Path = "../tmp/mdb"
)

func teardown() {
	os.RemoveAll(Path)
}

func getMeshdb() *MeshDB {
	return NewMemMeshDB(log.New("mdb", "", ""))
}

func TestNewMeshDB(t *testing.T) {
	mdb := getMeshdb()
	id := types.BlockID(123)
	mdb.AddBlock(types.NewExistingBlock(123, 1, nil))
	block, err := mdb.GetBlock(123)
	assert.NoError(t, err)
	assert.True(t, id == block.Id)
}

func TestMeshDB_AddBlock(t *testing.T) {

	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	defer mdb.Close()
	coinbase := address.HexToAddress("aaaa")

	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data1"))

	addTransactionsWithGas(mdb, block1, 4, rand.Int63n(100))

	poetRef := []byte{0xba, 0x05}
	atx := types.NewActivationTx(types.NodeId{"aaaa", []byte("bbb")}, coinbase, 1, types.AtxId{}, 5, 1, types.AtxId{}, 5, []types.BlockID{1, 2, 3}, nipst.NewNIPSTWithChallenge(&common.Hash{}, poetRef))

	block1.AtxIds = append(block1.AtxIds, atx.Id())
	err := mdb.AddBlock(block1)
	assert.NoError(t, err)

	rBlock1, err := mdb.GetBlock(block1.Id)
	assert.NoError(t, err)

	assert.True(t, len(rBlock1.TxIds) == len(block1.TxIds), "block content was wrong")
	assert.True(t, len(rBlock1.AtxIds) == len(block1.AtxIds), "block content was wrong")
	//assert.True(t, bytes.Compare(rBlock2.Data, []byte("data2")) == 0, "block content was wrong")
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
		bl := types.NewExistingBlock(types.RandBlockId(), 0, []byte("data data data"))
		layerBlocks = append(layerBlocks, bl.ID())
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				bl.AddVote(types.BlockID(b.Id))
			}
		}
		for _, prevBloc := range prev[0].Blocks() {
			bl.AddView(types.BlockID(prevBloc.Id))
		}
		l.AddBlock(bl)
	}
	lg.Info("Created mesh.LayerID %d with blocks %d", l.Index(), layerBlocks)
	return l
}

func TestForEachInView_Persistent(t *testing.T) {
	mdb, err := NewPersistentMeshDB(Path+"/mesh_db/", log.New("TestForEachInView", "", ""))
	require.NoError(t, err)
	defer mdb.Close()
	defer teardown()
	testForeachInView(mdb, t)
}

func TestForEachInView_InMem(t *testing.T) {
	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	testForeachInView(mdb, t)
}

func testForeachInView(mdb *MeshDB, t *testing.T) {
	blocks := make(map[types.BlockID]*types.Block)
	l := GenesisLayer()
	gen := l.Blocks()[0]
	blocks[gen.ID()] = gen

	if err := mdb.AddBlock(gen); err != nil {
		t.Fail()
	}

	for i := 0; i < 4; i++ {
		lyr := createLayerWithRandVoting(l.Index()+1, []*types.Layer{l}, 2, 2, log.NewDefault("msh"))
		for _, b := range lyr.Blocks() {
			blocks[b.ID()] = b
			mdb.AddBlock(b)
		}
		l = lyr
	}
	mp := map[types.BlockID]struct{}{}
	foo := func(nb *types.Block) error {
		fmt.Println("process block", "layer", nb.Id, nb.LayerIndex)
		mp[nb.Id] = struct{}{}
		return nil
	}
	ids := map[types.BlockID]struct{}{}
	for _, b := range l.Blocks() {
		ids[b.Id] = struct{}{}
	}
	mdb.ForBlockInView(ids, 0, foo)
	for _, bl := range blocks {
		_, found := mp[bl.ID()]
		assert.True(t, found, "did not process block  ", bl)
	}
}

func BenchmarkNewPersistentMeshDB(b *testing.B) {
	const batchSize = 50

	r := require.New(b)

	mdb, err := NewPersistentMeshDB(path.Join(Path, "mesh_db"), log.NewDefault("meshDb"))
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
					block, err := mdb.GetBlock(b.Id)
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

type MockStateObj struct {
}

func (m *MockStateObj) Address() address.Address {
	var addr [20]byte
	copy(addr[:], "12345678901234567890")
	return addr
}

func (m *MockStateObj) Nonce() uint64 {
	return initialNonce
}

func (m *MockStateObj) Balance() *big.Int {
	return big.NewInt(initialBalance)
}

func newTx(origin address.Address, nonce, amount uint64) *types.AddressableSignedTransaction {
	return types.NewAddressableTx(nonce, origin, address.Address{}, amount, 3, 1)
}

func TestMeshDB_GetStateProjection(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	stateObj := &MockStateObj{}
	origin := stateObj.Address()
	err := mdb.addToMeshTxs([]*types.AddressableSignedTransaction{
		newTx(origin, 0, 10),
		newTx(origin, 1, 20),
	})
	r.NoError(err)

	nonce, balance, err := mdb.GetStateProjection(stateObj)
	r.NoError(err)
	r.Equal(uint64(initialNonce+2), nonce)
	r.Equal(uint64(initialBalance-30), balance)
}

func addToMeshTxs(mdb *MeshDB, txs []*types.AddressableSignedTransaction) error {
	tinyTxs := make([]tinyTx, 0, len(txs))
	for _, tx := range txs {
		tinyTxs = append(tinyTxs, addressableTxToTiny(tx))
	}
	groupedTxs := make(map[address.Address][]tinyTx)
	for _, tx := range tinyTxs {
		groupedTxs[tx.Origin] = append(groupedTxs[tx.Origin], tx)
	}

	for account, accountTxs := range groupedTxs {
		accountTxsBytes, err := types.InterfaceToBytes(&accountTxs)
		if err != nil {
			return fmt.Errorf("failed to marshal account tx list: %v", err)
		}
		if err := mdb.meshTxs.Put(account.Bytes(), accountTxsBytes); err != nil {
			return fmt.Errorf("failed to store mesh txs for address %s", account.Short())
		}
	}
	return nil
}

func TestMeshDB_GetStateProjection_WrongNonce(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	stateObj := &MockStateObj{}
	origin := stateObj.Address()
	err := addToMeshTxs(mdb, []*types.AddressableSignedTransaction{
		newTx(origin, 1, 10),
		newTx(origin, 0, 20),
	})
	r.NoError(err)

	_, _, err = mdb.GetStateProjection(stateObj)
	r.EqualError(err, "inconsistent state! When adding tx a44e208d8241599c30add840988c233ba73d2d9300549fb11e9572ee5c37be9a, expected nonce: 0, actual: 1")
}

func TestMeshDB_GetStateProjection_DetectNegativeBalance(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	stateObj := &MockStateObj{}
	origin := stateObj.Address()
	err := addToMeshTxs(mdb, []*types.AddressableSignedTransaction{
		newTx(origin, 0, 10),
		newTx(origin, 1, 95),
	})
	r.NoError(err)

	_, _, err = mdb.GetStateProjection(stateObj)
	r.EqualError(err, "inconsistent state! When adding tx ad650cd8f1c3ab90ec015f77bcbb96def02ca0268ccee08cf77041e7c9d5cc07 balance becomes negative. Balance before: 90, Amount: 95")
}

func TestMeshDB_GetStateProjection_NothingToApply(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	stateObj := &MockStateObj{}

	nonce, balance, err := mdb.GetStateProjection(stateObj)
	r.NoError(err)
	r.Equal(uint64(initialNonce), nonce)
	r.Equal(uint64(initialBalance), balance)
}

func TestMeshDB_MeshTxs(t *testing.T) {
	r := require.New(t)

	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))

	origin1 := address.BytesToAddress([]byte("thc"))
	origin2 := address.BytesToAddress([]byte("cbd"))
	err := mdb.addToMeshTxs([]*types.AddressableSignedTransaction{
		newTx(origin1, 421, 241),
		newTx(origin1, 420, 240),
		newTx(origin2, 1, 101),
		newTx(origin2, 0, 100),
	})
	r.NoError(err)

	txns1 := getTxns(r, mdb, origin1)
	r.Len(txns1, 2)
	r.Equal(420, int(txns1[0].Nonce))
	r.Equal(421, int(txns1[1].Nonce))
	r.Equal(240, int(txns1[0].Amount))
	r.Equal(241, int(txns1[1].Amount))

	txns2 := getTxns(r, mdb, origin2)
	r.Len(txns2, 2)
	r.Equal(0, int(txns2[0].Nonce))
	r.Equal(1, int(txns2[1].Nonce))
	r.Equal(100, int(txns2[0].Amount))
	r.Equal(101, int(txns2[1].Amount))

	err = mdb.removeFromMeshTxs([]*Transaction{SerializableSignedTransaction2StateTransaction(newTx(origin2, 1, 101))})
	r.NoError(err)

	txns1 = getTxns(r, mdb, origin1)
	r.Len(txns1, 2)
	r.Equal(420, int(txns1[0].Nonce))
	r.Equal(421, int(txns1[1].Nonce))
	r.Equal(240, int(txns1[0].Amount))
	r.Equal(241, int(txns1[1].Amount))

	txns2 = getTxns(r, mdb, origin2)
	r.Len(txns2, 1)
	r.Equal(1, int(txns2[0].Nonce))
	r.Equal(101, int(txns2[0].Amount))
}

func getTxns(r *require.Assertions, mdb *MeshDB, origin address.Address) []tinyTx {
	txnsB, err := mdb.meshTxs.Get(origin.Bytes())
	r.NoError(err)
	var txns []tinyTx
	err = types.BytesToInterface(txnsB, &txns)
	r.NoError(err)
	return txns
}
