package activation

import (
	"bytes"
	"errors"
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
		block1 := types.NewExistingBlock(id, []byte(rand.RandString(8)))
		block1.BlockVotes = append(block1.BlockVotes, votes...)
		for _, atx := range atxs {
			block1.AtxIds = append(block1.AtxIds, atx.Id())
		}
		block1.ViewEdges = append(block1.ViewEdges, views...)
		err := msh.AddBlockWithTxs(block1, []*types.Transaction{}, atxs)
		require.NoError(t, err)
		created = append(created, block1.Id())
	}
	return
}

type MeshValidatorMock struct{}

func (m *MeshValidatorMock) HandleIncomingLayer(layer *types.Layer) (types.LayerID, types.LayerID) {
	return layer.Index() - 1, layer.Index()
}
func (m *MeshValidatorMock) HandleLateBlock(bl *types.Block)              {}
func (m *MeshValidatorMock) RegisterLayerCallback(func(id types.LayerID)) {}
func (m *MeshValidatorMock) ContextualValidity(id types.BlockID) bool     { return true }
func (m *MeshValidatorMock) GetGoodPatternBlocks(layer types.LayerID) (map[types.BlockID]struct{}, error) {
	return nil, errors.New("not implemented")
}

type MockState struct{}

func (MockState) ValidateNonceAndBalance(transaction *types.Transaction) error {
	panic("implement me")
}

func (MockState) GetLayerApplied(txId types.TransactionId) *types.LayerID {
	panic("implement me")
}

func (MockState) ApplyTransactions(layer types.LayerID, txs []*types.Transaction) (int, error) {
	return 0, nil
}

func (MockState) ApplyRewards(layer types.LayerID, miners []types.Address, reward *big.Int) {
}

func (MockState) AddressExists(addr types.Address) bool {
	return true
}

func (MockState) ValidateSignature(signed types.Signed) (types.Address, error) {
	return types.Address{}, nil
}

type ATXDBMock struct {
	mock.Mock
	counter     int
	workSymLock sync.Mutex
	activeSet   uint32
}

func (mock *ATXDBMock) CalcActiveSetFromView(view []types.BlockID, pubEpoch types.EpochId) (uint32, error) {
	return mock.activeSet, nil
}

func (mock *ATXDBMock) CalcActiveSetSize(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
	mock.counter++
	defer mock.workSymLock.Unlock()
	log.Info("working")
	mock.workSymLock.Lock()
	log.Info("done working")
	return map[string]struct{}{"aaaaac": {}, "aaabddb": {}, "aaaccc": {}}, nil
}

func (mock *ATXDBMock) GetAtxHeader(id types.AtxId) (*types.ActivationTxHeader, error) {
	panic("not implemented")
}

func (mock *ATXDBMock) GetPosAtxId(types.EpochId) (*types.AtxId, error) {
	panic("not implemented")
}

func (mock *ATXDBMock) GetNodeLastAtxId(nodeId types.NodeId) (types.AtxId, error) {
	panic("not implemented")
}

func (mock *ATXDBMock) GetPrevAtxId(node types.NodeId) (*types.AtxId, error) {
	panic("not implemented")
}

type MockTxMemPool struct{}

func (MockTxMemPool) Get(id types.TransactionId) (*types.Transaction, error) {
	return &types.Transaction{}, nil
}
func (MockTxMemPool) PopItems(size int) []types.Transaction {
	return nil
}
func (MockTxMemPool) Put(id types.TransactionId, item *types.Transaction) {

}
func (MockTxMemPool) Invalidate(id types.TransactionId) {

}

type MockAtxMemPool struct{}

func (MockAtxMemPool) Get(id types.AtxId) (*types.ActivationTx, error) {
	return &types.ActivationTx{}, nil
}

func (MockAtxMemPool) PopItems(size int) []types.ActivationTx {
	return nil
}

func (MockAtxMemPool) Put(atx *types.ActivationTx) {

}

func (MockAtxMemPool) Invalidate(id types.AtxId) {

}

func ConfigTst() mesh.Config {
	return mesh.Config{
		BaseReward: big.NewInt(5000),
	}
}

