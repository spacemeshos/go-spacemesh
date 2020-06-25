package activation

import (
	"bytes"
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"math/big"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

func createLayerWithAtx2(t require.TestingT, msh *mesh.Mesh, id types.LayerID, numOfBlocks int, atxs []*types.ActivationTx, votes []types.BlockID, views []types.BlockID) (created []types.BlockID) {
	for i := 0; i < numOfBlocks; i++ {
		block1 := types.NewExistingBlock(id, []byte(rand.String(8)))
		block1.BlockVotes = append(block1.BlockVotes, votes...)
		for _, atx := range atxs {
			block1.ATXIDs = append(block1.ATXIDs, atx.ID())
		}
		block1.ViewEdges = append(block1.ViewEdges, views...)
		err := msh.AddBlockWithTxs(block1, []*types.Transaction{}, atxs)
		require.NoError(t, err)
		created = append(created, block1.ID())
	}
	return
}

type MeshValidatorMock struct{}

func (m *MeshValidatorMock) Persist() error {
	return nil
}

func (m *MeshValidatorMock) LatestComplete() types.LayerID {
	panic("implement me")
}

func (m *MeshValidatorMock) HandleIncomingLayer(layer *types.Layer) (types.LayerID, types.LayerID) {
	return layer.Index() - 1, layer.Index()
}
func (m *MeshValidatorMock) HandleLateBlock(bl *types.Block) (types.LayerID, types.LayerID) {
	return bl.Layer() - 1, bl.Layer()
}

type MockState struct{}

func (MockState) LoadState(types.LayerID) error {
	panic("implement me")
}

func (MockState) GetStateRoot() types.Hash32 {
	panic("implement me")
}

func (MockState) ValidateNonceAndBalance(*types.Transaction) error {
	panic("implement me")
}

func (MockState) GetLayerApplied(types.TransactionID) *types.LayerID {
	panic("implement me")
}

func (MockState) ApplyTransactions(types.LayerID, []*types.Transaction) (int, error) {
	return 0, nil
}

func (MockState) ApplyRewards(types.LayerID, []types.Address, *big.Int) {
}

func (MockState) AddressExists(types.Address) bool {
	return true
}

type ATXDBMock struct {
	mock.Mock
	counter     int
	workSymLock sync.Mutex
	activeSet   uint32
}

func (mock *ATXDBMock) CalcMinerWeights(types.EpochID, map[types.BlockID]struct{}) (map[string]uint64, error) {
	log.Debug("waiting lock")
	mock.workSymLock.Lock()
	defer mock.workSymLock.Unlock()
	log.Debug("done wait")

	mock.counter++
	return map[string]uint64{"aaaaac": 1, "aaabddb": 2, "aaaccc": 3}, nil
}

type MockTxMemPool struct{}

func (MockTxMemPool) Get(types.TransactionID) (*types.Transaction, error) {
	return &types.Transaction{}, nil
}

func (MockTxMemPool) Put(types.TransactionID, *types.Transaction) {}
func (MockTxMemPool) Invalidate(types.TransactionID)              {}

type MockAtxMemPool struct{}

func (MockAtxMemPool) Get(types.ATXID) (*types.ActivationTx, error) {
	return &types.ActivationTx{}, nil
}

func (MockAtxMemPool) Put(*types.ActivationTx) {}
func (MockAtxMemPool) Invalidate(types.ATXID)  {}

func ConfigTst() mesh.Config {
	return mesh.Config{
		BaseReward: big.NewInt(5000),
	}
}

const layersPerEpochBig = 1000

func getAtxDb(id string) (*DB, *mesh.Mesh, database.Database) {
	lg := log.NewDefault(id)
	memesh := mesh.NewMemMeshDB(lg.WithName("meshDB"))
	atxStore := database.NewMemDatabase()
	atxdb := NewDB(atxStore, NewIdentityStore(database.NewMemDatabase()), memesh, layersPerEpochBig, &ValidatorMock{}, lg.WithName("atxDB"))
	layers := mesh.NewMesh(memesh, atxdb, ConfigTst(), &MeshValidatorMock{}, &MockTxMemPool{}, &MockAtxMemPool{}, &MockState{}, lg.WithName("mesh"))
	return atxdb, layers, atxStore
}

func rndStr() string {
	a := make([]byte, 8)
	_, _ = rand.Read(a)
	return string(a)
}

func createLayerWithAtx(t *testing.T, msh *mesh.Mesh, id types.LayerID, numOfBlocks int, atxs []*types.ActivationTx, votes []types.BlockID, views []types.BlockID) (created []types.BlockID) {
	if numOfBlocks < len(atxs) {
		panic("not supported")
	}
	for i := 0; i < numOfBlocks; i++ {
		block1 := types.NewExistingBlock(id, []byte(rand.String(8)))
		block1.BlockVotes = append(block1.BlockVotes, votes...)
		if i < len(atxs) {
			block1.ATXIDs = append(block1.ATXIDs, atxs[i].ID())
			fmt.Printf("adding i=%v bid=%v atxid=%v", i, block1.ID(), atxs[i].ShortString())
		}
		block1.ViewEdges = append(block1.ViewEdges, views...)
		var actualAtxs []*types.ActivationTx
		if i < len(atxs) {
			actualAtxs = atxs[i : i+1]
		}
		block1.Initialize()
		err := msh.AddBlockWithTxs(block1, []*types.Transaction{}, actualAtxs)
		require.NoError(t, err)
		created = append(created, block1.ID())
	}
	return
}

func TestATX_ActiveSetForLayerView(t *testing.T) {
	rand.Seed(1234573298579)
	atxdb, layers, _ := getAtxDb(t.Name())
	blocksMap := make(map[types.BlockID]struct{})
	id1 := types.NodeID{Key: rndStr(), VRFPublicKey: []byte("anton")}
	id2 := types.NodeID{Key: rndStr(), VRFPublicKey: []byte("anton")}
	id3 := types.NodeID{Key: rndStr(), VRFPublicKey: []byte("anton")}
	id4 := types.NodeID{Key: rndStr(), VRFPublicKey: []byte("anton")}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	coinbase4 := types.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		newActivationTx(id1, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase1, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id2, 0, *types.EmptyATXID, *types.EmptyATXID, 2, 0, 100, 200, coinbase2, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id4, 0, *types.EmptyATXID, *types.EmptyATXID, 2, 0, 100, 400, coinbase4, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id3, 0, *types.EmptyATXID, *types.EmptyATXID, 11, 0, 100, 300, coinbase3, 0, []types.BlockID{}, &types.NIPST{}),
	}

	poetRef := []byte{0xba, 0xb0}
	for _, atx := range atxs {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
	}
	blocks := createLayerWithAtx(t, layers, 1, 4, atxs, []types.BlockID{}, []types.BlockID{})
	before := blocks[:2]
	two := blocks[2:3]
	after := blocks[3:]
	for i := 2; i <= 10; i++ {
		before = createLayerWithAtx(t, layers, types.LayerID(i), 1, []*types.ActivationTx{}, before, before)
		two = createLayerWithAtx(t, layers, types.LayerID(i), 1, []*types.ActivationTx{}, two, two)
		after = createLayerWithAtx(t, layers, types.LayerID(i), 1, []*types.ActivationTx{}, after, after)
	}
	for _, x := range before {
		blocksMap[x] = struct{}{}
	}

	for _, x := range after {
		blocksMap[x] = struct{}{}
	}

	layer := types.LayerID(10)
	layersPerEpoch := uint16(6)
	atxdb.LayersPerEpoch = layersPerEpoch
	epoch := layer.GetEpoch(layersPerEpoch)
	actives, err := atxdb.GetMinerWeightsInEpochFromView(epoch, blocksMap)
	assert.NoError(t, err)
	assert.Len(t, actives, 2)
	assert.Equal(t, uint64(10000), actives[id1.Key], "actives[id1.Key] (%d) != %d", actives[id1.Key], 10000)
	assert.Equal(t, uint64(20000), actives[id2.Key], "actives[id2.Key] (%d) != %d", actives[id2.Key], 20000)
}

