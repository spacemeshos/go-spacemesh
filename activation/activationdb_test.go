package activation

import (
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"math/big"
	"strconv"
	"testing"
)

func createLayerWithAtx(msh *mesh.Mesh, atxdb *ActivationDb, id types.LayerID, numOfBlocks int, atxs []*types.ActivationTx, votes []types.BlockID, views []types.BlockID) (created []types.BlockID) {
	for i := 0; i < numOfBlocks; i++ {
		block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), id, []byte("data1"))
		block1.MinerID.Key = strconv.Itoa(i)
		block1.BlockVotes = append(block1.BlockVotes, votes...)
		for _, atx := range atxs {
			block1.AtxIds = append(block1.AtxIds, atx.Id())
		}
		block1.ViewEdges = append(block1.ViewEdges, views...)
		msh.AddBlockWithTxs(block1, []*types.SerializableTransaction{}, atxs)
		created = append(created, block1.Id)
		for _, atx := range atxs {
			atxdb.StoreAtx(atx.PubLayerIdx.GetEpoch(1000), atx)
		}
	}
	return
}

type MeshValidatorMock struct{}

func (m *MeshValidatorMock) HandleIncomingLayer(layer *types.Layer) (types.LayerID, types.LayerID) {
	return layer.Index() - 1, layer.Index()
}
func (m *MeshValidatorMock) HandleLateBlock(bl *types.Block)              {}
func (m *MeshValidatorMock) RegisterLayerCallback(func(id types.LayerID)) {}
func (mlg *MeshValidatorMock) ContextualValidity(id types.BlockID) bool   { return true }

type MockState struct{}

func (MockState) ApplyTransactions(layer types.LayerID, txs mesh.Transactions) (uint32, error) {
	return 0, nil
}

func (MockState) ApplyRewards(layer types.LayerID, miners []address.Address, underQuota map[address.Address]int, bonusReward, diminishedReward *big.Int) {
}

func (MockState) ValidateTransactionSignature(tx types.SerializableSignedTransaction) (address.Address, error) {
	return address.Address{}, nil
}

func (MockState) ValidateSignature(signed types.Signed) (address.Address, error) {
	return address.Address{}, nil
}

type AtxDbMock struct{}

func (AtxDbMock) ProcessBlockATXs(block *types.Block) {

}

type MemPoolMock struct {
}

func (mem *MemPoolMock) Get(id interface{}) interface{} {
	return nil
}

func (mem *MemPoolMock) PopItems(size int) interface{} {
	return nil
}

func (mem *MemPoolMock) Put(id interface{}, item interface{}) {
}

func (mem *MemPoolMock) Invalidate(id interface{}) {
}

func ConfigTst() mesh.Config {
	return mesh.Config{
		big.NewInt(10),
		big.NewInt(5000),
		big.NewInt(15),
		15,
		5,
	}
}

func getAtxDb(id string) (*ActivationDb, *mesh.Mesh) {
	lg := log.NewDefault(id)
	memesh := mesh.NewMemMeshDB(lg.WithName("meshDB"))
	atxdb := NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), NewIdentityStore(database.NewMemDatabase()), memesh, 1000, &ValidatorMock{}, lg.WithName("atxDB"))
	layers := mesh.NewMesh(memesh, atxdb, ConfigTst(), &MeshValidatorMock{}, &MemPoolMock{}, &MemPoolMock{}, &MockState{}, lg.WithName("mesh"))
	return atxdb, layers
}