const layersPerEpochBig = 1000

func getAtxDb(id string) (*ActivationDb, *mesh.Mesh, database.Database) {
	lg := log.NewDefault(id)
	memesh := mesh.NewMemMeshDB(lg.WithName("meshDB"))
	atxStore := database.NewMemDatabase()
	atxdb := NewActivationDb(atxStore, NewIdentityStore(database.NewMemDatabase()), memesh, layersPerEpochBig, &ValidatorMock{}, lg.WithName("atxDB"))
	layers := mesh.NewMesh(memesh, atxdb, ConfigTst(), &MeshValidatorMock{}, &MockTxMemPool{}, &MockAtxMemPool{}, &MockState{}, lg.WithName("mesh"))
	return atxdb, layers, atxStore
}

func rndStr() string {
	a := make([]byte, 8)
	rand.Read(a)
	return string(a)
}

func createLayerWithAtx(t *testing.T, msh *mesh.Mesh, id types.LayerID, numOfBlocks int, atxs []*types.ActivationTx, votes []types.BlockID, views []types.BlockID) (created []types.BlockID) {
	if numOfBlocks < len(atxs) {
		panic("not supported")
	}
	for i := 0; i < numOfBlocks; i++ {
		block1 := types.NewExistingBlock(id, []byte(rand.RandString(8)))
		block1.BlockVotes = append(block1.BlockVotes, votes...)
		if i < len(atxs) {
			block1.AtxIds = append(block1.AtxIds, atxs[i].Id())
			fmt.Printf("adding i=%v bid=%v atxid=%v", i, block1.Id(), atxs[i].ShortString())
		}
		block1.ViewEdges = append(block1.ViewEdges, views...)
		var actualAtxs []*types.ActivationTx
		if i < len(atxs) {
			actualAtxs = atxs[i : i+1]
		}
		block1.CalcAndSetId()
		err := msh.AddBlockWithTxs(block1, []*types.Transaction{}, actualAtxs)
		require.NoError(t, err)
		created = append(created, block1.Id())
	}
	return
}

func TestATX_ActiveSetForLayerView(t *testing.T) {
	rand.Seed(1234573298579)
	atxdb, layers, _ := getAtxDb(t.Name())
	blocksMap := make(map[types.BlockID]struct{})
	//layers.AtxDB = &AtxDbMock{make(map[types.AtxId]*types.ActivationTx), make(map[types.AtxId]*types.NIPST)}
	id1 := types.NodeId{Key: rndStr(), VRFPublicKey: []byte("anton")}
	id2 := types.NodeId{Key: rndStr(), VRFPublicKey: []byte("anton")}
	id3 := types.NodeId{Key: rndStr(), VRFPublicKey: []byte("anton")}
	id4 := types.NodeId{Key: rndStr(), VRFPublicKey: []byte("anton")}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	coinbase4 := types.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 2, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 3, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id2, coinbase2, 0, *types.EmptyAtxId, 2, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id4, coinbase4, 0, *types.EmptyAtxId, 2, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 11, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
	}

	poetRef := []byte{0xba, 0xb0}
	for _, atx := range atxs {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
		//layers.AtxDB.(*AtxDbMock).AddAtx(atx.Id(), atx)
	}
	id := atxs[4].Id()
	fmt.Println("ID4 ", id.ShortString())
	blocks := createLayerWithAtx(t, layers, 1, 6, atxs, []types.BlockID{}, []types.BlockID{})
	before := blocks[:4]
	four := blocks[4:5]
	after := blocks[5:]
	for i := 2; i <= 10; i++ {
		before = createLayerWithAtx(t, layers, types.LayerID(i), 1, []*types.ActivationTx{}, before, before)
		four = createLayerWithAtx(t, layers, types.LayerID(i), 1, []*types.ActivationTx{}, four, four)
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
	actives, err := atxdb.CalcActiveSetSize(epoch, blocksMap)
	assert.NoError(t, err)
	assert.Equal(t, 1, int(len(actives)))
	_, ok := actives[id2.Key]
	assert.True(t, ok)
}