func TestMesh_ActiveSetForLayerView2(t *testing.T) {
	atxdb, _, _ := getAtxDb(t.Name())
	actives, err := atxdb.GetMinerWeightsInEpochFromView(0, nil)
	assert.Error(t, err)
	assert.Equal(t, "tried to retrieve miner weights for targetEpoch 0", err.Error())
	assert.Nil(t, actives)
}

func TestActivationDb_CalcActiveSetFromViewWithCache(t *testing.T) {
	totalWeightCache.Purge()
	atxdb, layers, _ := getAtxDb("t6")

	id1 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id2 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id3 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		newActivationTx(id1, 0, *types.EmptyATXID, *types.EmptyATXID, 12, 0, 100, 100, coinbase1, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id2, 0, *types.EmptyATXID, *types.EmptyATXID, 300, 0, 100, 100, coinbase2, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id3, 0, *types.EmptyATXID, *types.EmptyATXID, 435, 0, 100, 100, coinbase3, 0, []types.BlockID{}, &types.NIPST{}),
	}

	poetRef := []byte{0xba, 0xb0}
	for _, atx := range atxs {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	mck := ATXDBMock{}
	atx := newActivationTx(id1, 1, atxs[0].ID(), atxs[0].ID(), 1000, 0, 100, 100, coinbase1, 3, blocks, &types.NIPST{})
	atxdb.calcTotalWeightFunc = mck.CalcMinerWeights
	wg := sync.WaitGroup{}
	wg.Add(100)
	mck.workSymLock.Lock()
	for i := 0; i < 100; i++ {
		go func() {
			totalWeight, err := atxdb.CalcTotalWeightFromView(atx.View, atx.PubLayerID.GetEpoch(layersPerEpochBig))
			assert.NoError(t, err)
			assert.Equal(t, 6, int(totalWeight))
			assert.NoError(t, err)
			wg.Done()
		}()
	}
	mck.workSymLock.Unlock()
	wg.Wait()
	assert.Equal(t, 1, mck.counter)
}