func Test_CalcActiveSetFromView(t *testing.T) {
	activesetCache.Purge()
	atxdb, layers := getAtxDb("t6")

	id1 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id2 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id3 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase1 := address.HexToAddress("aaaa")
	coinbase2 := address.HexToAddress("bbbb")
	coinbase3 := address.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 12, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(id2, coinbase2, 0, *types.EmptyAtxId, 300, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 435, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}, true, ),
	}

	for _, atx := range atxs {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = nipst.NewNIPSTWithChallenge(hash)
	}

	blocks := createLayerWithAtx(layers, atxdb, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(layers, atxdb, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(layers, atxdb, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	atx := types.NewActivationTx(id1, coinbase1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 3, blocks, &types.NIPST{}, true, )
	num, err := atxdb.CalcActiveSetFromView(atx)
	assert.NoError(t, err)
	assert.Equal(t, 3, int(num))

	// check that further atxs dont affect current epoch count
	atxs2 := []*types.ActivationTx{
		types.NewActivationTx(types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}, coinbase1, 0, *types.EmptyAtxId, 1012, 0, atxs[0].Id(), 0, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}, coinbase2, 0, *types.EmptyAtxId, 1300, 0, atxs[1].Id(), 0, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}, coinbase3, 0, *types.EmptyAtxId, 1435, 0, atxs[2].Id(), 0, []types.BlockID{}, &types.NIPST{}, true, ),
	}

	for _, atx := range atxs2 {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = nipst.NewNIPSTWithChallenge(hash)
	}

	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 2200, []byte("data1"))
	block2.MinerID.Key = strconv.Itoa(1)
	block2.ViewEdges = blocks
	layers.AddBlockWithTxs(block2, nil, atxs2)

	for _, t := range atxs2 {
		atxdb.ProcessAtx(t)
	}

	atx2 := types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 1435, 0, *types.EmptyAtxId, 6, []types.BlockID{block2.Id}, &types.NIPST{}, true, )
	num, err = atxdb.CalcActiveSetFromView(atx2)
	assert.NoError(t, err)
	assert.Equal(t, 3, int(num))
}

func Test_DBSanity(t *testing.T) {
	atxdb, _ := getAtxDb("t6")

	id1 := types.NodeId{Key: uuid.New().String()}
	id2 := types.NodeId{Key: uuid.New().String()}
	id3 := types.NodeId{Key: uuid.New().String()}
	coinbase1 := address.HexToAddress("aaaa")
	coinbase2 := address.HexToAddress("bbbb")
	coinbase3 := address.HexToAddress("cccc")

	atx1 := types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, )
	atx2 := types.NewActivationTx(id1, coinbase2, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, )
	atx3 := types.NewActivationTx(id1, coinbase3, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, )

	err := atxdb.storeAtxUnlocked(atx1)
	assert.NoError(t, err)
	err = atxdb.storeAtxUnlocked(atx2)
	assert.NoError(t, err)
	err = atxdb.storeAtxUnlocked(atx3)
	assert.NoError(t, err)

	err = atxdb.addAtxToNodeIdSorted(id1, atx1)
	assert.NoError(t, err)
	ids, err := atxdb.GetNodeAtxIds(id1)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(ids))
	assert.Equal(t, atx1.Id(), ids[0])

	err = atxdb.addAtxToNodeIdSorted(id2, atx2)
	assert.NoError(t, err)

	err = atxdb.addAtxToNodeIdSorted(id1, atx3)
	assert.NoError(t, err)

	ids, err = atxdb.GetNodeAtxIds(id2)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(ids))
	assert.Equal(t, atx2.Id(), ids[0])

	ids, err = atxdb.GetNodeAtxIds(id1)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(ids))
	assert.Equal(t, atx1.Id(), ids[0])

	ids, err = atxdb.GetNodeAtxIds(id3)
	assert.Error(t, err)
	assert.Equal(t, 0, len(ids))
}