func TestMesh_ActiveSetForLayerView2(t *testing.T) {
	atxdb, _, _ := getAtxDb(t.Name())
	actives, err := atxdb.CalcActiveSetSize(0, nil)
	assert.Error(t, err)
	assert.Equal(t, "tried to retrieve active set for epoch 0", err.Error())
	assert.Nil(t, actives)
}

func TestActivationDb_CalcActiveSetFromViewWithCache(t *testing.T) {
	activesetCache.Purge()
	atxdb, layers, _ := getAtxDb("t6")

	id1 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id2 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id3 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 12, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id2, coinbase2, 0, *types.EmptyAtxId, 300, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 435, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
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
	atx := types.NewActivationTx(id1, coinbase1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 3, blocks, &types.NIPST{})
	atxdb.calcActiveSetFunc = mck.CalcActiveSetSize
	wg := sync.WaitGroup{}
	wg.Add(100)
	mck.workSymLock.Lock()
	for i := 0; i < 100; i++ {
		go func() {
			num, err := atxdb.CalcActiveSetFromView(atx.View, atx.PubLayerIdx.GetEpoch(layersPerEpochBig))
			assert.NoError(t, err)
			assert.Equal(t, 3, int(num))
			assert.NoError(t, err)
			wg.Done()
		}()
	}
	mck.workSymLock.Unlock()
	wg.Wait()
	assert.Equal(t, 1, mck.counter)
	//mck.AssertNumberOfCalls(t, "CalcActiveSetSize", 1)
}

func Test_CalcActiveSetFromView(t *testing.T) {
	activesetCache.Purge()
	atxdb, layers, _ := getAtxDb("t6")

	id1 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id2 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id3 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 12, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id2, coinbase2, 0, *types.EmptyAtxId, 300, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 435, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
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

	atx := types.NewActivationTx(id1, coinbase1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 3, blocks, &types.NIPST{})
	num, err := atxdb.CalcActiveSetFromView(atx.View, atx.PubLayerIdx.GetEpoch(layersPerEpochBig))
	assert.NoError(t, err)
	assert.Equal(t, 3, int(num))

	// check that further atxs dont affect current epoch count
	atxs2 := []*types.ActivationTx{
		types.NewActivationTx(types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}, coinbase1, 0, *types.EmptyAtxId, 1012, 0, atxs[0].Id(), 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}, coinbase2, 0, *types.EmptyAtxId, 1300, 0, atxs[1].Id(), 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}, coinbase3, 0, *types.EmptyAtxId, 1435, 0, atxs[2].Id(), 0, []types.BlockID{}, &types.NIPST{}),
	}

	for _, atx := range atxs2 {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
	}

	block2 := types.NewExistingBlock(2200, []byte(rand.RandString(8)))

	block2.ViewEdges = blocks
	block2.CalcAndSetId()
	layers.AddBlockWithTxs(block2, nil, atxs2)

	block3 := types.NewExistingBlock(2200, []byte(rand.RandString(8)))

	block3.ViewEdges = blocks
	block2.CalcAndSetId()
	layers.AddBlockWithTxs(block3, nil, atxs2)

	err = atxdb.ProcessAtxs(atxs2)
	assert.NoError(t, err)

	view := []types.BlockID{block2.Id(), block3.Id()}
	sort.Slice(view, func(i, j int) bool {
		return bytes.Compare(view[i].ToBytes(), view[j].ToBytes()) > 0 // sort view in wrong order
	})
	atx2 := types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 1435, 0, *types.EmptyAtxId, 6, view, &types.NIPST{})
	num, err = atxdb.CalcActiveSetFromView(atx2.View, atx2.PubLayerIdx.GetEpoch(layersPerEpochBig))
	assert.NoError(t, err)
	assert.Equal(t, 3, int(num))

	// put a fake value in the cache and ensure that it's used
	viewHash, err := types.CalcBlocksHash12(atx2.View)
	assert.NoError(t, err)
	activesetCache.Purge()
	activesetCache.Add(viewHash, 8)

	num, err = atxdb.CalcActiveSetFromView(atx2.View, atx2.PubLayerIdx.GetEpoch(layersPerEpochBig))
	assert.NoError(t, err)
	assert.Equal(t, 8, int(num))

	// if the cache has the view in wrong order it should not be used
	sorted := sort.SliceIsSorted(atx2.View, func(i, j int) bool { return bytes.Compare(atx2.View[i].ToBytes(), atx2.View[j].ToBytes()) > 0 })
	assert.True(t, sorted) // assert that the view is wrongly ordered
	viewBytes, err := types.InterfaceToBytes(atx2.View)
	assert.NoError(t, err)
	viewHash = types.CalcHash12(viewBytes)
	activesetCache.Purge()
	activesetCache.Add(viewHash, 8)

	num, err = atxdb.CalcActiveSetFromView(atx2.View, atx2.PubLayerIdx.GetEpoch(layersPerEpochBig))
	assert.NoError(t, err)
	assert.Equal(t, 3, int(num))
}