func Test_CalcActiveSetFromView(t *testing.T) {
	totalWeightCache.Purge()
	atxdb, layers, _ := getAtxDb("t6")

	id1 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id2 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id3 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		newActivationTx(id1, 0, *types.EmptyATXID, *types.EmptyATXID, 12, 0, 100, 100, coinbase1, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id2, 0, *types.EmptyATXID, *types.EmptyATXID, 300, 0, 100, 100, coinbase2, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id3, 0, *types.EmptyATXID, *types.EmptyATXID, 435, 0, 100, 100, coinbase3, 0, []types.BlockID{}, &types.NIPST{}),
	}

	poetRef := []byte{0xba, 0xb0}
	for _, atx := range atxs {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	atx := newActivationTx(id1, 1, atxs[0].ID(), atxs[0].ID(), 1000, 0, 100, 100, coinbase1, 3, blocks, &types.NIPST{})
	num, err := atxdb.CalcTotalWeightFromView(atx.View, atx.PubLayerID.GetEpoch(layersPerEpochBig))
	assert.NoError(t, err)
	assert.Equal(t, 30000, int(num))

	// check that further atxs dont affect current epoch count
	atxs2 := []*types.ActivationTx{
		newActivationTx(types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}, 0, *types.EmptyATXID, atxs[0].ID(), 1012, 0, 100, 100, coinbase1, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}, 0, *types.EmptyATXID, atxs[1].ID(), 1300, 0, 100, 100, coinbase2, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}, 0, *types.EmptyATXID, atxs[2].ID(), 1435, 0, 100, 100, coinbase3, 0, []types.BlockID{}, &types.NIPST{}),
	}

	for _, atx := range atxs2 {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
	}

	block2 := types.NewExistingBlock(2200, []byte(rand.String(8)))

	block2.ViewEdges = blocks
	block2.Initialize()
	err = layers.AddBlockWithTxs(block2, nil, atxs2)
	assert.NoError(t, err)

	block3 := types.NewExistingBlock(2200, []byte(rand.String(8)))

	block3.ViewEdges = blocks
	block2.Initialize()
	err = layers.AddBlockWithTxs(block3, nil, atxs2)
	assert.NoError(t, err)

	err = atxdb.ProcessAtxs(atxs2)
	assert.NoError(t, err)

	view := []types.BlockID{block2.ID(), block3.ID()}
	sort.Slice(view, func(i, j int) bool {
		return bytes.Compare(view[i].Bytes(), view[j].Bytes()) > 0 // sort view in wrong order
	})
	atx2 := newActivationTx(id3, 0, *types.EmptyATXID, *types.EmptyATXID, 1435, 0, 100, 100, coinbase3, 6, view, &types.NIPST{})
	num, err = atxdb.CalcTotalWeightFromView(atx2.View, atx2.PubLayerID.GetEpoch(layersPerEpochBig))
	assert.NoError(t, err)
	assert.Equal(t, 30000, int(num))

	// put a fake value in the cache and ensure that it's used
	viewHash := types.CalcBlocksHash12(atx2.View)
	totalWeightCache.Purge()
	totalWeightCache.Add(viewHash, 8)

	num, err = atxdb.CalcTotalWeightFromView(atx2.View, atx2.PubLayerID.GetEpoch(layersPerEpochBig))
	assert.NoError(t, err)
	assert.Equal(t, 8, int(num))

	// if the cache has the view in wrong order it should not be used
	sorted := sort.SliceIsSorted(atx2.View, func(i, j int) bool { return bytes.Compare(atx2.View[i].Bytes(), atx2.View[j].Bytes()) > 0 })
	assert.True(t, sorted) // assert that the view is wrongly ordered
	viewBytes, err := types.InterfaceToBytes(atx2.View)
	assert.NoError(t, err)
	viewHash = types.CalcHash12(viewBytes)
	totalWeightCache.Purge()
	totalWeightCache.Add(viewHash, 8)

	num, err = atxdb.CalcTotalWeightFromView(atx2.View, atx2.PubLayerID.GetEpoch(layersPerEpochBig))
	assert.NoError(t, err)
	assert.Equal(t, 30000, int(num))
}

func TestActivationDb_GetNodeLastAtxId(t *testing.T) {
	r := require.New(t)

	atxdb, _, _ := getAtxDb("t6")
	id1 := types.NodeID{Key: uuid.New().String()}
	coinbase1 := types.HexToAddress("aaaa")
	epoch1 := types.EpochID(2)
	atx1 := types.NewActivationTx(newChallenge(id1, 0, *types.EmptyATXID, *types.EmptyATXID, epoch1.FirstLayer(atxdb.LayersPerEpoch)), coinbase1, 3, []types.BlockID{}, &types.NIPST{}, 0, nil)
	r.NoError(atxdb.StoreAtx(epoch1, atx1))

	epoch2 := types.EpochID(1) + (1 << 8)
	// This will fail if we convert the epoch id to bytes using LittleEndian, since LevelDB's lexicographic sorting will
	// then sort by LSB instead of MSB, first.
	atx2 := types.NewActivationTx(newChallenge(id1, 1, atx1.ID(), atx1.ID(), epoch2.FirstLayer(atxdb.LayersPerEpoch)), coinbase1, 3, []types.BlockID{}, &types.NIPST{}, 0, nil)
	r.NoError(atxdb.StoreAtx(epoch2, atx2))

	id, err := atxdb.GetNodeLastAtxID(id1)
	r.NoError(err)
	r.Equal(atx2.ShortString(), id.ShortString(), "atx1.ShortString(): %v", atx1.ShortString())
}