func Test_Wrong_CalcActiveSetFromView(t *testing.T) {
	atxdb, layers := getAtxDb("t6")

	id1 := types.NodeId{Key: uuid.New().String()}
	id2 := types.NodeId{Key: uuid.New().String()}
	id3 := types.NodeId{Key: uuid.New().String()}
	coinbase1 := address.HexToAddress("aaaa")
	coinbase2 := address.HexToAddress("bbbb")
	coinbase3 := address.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(id2, coinbase2, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, ),
	}

	blocks := createLayerWithAtx(layers, atxdb, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(layers, atxdb, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(layers, atxdb, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	atx := types.NewActivationTx(id1, coinbase1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 20, blocks, &types.NIPST{}, true, )
	num, err := atxdb.CalcActiveSetFromView(atx)
	assert.NoError(t, err)
	assert.NotEqual(t, 20, int(num))

}

func TestMesh_processBlockATXs(t *testing.T) {
	activesetCache.Purge()
	atxdb, _ := getAtxDb("t6")

	id1 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id2 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id3 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase1 := address.HexToAddress("aaaa")
	coinbase2 := address.HexToAddress("bbbb")
	coinbase3 := address.HexToAddress("cccc")
	chlng := common.HexToHash("0x3333")
	npst := nipst.NewNIPSTWithChallenge(&chlng)
	posATX := types.NewActivationTx(types.NodeId{"aaaaaa", []byte("anton")}, coinbase1, 0, *types.EmptyAtxId, 1000, 0, *types.EmptyAtxId, 0, []types.BlockID{}, npst, true, )
	err := atxdb.StoreAtx(0, posATX)
	assert.NoError(t, err)
	atxs := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 1012, 0, posATX.Id(), 0, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(id2, coinbase2, 0, *types.EmptyAtxId, 1300, 0, posATX.Id(), 0, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 1435, 0, posATX.Id(), 0, []types.BlockID{}, &types.NIPST{}, true, ),
	}
	for _, atx := range atxs {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = nipst.NewNIPSTWithChallenge(hash)
	}

	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data1"))
	block1.MinerID.Key = strconv.Itoa(1)
	for _, t := range atxs {
		atxdb.ProcessAtx(t)
	}
	i, err := atxdb.ActiveSetSize(1)
	assert.NoError(t, err)
	assert.Equal(t, uint32(3), i)

	atxdb.ProcessAtx(atxs[0])
	atxdb.ProcessAtx(atxs[1])
	atxdb.ProcessAtx(atxs[2])
	activeSetSize, err := atxdb.ActiveSetSize(1)
	assert.NoError(t, err)
	assert.Equal(t, 3, int(activeSetSize))

	// check that further atxs dont affect current epoch count
	atxs2 := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase1, 1, atxs[0].Id(), 2012, 0, atxs[0].Id(), 0, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(id2, coinbase2, 1, atxs[1].Id(), 2300, 0, atxs[1].Id(), 0, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(id3, coinbase3, 1, atxs[2].Id(), 2435, 0, atxs[2].Id(), 0, []types.BlockID{}, &types.NIPST{}, true, ),
	}
	for _, atx := range atxs2 {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = nipst.NewNIPSTWithChallenge(hash)
	}

	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 2000, []byte("data1"))
	block2.MinerID.Key = strconv.Itoa(1)
	for _, t := range atxs2 {
		atxdb.ProcessAtx(t)
	}

	activeSetSize, err = atxdb.ActiveSetSize(1)
	assert.NoError(t, err)
	assert.Equal(t, 3, int(activeSetSize))
	activeSetSize, err = atxdb.ActiveSetSize(2)
	assert.NoError(t, err)
	assert.Equal(t, 3, int(activeSetSize))
}