func Test_DBSanity(t *testing.T) {
	atxdb, _, _ := getAtxDb("t6")

	id1 := types.NodeId{Key: uuid.New().String()}
	id2 := types.NodeId{Key: uuid.New().String()}
	id3 := types.NodeId{Key: uuid.New().String()}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")

	atx1 := types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{})
	atx2 := types.NewActivationTx(id1, coinbase2, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{})
	atx3 := types.NewActivationTx(id1, coinbase3, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{})

	err := atxdb.storeAtxUnlocked(atx1)
	assert.NoError(t, err)
	err = atxdb.storeAtxUnlocked(atx2)
	assert.NoError(t, err)
	err = atxdb.storeAtxUnlocked(atx3)
	assert.NoError(t, err)

	err = atxdb.addAtxToNodeId(id1, atx1)
	assert.NoError(t, err)
	id, err := atxdb.GetNodeLastAtxId(id1)
	assert.NoError(t, err)
	assert.Equal(t, atx1.Id(), id)

	err = atxdb.addAtxToNodeId(id2, atx2)
	assert.NoError(t, err)

	err = atxdb.addAtxToNodeId(id1, atx3)
	assert.NoError(t, err)

	id, err = atxdb.GetNodeLastAtxId(id2)
	assert.NoError(t, err)
	assert.Equal(t, atx2.Id(), id)

	id, err = atxdb.GetNodeLastAtxId(id1)
	assert.NoError(t, err)
	assert.Equal(t, atx3.Id(), id)

	id, err = atxdb.GetNodeLastAtxId(id3)
	assert.Error(t, err)
	assert.Equal(t, *types.EmptyAtxId, id)
}

func Test_Wrong_CalcActiveSetFromView(t *testing.T) {
	atxdb, layers, _ := getAtxDb("t6")

	id1 := types.NodeId{Key: uuid.New().String()}
	id2 := types.NodeId{Key: uuid.New().String()}
	id3 := types.NodeId{Key: uuid.New().String()}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id2, coinbase2, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	atx := types.NewActivationTx(id1, coinbase1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 20, blocks, &types.NIPST{})
	num, err := atxdb.CalcActiveSetFromView(atx.View, atx.PubLayerIdx.GetEpoch(layersPerEpoch))
	assert.NoError(t, err)
	assert.NotEqual(t, 20, int(num))

}

func TestMesh_processBlockATXs(t *testing.T) {
	activesetCache.Purge()
	atxdb, _, _ := getAtxDb("t6")

	id1 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id2 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id3 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0x76, 0x45}
	npst := NewNIPSTWithChallenge(&chlng, poetRef)
	posATX := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("anton")}, coinbase1, 0, *types.EmptyAtxId, 1000, 0, *types.EmptyAtxId, 0, []types.BlockID{}, npst)
	err := atxdb.StoreAtx(0, posATX)
	assert.NoError(t, err)
	atxs := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 1012, 0, posATX.Id(), 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id2, coinbase2, 0, *types.EmptyAtxId, 1300, 0, posATX.Id(), 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 1435, 0, posATX.Id(), 0, []types.BlockID{}, &types.NIPST{}),
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
		types.NewActivationTx(id1, coinbase1, 1, atxs[0].Id(), 2012, 0, atxs[0].Id(), 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id2, coinbase2, 1, atxs[1].Id(), 2300, 0, atxs[1].Id(), 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id3, coinbase3, 1, atxs[2].Id(), 2435, 0, atxs[2].Id(), 0, []types.BlockID{}, &types.NIPST{}),
	}
	for _, atx := range atxs2 {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
	}
	err = atxdb.ProcessAtxs(atxs2)
	assert.NoError(t, err)
}