func Test_DBSanity(t *testing.T) {
	atxdb, _, _ := getAtxDb("t6")

	id1 := types.NodeID{Key: uuid.New().String()}
	id2 := types.NodeID{Key: uuid.New().String()}
	id3 := types.NodeID{Key: uuid.New().String()}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")

	atx1 := newActivationTx(id1, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase1, 3, []types.BlockID{}, &types.NIPST{})
	atx2 := newActivationTx(id1, 0, *types.EmptyATXID, *types.EmptyATXID, 1001, 0, 100, 100, coinbase2, 3, []types.BlockID{}, &types.NIPST{})
	atx3 := newActivationTx(id1, 0, *types.EmptyATXID, *types.EmptyATXID, 2001, 0, 100, 100, coinbase3, 3, []types.BlockID{}, &types.NIPST{})

	err := atxdb.storeAtxUnlocked(atx1)
	assert.NoError(t, err)
	err = atxdb.storeAtxUnlocked(atx2)
	assert.NoError(t, err)
	err = atxdb.storeAtxUnlocked(atx3)
	assert.NoError(t, err)

	err = atxdb.addAtxToNodeID(id1, atx1)
	assert.NoError(t, err)
	id, err := atxdb.GetNodeLastAtxID(id1)
	assert.NoError(t, err)
	assert.Equal(t, atx1.ID(), id)
	assert.Equal(t, types.EpochID(1), atx1.TargetEpoch(atxdb.LayersPerEpoch))
	id, err = atxdb.GetNodeAtxIDForEpoch(id1, atx1.TargetEpoch(atxdb.LayersPerEpoch))
	assert.NoError(t, err)
	assert.Equal(t, atx1.ID(), id)

	err = atxdb.addAtxToNodeID(id2, atx2)
	assert.NoError(t, err)

	err = atxdb.addAtxToNodeID(id1, atx3)
	assert.NoError(t, err)

	id, err = atxdb.GetNodeLastAtxID(id2)
	assert.NoError(t, err)
	assert.Equal(t, atx2.ID(), id)
	assert.Equal(t, types.EpochID(2), atx2.TargetEpoch(atxdb.LayersPerEpoch))
	id, err = atxdb.GetNodeAtxIDForEpoch(id2, atx2.TargetEpoch(atxdb.LayersPerEpoch))
	assert.NoError(t, err)
	assert.Equal(t, atx2.ID(), id)

	id, err = atxdb.GetNodeLastAtxID(id1)
	assert.NoError(t, err)
	assert.Equal(t, atx3.ID(), id)
	assert.Equal(t, types.EpochID(3), atx3.TargetEpoch(atxdb.LayersPerEpoch))
	id, err = atxdb.GetNodeAtxIDForEpoch(id1, atx3.TargetEpoch(atxdb.LayersPerEpoch))
	assert.NoError(t, err)
	assert.Equal(t, atx3.ID(), id)

	id, err = atxdb.GetNodeLastAtxID(id3)
	assert.EqualError(t, err, fmt.Sprintf("atx for node %v does not exist", id3.ShortString()))
	assert.Equal(t, *types.EmptyATXID, id)
}

func Test_Wrong_CalcActiveSetFromView(t *testing.T) {
	atxdb, layers, _ := getAtxDb("t6")

	id1 := types.NodeID{Key: uuid.New().String()}
	id2 := types.NodeID{Key: uuid.New().String()}
	id3 := types.NodeID{Key: uuid.New().String()}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		newActivationTx(id1, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase1, 3, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id2, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase2, 3, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id3, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase3, 3, []types.BlockID{}, &types.NIPST{}),
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	atx := newActivationTx(id1, 1, atxs[0].ID(), atxs[0].ID(), 1000, 0, 100, 100, coinbase1, 20, blocks, &types.NIPST{})
	num, err := atxdb.CalcTotalWeightFromView(atx.View, atx.PubLayerID.GetEpoch(layersPerEpoch))
	assert.NoError(t, err)
	assert.NotEqual(t, 20, int(num))

}

func TestMesh_processBlockATXs(t *testing.T) {
	totalWeightCache.Purge()
	atxdb, _, _ := getAtxDb("t6")

	id1 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id2 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id3 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0x76, 0x45}
	npst := NewNIPSTWithChallenge(&chlng, poetRef)
	posATX := newActivationTx(types.NodeID{Key: "aaaaaa", VRFPublicKey: []byte("anton")}, 0, *types.EmptyATXID, *types.EmptyATXID, 1000, 0, 100, 100, coinbase1, 0, []types.BlockID{}, npst)
	err := atxdb.StoreAtx(0, posATX)
	assert.NoError(t, err)
	atxs := []*types.ActivationTx{
		newActivationTx(id1, 0, *types.EmptyATXID, posATX.ID(), 1012, 0, 100, 100, coinbase1, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id2, 0, *types.EmptyATXID, posATX.ID(), 1300, 0, 100, 100, coinbase2, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id3, 0, *types.EmptyATXID, posATX.ID(), 1435, 0, 100, 100, coinbase3, 0, []types.BlockID{}, &types.NIPST{}),
	}
	for _, atx := range atxs {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
	}

	err = atxdb.ProcessAtxs(atxs)
	assert.NoError(t, err)

	// check that further atxs dont affect current epoch count
	atxs2 := []*types.ActivationTx{
		newActivationTx(id1, 1, atxs[0].ID(), atxs[0].ID(), 2012, 0, 100, 100, coinbase1, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id2, 1, atxs[1].ID(), atxs[1].ID(), 2300, 0, 100, 100, coinbase2, 0, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id3, 1, atxs[2].ID(), atxs[2].ID(), 2435, 0, 100, 100, coinbase3, 0, []types.BlockID{}, &types.NIPST{}),
	}
	for _, atx := range atxs2 {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
	}
	err = atxdb.ProcessAtxs(atxs2)
	assert.NoError(t, err)

	assertEpochWeight(t, atxdb, 2, 100*100*4) // 1 posATX + 3 from `atxs`
	assertEpochWeight(t, atxdb, 3, 100*100*3) // 3 from `atxs2`
}

func TestDB_addToEpochWeight(t *testing.T) {
	r := require.New(t)

	atxdb, _, _ := getAtxDb("t6")
	r.NoError(atxdb.addToEpochWeight(123, 1<<64-100))
	r.NoError(atxdb.addToEpochWeight(123, 200))

	assertEpochWeight(t, atxdb, 123, 1<<64-1)
}

func assertEpochWeight(t *testing.T, atxdb *DB, epochID types.EpochID, expectedWeight uint64) {
	epochWeight, err := atxdb.GetEpochWeight(epochID)
	assert.NoError(t, err)
	assert.Equal(t, expectedWeight, epochWeight,
		fmt.Sprintf("expectedWeight (%d) != epochWeight (%d)", expectedWeight, epochWeight))
}