func TestActivationDB_ValidateAtx(t *testing.T) {
	atxdb, layers := getAtxDb("t8")

	idx1 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}

	id1 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id2 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	id3 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase1 := address.HexToAddress("aaaa")
	coinbase2 := address.HexToAddress("bbbb")
	coinbase3 := address.HexToAddress("cccc")
	atxs := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(id2, coinbase2, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, ),
	}
	for _, atx := range atxs {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = nipst.NewNIPSTWithChallenge(hash)
	}

	blocks := createLayerWithAtx(layers, atxdb, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(layers, atxdb, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(layers, atxdb, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	//atx := types.NewActivationTx(id1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 3, blocks, &nipst.NIPST{})
	prevAtx := types.NewActivationTx(idx1, coinbase1, 0, *types.EmptyAtxId, 100, 0, *types.EmptyAtxId, 3, blocks, &types.NIPST{}, true, )
	prevAtx.Valid = true
	hash, err := prevAtx.NIPSTChallenge.Hash()
	assert.NoError(t, err)
	prevAtx.Nipst = nipst.NewNIPSTWithChallenge(hash)

	atx := types.NewActivationTx(idx1, coinbase1, 1, prevAtx.Id(), 1012, 0, prevAtx.Id(), 3, []types.BlockID{}, &types.NIPST{}, true, )
	atx.VerifiedActiveSet = 3
	hash, err = atx.NIPSTChallenge.Hash()
	assert.NoError(t, err)
	atx.Nipst = nipst.NewNIPSTWithChallenge(hash)
	err = atxdb.StoreAtx(1, prevAtx)
	assert.NoError(t, err)

	err = atxdb.ValidateAtx(atx)
	assert.NoError(t, err)
}

func TestActivationDB_ValidateAtxErrors(t *testing.T) {
	atxdb, layers := getAtxDb("t8")

	idx1 := types.NodeId{Key: uuid.New().String()}
	coinbase := address.HexToAddress("aaaa")

	id1 := types.NodeId{Key: uuid.New().String()}
	id2 := types.NodeId{Key: uuid.New().String()}
	id3 := types.NodeId{Key: uuid.New().String()}
	atxs := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(id2, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(id3, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, ),
	}

	blocks := createLayerWithAtx(layers, atxdb, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(layers, atxdb, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(layers, atxdb, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	//atx := types.NewActivationTx(id1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 3, blocks, &nipst.NIPST{})
	chlng := common.HexToHash("0x3333")
	npst := nipst.NewNIPSTWithChallenge(&chlng)
	prevAtx := types.NewActivationTx(idx1, coinbase, 0, *types.EmptyAtxId, 100, 0, *types.EmptyAtxId, 3, blocks, npst, true, )
	prevAtx.Valid = true

	err := atxdb.StoreAtx(1, prevAtx)
	assert.NoError(t, err)

	//todo: can test against exact error strings
	//wrong sequnce
	atx := types.NewActivationTx(idx1, coinbase, 0, prevAtx.Id(), 1012, 0, prevAtx.Id(), 3, []types.BlockID{}, &types.NIPST{}, true, )
	err = atxdb.ValidateAtx(atx)
	assert.Error(t, err)

	//wrong active set
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 1012, 0, prevAtx.Id(), 10, []types.BlockID{}, &types.NIPST{}, true, )
	err = atxdb.ValidateAtx(atx)
	assert.Error(t, err)

	//wrong positioning atx
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 1012, 0, atxs[0].Id(), 3, []types.BlockID{}, &types.NIPST{}, true, )
	err = atxdb.ValidateAtx(atx)
	assert.Error(t, err)

	//wrong prevATx
	atx = types.NewActivationTx(idx1, coinbase, 1, atxs[0].Id(), 1012, 0, prevAtx.Id(), 3, []types.BlockID{}, &types.NIPST{}, true, )
	err = atxdb.ValidateAtx(atx)
	assert.Error(t, err)

	//wrong layerId
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 12, 0, prevAtx.Id(), 3, []types.BlockID{}, npst, true, )
	err = atxdb.ValidateAtx(atx)
	assert.Error(t, err)

	//atx already exists
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 12, 0, prevAtx.Id(), 3, []types.BlockID{}, &types.NIPST{}, true, )
	err = atxdb.ValidateAtx(atx)
	assert.Error(t, err)
	//atx = types.NewActivationTx(idx1, 1, prevAtx.Id(), 1012, 0, prevAtx.Id(), 3, []types.BlockID{}, &nipst.NIPST{})
}

func TestActivationDB_ValidateAndInsertSorted(t *testing.T) {
	atxdb, layers := getAtxDb("t8")

	idx1 := types.NodeId{Key: uuid.New().String()}
	coinbase := address.HexToAddress("aaaa")

	id1 := types.NodeId{Key: uuid.New().String()}
	id2 := types.NodeId{Key: uuid.New().String()}
	id3 := types.NodeId{Key: uuid.New().String()}
	atxs := []*types.ActivationTx{
		types.NewActivationTx(id1, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(id2, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, ),
		types.NewActivationTx(id3, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, ),
	}

	blocks := createLayerWithAtx(layers, atxdb, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(layers, atxdb, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(layers, atxdb, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	//atx := types.NewActivationTx(id1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 3, blocks, &nipst.NIPST{})
	chlng := common.HexToHash("0x3333")
	npst := nipst.NewNIPSTWithChallenge(&chlng)
	prevAtx := types.NewActivationTx(idx1, coinbase, 0, *types.EmptyAtxId, 100, 0, *types.EmptyAtxId, 3, blocks, npst, true, )
	prevAtx.Valid = true

	var nodeAtxIds []types.AtxId

	err := atxdb.StoreAtx(1, prevAtx)
	assert.NoError(t, err)
	nodeAtxIds = append(nodeAtxIds, prevAtx.Id())

	//wrong sequnce
	atx := types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 1012, 0, prevAtx.Id(), 0, []types.BlockID{}, &types.NIPST{}, true, )
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	nodeAtxIds = append(nodeAtxIds, atx.Id())

	atx = types.NewActivationTx(idx1, coinbase, 2, atx.Id(), 1012, 0, atx.Id(), 0, []types.BlockID{}, &types.NIPST{}, true, )
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	nodeAtxIds = append(nodeAtxIds, atx.Id())
	atx2id := atx.Id()

	atx = types.NewActivationTx(idx1, coinbase, 4, atx.Id(), 1012, 0, prevAtx.Id(), 0, []types.BlockID{}, &types.NIPST{}, true, )
	err = atxdb.ValidateAtx(atx)
	assert.Error(t, err)
	assert.Equal(t, "sequence number is not one more than prev sequence number", err.Error())

	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	id4 := atx.Id()

	atx = types.NewActivationTx(idx1, coinbase, 3, atx2id, 1012, 0, prevAtx.Id(), 3, []types.BlockID{}, &types.NIPST{}, true, )
	err = atxdb.ValidateAtx(atx)
	assert.Error(t, err)
	assert.Equal(t, "last atx is not the one referenced", err.Error())

	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	nodeAtxIds = append(nodeAtxIds, atx.Id())
	nodeAtxIds = append(nodeAtxIds, id4)

	ids, err := atxdb.GetNodeAtxIds(idx1)
	assert.True(t, len(ids) == 5)
	assert.Equal(t, ids, nodeAtxIds)

	_, err = atxdb.GetAtx(ids[len(ids)-1])
	assert.NoError(t, err)

	_, err = atxdb.GetAtx(ids[len(ids)-2])
	assert.NoError(t, err)

	//test same sequence
	idx2 := types.NodeId{Key: uuid.New().String()}

	prevAtx = types.NewActivationTx(idx2, coinbase, 0, *types.EmptyAtxId, 100, 0, *types.EmptyAtxId, 3, blocks, npst, true, )
	prevAtx.Valid = true
	err = atxdb.StoreAtx(1, prevAtx)
	assert.NoError(t, err)

	atx = types.NewActivationTx(idx2, coinbase, 1, prevAtx.Id(), 1012, 0, prevAtx.Id(), 3, []types.BlockID{}, &types.NIPST{}, true, )
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	atxId := atx.Id()

	atx = types.NewActivationTx(idx2, coinbase, 2, atxId, 1012, 0, atx.Id(), 3, []types.BlockID{}, &types.NIPST{}, true, )
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)

	atx = types.NewActivationTx(idx2, coinbase, 2, atxId, 1013, 0, atx.Id(), 3, []types.BlockID{}, &types.NIPST{}, true, )
	err = atxdb.ValidateAtx(atx)
	assert.Error(t, err)
	assert.Equal(t, "last atx is not the one referenced", err.Error())

	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)

}

func TestActivationDb_ProcessAtx(t *testing.T) {
	atxdb, _ := getAtxDb("t8")
	idx1 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase := address.HexToAddress("aaaa")
	atx := types.NewActivationTx(idx1, coinbase,0, *types.EmptyAtxId, 100, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}, true, )
	atxdb.ProcessAtx(atx)
	res, err := atxdb.ids.GetIdentity(idx1.Key)
	assert.Nil(t, err)
	assert.Equal(t, idx1, res)
}