func TestActivationDB_ValidateAtx(t *testing.T) {
	atxdb, layers, _ := getAtxDb("t8")

	signer := signing.NewEdSigner()
	idx1 := types.NodeId{Key: signer.PublicKey().String(), VRFPublicKey: []byte("anton")}

	id1 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id2 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id3 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase1 := types.HexToAddress("aaaa")
	coinbase2 := types.HexToAddress("bbbb")
	coinbase3 := types.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id2, coinbase2, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
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

	//atx := types.NewActivationTx(id1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 3, blocks, &Nipst.NIPST{})
	prevAtx := types.NewActivationTx(idx1, coinbase1, 0, *types.EmptyAtxId, 100, 0, *types.EmptyAtxId, 3, blocks, &types.NIPST{})
	hash, err := prevAtx.NIPSTChallenge.Hash()
	assert.NoError(t, err)
	prevAtx.Nipst = NewNIPSTWithChallenge(hash, poetRef)

	atx := types.NewActivationTx(idx1, coinbase1, 1, prevAtx.Id(), 1012, 0, prevAtx.Id(), 3, blocks, &types.NIPST{})
	hash, err = atx.NIPSTChallenge.Hash()
	assert.NoError(t, err)
	atx.Nipst = NewNIPSTWithChallenge(hash, poetRef)
	_, err = SignAtx(signer, atx)
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
	idx1 := types.NodeId{Key: signer.PublicKey().String()}
	idx2 := types.NodeId{Key: uuid.New().String()}
	coinbase := types.HexToAddress("aaaa")

	id1 := types.NodeId{Key: uuid.New().String()}
	id2 := types.NodeId{Key: uuid.New().String()}
	id3 := types.NodeId{Key: uuid.New().String()}
	atxs := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id2, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id3, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	//atx := types.NewActivationTx(id1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 3, blocks, &Nipst.NIPST{})
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xba, 0xbe}
	npst := NewNIPSTWithChallenge(&chlng, poetRef)
	prevAtx := types.NewActivationTx(idx1, coinbase, 0, *types.EmptyAtxId, 100, 0, *types.EmptyAtxId, 3, blocks, npst)
	posAtx := types.NewActivationTx(idx2, coinbase, 0, *types.EmptyAtxId, 100, 0, *types.EmptyAtxId, 3, blocks, npst)
	err := atxdb.StoreNodeIdentity(idx1)
	assert.NoError(t, err)
	err = atxdb.StoreAtx(1, prevAtx)
	assert.NoError(t, err)
	err = atxdb.StoreAtx(1, posAtx)
	assert.NoError(t, err)

	// Wrong sequence.
	atx := types.NewActivationTx(idx1, coinbase, 0, prevAtx.Id(), 1012, 0, posAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "sequence number is not one more than prev sequence number")

	// Wrong active set.
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 1012, 0, posAtx.Id(), 10, []types.BlockID{}, &types.NIPST{})
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "atx contains view with unequal active ids (10) than seen (0)")

	// Wrong positioning atx.
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 1012, 0, atxs[0].Id(), 3, []types.BlockID{}, &types.NIPST{})
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "expected distance of one epoch (1000 layers) from pos ATX but found 1011")

	// Wrong prevATx.
	atx = types.NewActivationTx(idx1, coinbase, 1, atxs[0].Id(), 1012, 0, posAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, fmt.Sprintf("previous ATX belongs to different miner. atx.Id: %v, atx.NodeId: %v, prevAtx.NodeId: %v", atx.ShortString(), atx.NodeId.Key, atxs[0].NodeId.Key))

	// Wrong layerId.
	posAtx2 := types.NewActivationTx(idx2, coinbase, 0, *types.EmptyAtxId, 1020, 0, *types.EmptyAtxId, 3, blocks, npst)
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.StoreAtx(1, posAtx2)
	assert.NoError(t, err)
	err = atxdb.StoreNodeIdentity(idx1)
	assert.NoError(t, err)
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 1012, 0, posAtx2.Id(), 3, []types.BlockID{}, npst)
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "atx layer (1012) must be after positioning atx layer (1020)")

	// Atx already exists.
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 12, 0, posAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")

	// Prev atx declared but not found.
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 12, 0, posAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	err = atxdb.atxs.Delete(getNodeIdKey(atx.NodeId))
	assert.NoError(t, err)
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	assert.EqualError(t, err, "could not fetch node last ATX: leveldb: not found")

	// Prev atx not declared but commitment not included.
	atx = types.NewActivationTx(idx1, coinbase, 0, *types.EmptyAtxId, 1012, 0, posAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "no prevATX declared, but commitment proof is not included")

	// Prev atx not declared but commitment merkle root not included.
	atx = types.NewActivationTx(idx1, coinbase, 0, *types.EmptyAtxId, 1012, 0, posAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	atx.Commitment = commitment
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "no prevATX declared, but commitment merkle root is not included in challenge")

	// Challenge and commitment merkle root mismatch.
	atx = types.NewActivationTx(idx1, coinbase, 0, *types.EmptyAtxId, 1012, 0, posAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	atx.Commitment = commitment
	atx.CommitmentMerkleRoot = append([]byte{}, commitment.MerkleRoot...)
	atx.CommitmentMerkleRoot[0] += 1
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "commitment merkle root included in challenge is not equal to the merkle root included in the proof")

	// Prev atx declared but commitment is included.
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 1012, 0, posAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	atx.Commitment = commitment
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "prevATX declared, but commitment proof is included")

	// Prev atx declared but commitment merkle root is included.
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 1012, 0, posAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	atx.CommitmentMerkleRoot = commitment.MerkleRoot
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "prevATX declared, but commitment merkle root is included in challenge")

	// Prev atx has publication layer in the same epoch as the atx.
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 100, 0, posAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "prevAtx epoch (0) isn't older than current atx epoch (0)")

	// NodeId and etracted pubkey dont match
	atx = types.NewActivationTx(idx2, coinbase, 0, *types.EmptyAtxId, 1012, 0, posAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	atx.Commitment = commitment
	atx.CommitmentMerkleRoot = append([]byte{}, commitment.MerkleRoot...)
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "node ids don't match")
}