func TestActivationDB_ValidateAtx(t *testing.T) {
	atxdb, layers, _ := getAtxDb("t8")

	signer := signing.NewEdSigner()
	idx1 := types.NodeID{Key: signer.PublicKey().String(), VRFPublicKey: []byte("anton")}

	id1 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id2 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id3 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		newActivationTx(id1, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase1, 3, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id2, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase2, 3, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id3, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase3, 3, []types.BlockID{}, &types.NIPST{}),
	}
	poetRef := []byte{0x12, 0x21}
	for _, atx := range atxs {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	prevAtx := newActivationTx(idx1, 0, *types.EmptyATXID, *types.EmptyATXID, 100, 0, 100, 100, coinbase1, 3, blocks, &types.NIPST{})
	hash, err := prevAtx.NIPSTChallenge.Hash()
	assert.NoError(t, err)
	prevAtx.Nipst = NewNIPSTWithChallenge(hash, poetRef)

	atx := newActivationTx(idx1, 1, prevAtx.ID(), prevAtx.ID(), 1012, 0, 100, 100, coinbase1, 30000, blocks, &types.NIPST{})
	hash, err = atx.NIPSTChallenge.Hash()
	assert.NoError(t, err)
	atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.StoreNodeIdentity(idx1)
	assert.NoError(t, err)
	err = atxdb.StoreAtx(1, prevAtx)
	assert.NoError(t, err)

	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.NoError(t, err)

	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	assert.NoError(t, err)
}