func TestActivationDB_ValidateAndInsertSorted(t *testing.T) {
	atxdb, layers, _ := getAtxDb("t8")
	signer := signing.NewEdSigner()
	idx1 := types.NodeId{Key: signer.PublicKey().String(), VRFPublicKey: []byte("12345")}
	coinbase := types.HexToAddress("aaaa")

	id1 := types.NodeId{Key: uuid.New().String()}
	id2 := types.NodeId{Key: uuid.New().String()}
	id3 := types.NodeId{Key: uuid.New().String()}
	atxs := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id2, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id3, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	//atx := types.NewActivationTx(id1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 3, blocks, &Nipst.NIPST{})
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0x56, 0xbe}
	npst := NewNIPSTWithChallenge(&chlng, poetRef)
	prevAtx := types.NewActivationTx(idx1, coinbase, 0, *types.EmptyAtxId, 100, 0, *types.EmptyAtxId, 3, blocks, npst)

	var nodeAtxIds []types.AtxId

	err := atxdb.StoreAtx(1, prevAtx)
	assert.NoError(t, err)
	nodeAtxIds = append(nodeAtxIds, prevAtx.Id())

	//wrong sequnce
	atx := types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 1012, 0, prevAtx.Id(), 0, []types.BlockID{}, &types.NIPST{})
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	nodeAtxIds = append(nodeAtxIds, atx.Id())

	atx = types.NewActivationTx(idx1, coinbase, 2, atx.Id(), 1012, 0, atx.Id(), 0, []types.BlockID{}, &types.NIPST{})
	assert.NoError(t, err)
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.StoreNodeIdentity(idx1)
	assert.NoError(t, err)
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	nodeAtxIds = append(nodeAtxIds, atx.Id())
	atx2id := atx.Id()

	atx = types.NewActivationTx(idx1, coinbase, 4, prevAtx.Id(), 1012, 0, prevAtx.Id(), 0, []types.BlockID{}, &types.NIPST{})
	_, err = SignAtx(signer, atx)
	assert.NoError(t, err)
	err = atxdb.StoreNodeIdentity(idx1)
	assert.NoError(t, err)

	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "sequence number is not one more than prev sequence number")

	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	id4 := atx.Id()

	atx = types.NewActivationTx(idx1, coinbase, 3, atx2id, 1012, 0, prevAtx.Id(), 0, []types.BlockID{}, &types.NIPST{})
	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")

	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	nodeAtxIds = append(nodeAtxIds, atx.Id())
	nodeAtxIds = append(nodeAtxIds, id4)

	id, err := atxdb.GetNodeLastAtxId(idx1)
	assert.NoError(t, err)
	assert.Equal(t, atx.Id(), id)

	_, err = atxdb.GetAtxHeader(id)
	assert.NoError(t, err)

	_, err = atxdb.GetAtxHeader(atx2id)
	assert.NoError(t, err)

	//test same sequence
	idx2 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("12345")}

	prevAtx = types.NewActivationTx(idx2, coinbase, 0, *types.EmptyAtxId, 100, 0, *types.EmptyAtxId, 3, blocks, npst)
	err = atxdb.StoreAtx(1, prevAtx)
	assert.NoError(t, err)

	atx = types.NewActivationTx(idx2, coinbase, 1, prevAtx.Id(), 1012, 0, prevAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	atxId := atx.Id()

	atx = types.NewActivationTx(idx2, coinbase, 2, atxId, 1012, 0, atx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)

	atx = types.NewActivationTx(idx2, coinbase, 2, atxId, 1013, 0, atx.Id(), 0, []types.BlockID{}, &types.NIPST{})
	err = atxdb.ContextuallyValidateAtx(atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")

	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)

}

func TestActivationDb_ProcessAtx(t *testing.T) {
	r := require.New(t)

	atxdb, _, _ := getAtxDb("t8")
	idx1 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase := types.HexToAddress("aaaa")
	atx := types.NewActivationTx(idx1, coinbase, 0, *types.EmptyAtxId, 100, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{})

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
		id := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("vrf")}
		atxs = append(atxs, types.NewActivationTx(id, coinbase, 0, *types.EmptyAtxId, 1,
			0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}))
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

	idx1 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	challenge := newChallenge(idx1, 0, *types.EmptyAtxId, *types.EmptyAtxId, numberOfLayers+1)
	hash, err := challenge.Hash()
	r.NoError(err)
	prevAtx := newAtx(challenge, activesetSize, blocks, NewNIPSTWithChallenge(hash, poetRef))

	atx := types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), numberOfLayers+1+layersPerEpochBig, 0, prevAtx.Id(), activesetSize, blocks, &types.NIPST{})
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
	atxdb := NewActivationDb(store, NewIdentityStore(store), msh, layersPerEpochBig, &ValidatorMock{}, lg.WithName("atxDB"))

	const (
		numOfMiners = 300
		batchSize   = 15
		numOfEpochs = 10 * batchSize
	)
	prevAtxs := make([]types.AtxId, numOfMiners)
	pPrevAtxs := make([]types.AtxId, numOfMiners)
	posAtx := prevAtxId
	var atx *types.ActivationTx
	layer := types.LayerID(postGenesisEpochLayer)

	start := time.Now()
	eStart := time.Now()
	for epoch := postGenesisEpoch; epoch < postGenesisEpoch+numOfEpochs; epoch++ {
		for miner := 0; miner < numOfMiners; miner++ {
			challenge := newChallenge(nodeId, 1, prevAtxs[miner], posAtx, layer)
			h, err := challenge.Hash()
			r.NoError(err)
			atx = newAtx(challenge, numOfMiners, defaultView, NewNIPSTWithChallenge(h, poetRef))
			prevAtxs[miner] = atx.Id()
			storeAtx(r, atxdb, atx, log.NewDefault("storeAtx").WithOptions(log.Nop))
		}
		//noinspection GoNilness
		posAtx = atx.Id()
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
	r.Equal(atx.Id(), topAtx.AtxId)

	// higher-layer ATX stored should become new top ATX
	atx, err = createAndStoreAtx(atxdb, 3)
	r.NoError(err)

	topAtx, err = atxdb.getTopAtx()
	r.NoError(err)
	r.Equal(atx.Id(), topAtx.AtxId)

	// lower-layer ATX stored should NOT become new top ATX
	atx, err = createAndStoreAtx(atxdb, 1)
	r.NoError(err)

	topAtx, err = atxdb.getTopAtx()
	r.NoError(err)
	r.NotEqual(atx.Id(), topAtx.AtxId)
}