func TestActivationDB_ValidateAtxErrors(t *testing.T) {
	atxdb, layers, _ := getAtxDb("t8")
	signer := signing.NewEdSigner()
	idx1 := types.NodeID{Key: signer.PublicKey().String()}
	idx2 := types.NodeID{Key: uuid.New().String()}
	coinbase := types.HexToAddress("aaaa")

	id1 := types.NodeID{Key: uuid.New().String()}
	id2 := types.NodeID{Key: uuid.New().String()}
	id3 := types.NodeID{Key: uuid.New().String()}
	atxs := []*types.ActivationTx{
		newActivationTx(id1, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id2, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id3, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{}),
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xba, 0xbe}
	npst := NewNIPSTWithChallenge(&chlng, poetRef)
	prevAtx := newActivationTx(idx1, 0, *types.EmptyATXID, *types.EmptyATXID, 100, 0, 100, 100, coinbase, 3, blocks, npst)
	posAtx := newActivationTx(idx2, 0, *types.EmptyATXID, *types.EmptyATXID, 100, 0, 100, 100, coinbase, 3, blocks, npst)
	err := atxdb.StoreNodeIdentity(idx1)
	assert.NoError(t, err)
	err = atxdb.StoreAtx(1, prevAtx)
	assert.NoError(t, err)
	err = atxdb.StoreAtx(1, posAtx)
	assert.NoError(t, err)

	// Wrong sequence.
	atx := newActivationTx(idx1, 0, prevAtx.ID(), posAtx.ID(), 1012, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "sequence number is not one more than prev sequence number")

	// Wrong active set.
	atx = newActivationTx(idx1, 1, prevAtx.ID(), posAtx.ID(), 1012, 0, 100, 100, coinbase, 10, []types.BlockID{}, &types.NIPST{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "atx contains view with unequal weight (10) than seen (0)")

	// Wrong positioning atx.
	atx = newActivationTx(idx1, 1, prevAtx.ID(), atxs[0].ID(), 1012, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "expected distance of one epoch (1000 layers) from pos ATX but found 1011")

	// Wrong prevATx.
	atx = newActivationTx(idx1, 1, atxs[0].ID(), posAtx.ID(), 1012, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, fmt.Sprintf("previous ATX belongs to different miner. atx.ID: %v, atx.NodeID: %v, prevAtx.NodeID: %v", atx.ShortString(), atx.NodeID.Key, atxs[0].NodeID.Key))

	// Wrong layerId.
	posAtx2 := newActivationTx(idx2, 0, *types.EmptyATXID, *types.EmptyATXID, 1020, 0, 100, 100, coinbase, 3, blocks, npst)
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.StoreAtx(1, posAtx2)
	assert.NoError(t, err)
	err = atxdb.StoreNodeIdentity(idx1)
	assert.NoError(t, err)
	atx = newActivationTx(idx1, 1, prevAtx.ID(), posAtx2.ID(), 1012, 0, 100, 100, coinbase, 3, []types.BlockID{}, npst)
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "atx layer (1012) must be after positioning atx layer (1020)")

	// Atx already exists.
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	atx = newActivationTx(idx1, 1, prevAtx.ID(), posAtx.ID(), 12, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")

	// Prev atx declared but not found.
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	atx = newActivationTx(idx1, 1, prevAtx.ID(), posAtx.ID(), 12, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	iter := atxdb.atxs.Find(getNodeAtxPrefix(atx.NodeID))
	for iter.Next() {
		err = atxdb.atxs.Delete(iter.Key())
		assert.NoError(t, err)
	}
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	assert.EqualError(t, err,
		fmt.Sprintf("could not fetch node last ATX: atx for node %v does not exist", atx.NodeID.ShortString()))

	// Prev atx not declared but commitment not included.
	atx = newActivationTx(idx1, 0, *types.EmptyATXID, posAtx.ID(), 1012, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "no prevATX declared, but commitment proof is not included")

	// Prev atx not declared but commitment merkle root not included.
	atx = newActivationTx(idx1, 0, *types.EmptyATXID, posAtx.ID(), 1012, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	atx.Commitment = commitment
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "no prevATX declared, but commitment merkle root is not included in challenge")

	// Challenge and commitment merkle root mismatch.
	atx = newActivationTx(idx1, 0, *types.EmptyATXID, posAtx.ID(), 1012, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	atx.Commitment = commitment
	atx.CommitmentMerkleRoot = append([]byte{}, commitment.MerkleRoot...)
	atx.CommitmentMerkleRoot[0]++
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "commitment merkle root included in challenge is not equal to the merkle root included in the proof")

	// Prev atx declared but commitment is included.
	atx = newActivationTx(idx1, 1, prevAtx.ID(), posAtx.ID(), 1012, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	atx.Commitment = commitment
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "prevATX declared, but commitment proof is included")

	// Prev atx declared but commitment merkle root is included.
	atx = newActivationTx(idx1, 1, prevAtx.ID(), posAtx.ID(), 1012, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	atx.CommitmentMerkleRoot = commitment.MerkleRoot
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "prevATX declared, but commitment merkle root is included in challenge")

	// Prev atx has publication layer in the same epoch as the atx.
	atx = newActivationTx(idx1, 1, prevAtx.ID(), posAtx.ID(), 100, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "prevAtx epoch (0, layer 100) isn't older than current atx epoch (0, layer 100)")

	// NodeID and etracted pubkey dont match
	atx = newActivationTx(idx2, 0, *types.EmptyATXID, posAtx.ID(), 1012, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	atx.Commitment = commitment
	atx.CommitmentMerkleRoot = append([]byte{}, commitment.MerkleRoot...)
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "node ids don't match")
}

func TestActivationDB_ValidateAndInsertSorted(t *testing.T) {
	atxdb, layers, _ := getAtxDb("t8")
	signer := signing.NewEdSigner()
	idx1 := types.NodeID{Key: signer.PublicKey().String(), VRFPublicKey: []byte("12345")}
	coinbase := types.HexToAddress("aaaa")

	id1 := types.NodeID{Key: uuid.New().String()}
	id2 := types.NodeID{Key: uuid.New().String()}
	id3 := types.NodeID{Key: uuid.New().String()}
	atxs := []*types.ActivationTx{
		newActivationTx(id1, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id2, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{}),
		newActivationTx(id3, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{}),
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0x56, 0xbe}
	npst := NewNIPSTWithChallenge(&chlng, poetRef)
	prevAtx := newActivationTx(idx1, 0, *types.EmptyATXID, *types.EmptyATXID, 100, 0, 100, 100, coinbase, 3, blocks, npst)

	var nodeAtxIds []types.ATXID

	err := atxdb.StoreAtx(1, prevAtx)
	assert.NoError(t, err)
	nodeAtxIds = append(nodeAtxIds, prevAtx.ID())

	// wrong sequnce
	atx := newActivationTx(idx1, 1, prevAtx.ID(), prevAtx.ID(), 1012, 0, 100, 100, coinbase, 0, []types.BlockID{}, &types.NIPST{})
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	nodeAtxIds = append(nodeAtxIds, atx.ID())

	atx = newActivationTx(idx1, 2, atx.ID(), atx.ID(), 1012, 0, 100, 100, coinbase, 0, []types.BlockID{}, &types.NIPST{})
	assert.NoError(t, err)
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.StoreNodeIdentity(idx1)
	assert.NoError(t, err)
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	nodeAtxIds = append(nodeAtxIds, atx.ID())
	atx2id := atx.ID()

	atx = newActivationTx(idx1, 4, prevAtx.ID(), prevAtx.ID(), 1012, 0, 100, 100, coinbase, 0, []types.BlockID{}, &types.NIPST{})
	err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.StoreNodeIdentity(idx1)
	assert.NoError(t, err)

	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "sequence number is not one more than prev sequence number")

	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	id4 := atx.ID()

	atx = newActivationTx(idx1, 3, atx2id, prevAtx.ID(), 1012, 0, 100, 100, coinbase, 0, []types.BlockID{}, &types.NIPST{})
	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")

	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	nodeAtxIds = append(nodeAtxIds, atx.ID())
	nodeAtxIds = append(nodeAtxIds, id4)

	id, err := atxdb.GetNodeLastAtxID(idx1)
	assert.NoError(t, err)
	assert.Equal(t, atx.ID(), id)

	_, err = atxdb.GetAtxHeader(id)
	assert.NoError(t, err)

	_, err = atxdb.GetAtxHeader(atx2id)
	assert.NoError(t, err)

	// test same sequence
	idx2 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("12345")}

	prevAtx = newActivationTx(idx2, 0, *types.EmptyATXID, *types.EmptyATXID, 100, 0, 100, 100, coinbase, 3, blocks, npst)
	err = atxdb.StoreAtx(1, prevAtx)
	assert.NoError(t, err)

	atx = newActivationTx(idx2, 1, prevAtx.ID(), prevAtx.ID(), 1012, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	atxID := atx.ID()

	atx = newActivationTx(idx2, 2, atxID, atx.ID(), 1012, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)

	atx = newActivationTx(idx2, 2, atxID, atx.ID(), 1013, 0, 100, 100, coinbase, 0, []types.BlockID{}, &types.NIPST{})
	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")

	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)

}

func TestActivationDb_ProcessAtx(t *testing.T) {
	r := require.New(t)

	atxdb, _, _ := getAtxDb("t8")
	idx1 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase := types.HexToAddress("aaaa")
	atx := newActivationTx(idx1, 0, *types.EmptyATXID, *types.EmptyATXID, 100, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})

	err := atxdb.ProcessAtx(atx)
	r.NoError(err)
	r.NoError(err)
	res, err := atxdb.GetIdentity(idx1.Key)
	r.Nil(err)
	r.Equal(idx1, res)
}

func BenchmarkActivationDb_SyntacticallyValidateAtx(b *testing.B) {
	r := require.New(b)
	nopLogger := log.NewDefault("").WithOptions(log.Nop)

	atxdb, layers, _ := getAtxDb("t8")
	atxdb.log = nopLogger
	layers.Log = nopLogger

	const (
		activesetSize  = 300
		blocksPerLayer = 200
		numberOfLayers = 100
	)

	coinbase := types.HexToAddress("c012ba5e")
	var atxs []*types.ActivationTx
	for i := 0; i < activesetSize; i++ {
		id := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("vrf")}
		atxs = append(atxs, newActivationTx(id, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{}))
	}

	poetRef := []byte{0x12, 0x21}
	for _, atx := range atxs {
		hash, err := atx.NIPSTChallenge.Hash()
		r.NoError(err)
		atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
	}

	blocks := createLayerWithAtx2(b, layers, 0, blocksPerLayer, atxs, []types.BlockID{}, []types.BlockID{})
	for i := 1; i < numberOfLayers; i++ {
		blocks = createLayerWithAtx2(b, layers, types.LayerID(i), blocksPerLayer, []*types.ActivationTx{}, blocks, blocks)
	}

	idx1 := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	challenge := newChallenge(idx1, 0, *types.EmptyATXID, *types.EmptyATXID, numberOfLayers+1)
	hash, err := challenge.Hash()
	r.NoError(err)
	prevAtx := newAtx(challenge, blocks, NewNIPSTWithChallenge(hash, poetRef))

	atx := newActivationTx(idx1, 1, prevAtx.ID(), prevAtx.ID(), numberOfLayers+1+layersPerEpochBig, 0, 100, 100, coinbase, activesetSize, blocks, &types.NIPST{})
	hash, err = atx.NIPSTChallenge.Hash()
	r.NoError(err)
	atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
	err = atxdb.StoreAtx(1, prevAtx)
	r.NoError(err)

	start := time.Now()
	err = atxdb.SyntacticallyValidateAtx(atx)
	fmt.Printf("\nSyntactic validation took %v\n", time.Since(start))
	r.NoError(err)

	start = time.Now()
	err = atxdb.SyntacticallyValidateAtx(atx)
	fmt.Printf("\nSecond syntactic validation took %v\n", time.Since(start))
	r.NoError(err)

	start = time.Now()
	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	fmt.Printf("\nContextual validation took %v\n\n", time.Since(start))
	r.NoError(err)
}

func BenchmarkNewActivationDb(b *testing.B) {
	r := require.New(b)

	const tmpPath = "../tmp/atx"
	lg := log.NewDefault("id").WithOptions(log.Nop)

	msh := mesh.NewMemMeshDB(lg)
	store, err := database.NewLDBDatabase(tmpPath, 0, 0, lg.WithName("atxLDB"))
	r.NoError(err)
	atxdb := NewDB(store, NewIdentityStore(store), msh, layersPerEpochBig, &ValidatorMock{}, lg.WithName("atxDB"))

	const (
		numOfMiners = 300
		batchSize   = 15
		numOfEpochs = 10 * batchSize
	)
	prevAtxs := make([]types.ATXID, numOfMiners)
	pPrevAtxs := make([]types.ATXID, numOfMiners)
	posAtx := prevAtxID
	var atx *types.ActivationTx
	layer := types.LayerID(postGenesisEpochLayer)

	start := time.Now()
	eStart := time.Now()
	for epoch := postGenesisEpoch; epoch < postGenesisEpoch+numOfEpochs; epoch++ {
		for miner := 0; miner < numOfMiners; miner++ {
			challenge := newChallenge(nodeID, 1, prevAtxs[miner], posAtx, layer)
			h, err := challenge.Hash()
			r.NoError(err)
			atx = newAtx(challenge, defaultView, NewNIPSTWithChallenge(h, poetRef))
			prevAtxs[miner] = atx.ID()
			storeAtx(r, atxdb, atx, log.NewDefault("storeAtx").WithOptions(log.Nop))
		}
		//noinspection GoNilness
		posAtx = atx.ID()
		layer += layersPerEpoch
		if epoch%batchSize == batchSize-1 {
			fmt.Printf("epoch %3d-%3d took %v\t", epoch-(batchSize-1), epoch, time.Since(eStart))
			eStart = time.Now()

			for miner := 0; miner < numOfMiners; miner++ {
				atx, err := atxdb.GetAtxHeader(prevAtxs[miner])
				r.NoError(err)
				r.NotNil(atx)
				atx, err = atxdb.GetAtxHeader(pPrevAtxs[miner])
				r.NoError(err)
				r.NotNil(atx)
			}
			fmt.Printf("reading last and previous epoch 100 times took %v\n", time.Since(eStart))
			eStart = time.Now()
		}
		copy(pPrevAtxs, prevAtxs)
	}
	fmt.Printf("\n>>> Total time: %v\n\n", time.Since(start))
	time.Sleep(1 * time.Second)

	// cleanup
	err = os.RemoveAll(tmpPath)
	r.NoError(err)
}

func TestActivationDb_TopAtx(t *testing.T) {
	r := require.New(t)

	atxdb, _, _ := getAtxDb("t8")

	// ATX stored should become top ATX
	atx, err := createAndStoreAtx(atxdb, 0)
	r.NoError(err)

	topAtx, err := atxdb.getTopAtx()
	r.NoError(err)
	r.Equal(atx.ID(), topAtx.AtxID)

	// higher-layer ATX stored should become new top ATX
	atx, err = createAndStoreAtx(atxdb, 3)
	r.NoError(err)

	topAtx, err = atxdb.getTopAtx()
	r.NoError(err)
	r.Equal(atx.ID(), topAtx.AtxID)

	// lower-layer ATX stored should NOT become new top ATX
	atx, err = createAndStoreAtx(atxdb, 1)
	r.NoError(err)

	topAtx, err = atxdb.getTopAtx()
	r.NoError(err)
	r.NotEqual(atx.ID(), topAtx.AtxID)
}