func createAndValidateSignedATX(r *require.Assertions, atxdb *ActivationDb, ed *signing.EdSigner, atx *types.ActivationTx) (*types.ActivationTx, error) {
	atxBytes, err := types.InterfaceToBytes(atx.InnerActivationTx)
	r.NoError(err)
	sig := ed.Sign(atxBytes)

	signedAtx := &types.ActivationTx{atx.InnerActivationTx, sig}
	return signedAtx, atxdb.ValidateSignedAtx(*ed.PublicKey(), signedAtx)
}

func TestActivationDb_ValidateSignedAtx(t *testing.T) {
	r := require.New(t)
	lg := log.NewDefault("sigValidation")
	idStore := NewIdentityStore(database.NewMemDatabase())
	memesh := mesh.NewMemMeshDB(lg.WithName("meshDB"))
	atxdb := NewActivationDb(database.NewMemDatabase(), idStore, memesh, layersPerEpochBig, &ValidatorMock{}, lg.WithName("atxDB"))

	ed := signing.NewEdSigner()
	nodeId := types.NodeId{ed.PublicKey().String(), []byte("bbbbb")}

	// test happy flow of first ATX
	emptyAtx := types.EmptyAtxId
	atx := types.NewActivationTx(nodeId, coinbase, 1, *emptyAtx, 15, 1, *emptyAtx, 5, []types.BlockID{block1.Id(), block2.Id(), block3.Id()}, npst)
	_, err := createAndValidateSignedATX(r, atxdb, ed, atx)
	r.NoError(err)

	// test negative flow no atx found in idstore
	prevAtx := types.AtxId(types.HexToHash32("0x111"))
	atx = types.NewActivationTx(nodeId, coinbase, 1, prevAtx, 15, 1, prevAtx, 5, []types.BlockID{block1.Id(), block2.Id(), block3.Id()}, npst)
	signedAtx, err := createAndValidateSignedATX(r, atxdb, ed, atx)
	r.Equal(errInvalidSig, err)

	// test happy flow not first ATX
	err = idStore.StoreNodeIdentity(nodeId)
	r.NoError(err)
	_, err = createAndValidateSignedATX(r, atxdb, ed, atx)
	r.NoError(err)

	// test negative flow not first ATX, invalid sig
	signedAtx.Sig = []byte("anton")
	_, err = ExtractPublicKey(signedAtx)
	r.Error(err)

}

func createAndStoreAtx(atxdb *ActivationDb, layer types.LayerID) (*types.ActivationTx, error) {
	id := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("vrf")}
	atx := types.NewActivationTx(id, coinbase, 0, *types.EmptyAtxId, layer,
		0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{})
	err := atxdb.StoreAtx(atx.TargetEpoch(layersPerEpoch), atx)
	if err != nil {
		return nil, err
	}
	return atx, nil
}