func createAndValidateSignedATX(r *require.Assertions, atxdb *DB, ed *signing.EdSigner, atx *types.ActivationTx) (*types.ActivationTx, error) {
	atxBytes, err := types.InterfaceToBytes(atx.InnerActivationTx)
	r.NoError(err)
	sig := ed.Sign(atxBytes)

	signedAtx := &types.ActivationTx{InnerActivationTx: atx.InnerActivationTx, Sig: sig}
	return signedAtx, atxdb.ValidateSignedAtx(*ed.PublicKey(), signedAtx)
}

func TestActivationDb_ValidateSignedAtx(t *testing.T) {
	r := require.New(t)
	lg := log.NewDefault("sigValidation")
	idStore := NewIdentityStore(database.NewMemDatabase())
	memesh := mesh.NewMemMeshDB(lg.WithName("meshDB"))
	atxdb := NewDB(database.NewMemDatabase(), idStore, memesh, layersPerEpochBig, &ValidatorMock{}, lg.WithName("atxDB"))

	ed := signing.NewEdSigner()
	nodeID := types.NodeID{Key: ed.PublicKey().String(), VRFPublicKey: []byte("bbbbb")}

	// test happy flow of first ATX
	emptyAtx := types.EmptyATXID
	atx := newActivationTx(nodeID, 1, *emptyAtx, *emptyAtx, 15, 1, 100, 100, coinbase, 5, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, npst)
	_, err := createAndValidateSignedATX(r, atxdb, ed, atx)
	r.NoError(err)

	// test negative flow no atx found in idstore
	prevAtx := types.ATXID(types.HexToHash32("0x111"))
	atx = newActivationTx(nodeID, 1, prevAtx, prevAtx, 15, 1, 100, 100, coinbase, 5, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, npst)
	signedAtx, err := createAndValidateSignedATX(r, atxdb, ed, atx)
	r.Equal(errInvalidSig, err)

	// test happy flow not first ATX
	err = idStore.StoreNodeIdentity(nodeID)
	r.NoError(err)
	_, err = createAndValidateSignedATX(r, atxdb, ed, atx)
	r.NoError(err)

	// test negative flow not first ATX, invalid sig
	signedAtx.Sig = []byte("anton")
	_, err = ExtractPublicKey(signedAtx)
	r.Error(err)

}

func createAndStoreAtx(atxdb *DB, layer types.LayerID) (*types.ActivationTx, error) {
	id := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("vrf")}
	atx := newActivationTx(id, 0, *types.EmptyATXID, *types.EmptyATXID, layer, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})
	err := atxdb.StoreAtx(atx.TargetEpoch(layersPerEpoch), atx)
	if err != nil {
		return nil, err
	}
	return atx, nil
}

func TestActivationDb_AwaitAtx(t *testing.T) {
	r := require.New(t)

	lg := log.NewDefault("sigValidation")
	idStore := NewIdentityStore(database.NewMemDatabase())
	memesh := mesh.NewMemMeshDB(lg.WithName("meshDB"))
	atxdb := NewDB(database.NewMemDatabase(), idStore, memesh, layersPerEpochBig, &ValidatorMock{}, lg.WithName("atxDB"))
	id := types.NodeID{Key: uuid.New().String(), VRFPublicKey: []byte("vrf")}
	atx := newActivationTx(id, 0, *types.EmptyATXID, *types.EmptyATXID, 1, 0, 100, 100, coinbase, 3, []types.BlockID{}, &types.NIPST{})

	ch := atxdb.AwaitAtx(atx.ID())
	r.Len(atxdb.atxChannels, 1) // channel was created

	select {
	case <-ch:
		r.Fail("notified before ATX was stored")
	default:
	}

	err := atxdb.StoreAtx(atx.TargetEpoch(layersPerEpoch), atx)
	r.NoError(err)
	r.Len(atxdb.atxChannels, 0) // after notifying subscribers, channel is cleared

	select {
	case <-ch:
	default:
		r.Fail("not notified after ATX was stored")
	}

	otherID := types.ATXID{}
	copy(otherID[:], "abcd")
	atxdb.AwaitAtx(otherID)
	r.Len(atxdb.atxChannels, 1) // after first subscription - channel is created
	atxdb.AwaitAtx(otherID)
	r.Len(atxdb.atxChannels, 1) // second subscription to same id - no additional channel
	atxdb.UnsubscribeAtx(otherID)
	r.Len(atxdb.atxChannels, 1) // first unsubscribe doesn't clear the channel
	atxdb.UnsubscribeAtx(otherID)
	r.Len(atxdb.atxChannels, 0) // last unsubscribe clears the channel
}

func TestActivationDb_ContextuallyValidateAtx(t *testing.T) {
	r := require.New(t)

	lg := log.NewDefault("sigValidation")
	idStore := NewIdentityStore(database.NewMemDatabase())
	memesh := mesh.NewMemMeshDB(lg.WithName("meshDB"))
	atxdb := NewDB(database.NewMemDatabase(), idStore, memesh, layersPerEpochBig, &ValidatorMock{}, lg.WithName("atxDB"))

	atx := types.NewActivationTx(newChallenge(nodeID, 0, *types.EmptyATXID, *types.EmptyATXID, 0), [20]byte{}, 5, nil, nil, 0, nil)
	err := atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	r.NoError(err)
}
