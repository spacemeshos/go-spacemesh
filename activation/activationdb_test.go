package activation

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/sync"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
	"time"
)

func createLayerWithAtx(t require.TestingT, msh *mesh.Mesh, id types.LayerID, numOfBlocks int, atxs []*types.ActivationTx, votes []types.BlockID, views []types.BlockID) (created []types.BlockID) {
	for i := 0; i < numOfBlocks; i++ {
		block1 := types.NewExistingBlock(types.BlockID(binary.BigEndian.Uint64([]byte(uuid.New().String()[:8]))), id, []byte("data1"))
		block1.BlockVotes = append(block1.BlockVotes, votes...)
		for _, atx := range atxs {
			block1.AtxIds = append(block1.AtxIds, atx.Id())
		}
		block1.ViewEdges = append(block1.ViewEdges, views...)
		err := msh.AddBlockWithTxs(block1, []*types.AddressableSignedTransaction{}, atxs)
		require.NoError(t, err)
		created = append(created, block1.Id)
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

func (MockState) ApplyTransactions(layer types.LayerID, txs mesh.Transactions) (uint32, error) {
	return 0, nil
}

func (MockState) ApplyRewards(layer types.LayerID, miners []address.Address, underQuota map[address.Address]int, bonusReward, diminishedReward *big.Int) {
}

func (MockState) ValidateTransactionSignature(tx *types.SerializableSignedTransaction) (address.Address, error) {
	return address.Address{}, nil
}

func (MockState) ValidateSignature(signed types.Signed) (address.Address, error) {
	return address.Address{}, nil
}

type ATXDBMock struct {
	activeSet uint32
}

func (mock *ATXDBMock) CalcActiveSetFromView(a *types.ActivationTx) (uint32, error) {
	return mock.activeSet, nil
}

func (mock *ATXDBMock) GetAtx(id types.AtxId) (*types.ActivationTxHeader, error) {
	panic("not implemented")
}

func (mock *ATXDBMock) GetEpochAtxIds(epochId types.EpochId) ([]types.AtxId, error) {
	panic("not implemented")
}

func (mock *ATXDBMock) GetNodeAtxIds(nodeId types.NodeId) ([]types.AtxId, error) {
	panic("not implemented")
}

func (mock *ATXDBMock) GetPrevAtxId(node types.NodeId) (*types.AtxId, error) {
	panic("not implemented")
}

func ConfigTst() mesh.Config {
	return mesh.Config{
		SimpleTxCost:   big.NewInt(10),
		BaseReward:     big.NewInt(5000),
		PenaltyPercent: big.NewInt(15),
		TxQuota:        15,
		RewardMaturity: 5,
	}
}

const layersPerEpochBig = 1000

func getAtxDb(id string) (*ActivationDb, *mesh.Mesh) {
	lg := log.NewDefault(id)
	memesh := mesh.NewMemMeshDB(lg.WithName("meshDB"))
	atxdb := NewActivationDb(database.NewMemDatabase(), NewIdentityStore(database.NewMemDatabase()), memesh, layersPerEpochBig, &ValidatorMock{}, lg.WithName("atxDB"))
	layers := mesh.NewMesh(memesh, atxdb, ConfigTst(), &MeshValidatorMock{}, &sync.MockTxMemPool{}, &sync.MockAtxMemPool{}, &MockState{}, lg.WithName("mesh"))
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
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 12, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id2, coinbase2, 0, *types.EmptyAtxId, 300, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 435, 0, *types.EmptyAtxId, 0, []types.BlockID{}, &types.NIPST{}),
	}

	poetRef := []byte{0xba, 0xb0}
	for _, atx := range atxs {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = nipst.NewNIPSTWithChallenge(hash, poetRef)
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	atx := types.NewActivationTx(id1, coinbase1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 3, blocks, &types.NIPST{})
	num, err := atxdb.CalcActiveSetFromView(atx)
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
		atx.Nipst = nipst.NewNIPSTWithChallenge(hash, poetRef)
	}

	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 2200, []byte("data1"))

	block2.ViewEdges = blocks
	layers.AddBlockWithTxs(block2, nil, atxs2)

	for _, t := range atxs2 {
		atxdb.ProcessAtx(t)
	}

	atx2 := types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 1435, 0, *types.EmptyAtxId, 6, []types.BlockID{block2.Id}, &types.NIPST{})
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

	atx1 := types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{})
	atx2 := types.NewActivationTx(id1, coinbase2, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{})
	atx3 := types.NewActivationTx(id1, coinbase3, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{})

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
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id2, coinbase2, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	atx := types.NewActivationTx(id1, coinbase1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 20, blocks, &types.NIPST{})
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
	poetRef := []byte{0x76, 0x45}
	npst := nipst.NewNIPSTWithChallenge(&chlng, poetRef)
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
		atx.Nipst = nipst.NewNIPSTWithChallenge(hash, poetRef)
	}

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
		types.NewActivationTx(id1, coinbase1, 1, atxs[0].Id(), 2012, 0, atxs[0].Id(), 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id2, coinbase2, 1, atxs[1].Id(), 2300, 0, atxs[1].Id(), 0, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id3, coinbase3, 1, atxs[2].Id(), 2435, 0, atxs[2].Id(), 0, []types.BlockID{}, &types.NIPST{}),
	}
	for _, atx := range atxs2 {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = nipst.NewNIPSTWithChallenge(hash, poetRef)
	}
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
		types.NewActivationTx(id1, coinbase1, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id2, coinbase2, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id3, coinbase3, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
	}
	poetRef := []byte{0x12, 0x21}
	for _, atx := range atxs {
		hash, err := atx.NIPSTChallenge.Hash()
		assert.NoError(t, err)
		atx.Nipst = nipst.NewNIPSTWithChallenge(hash, poetRef)
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	//atx := types.NewActivationTx(id1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 3, blocks, &nipst.NIPST{})
	prevAtx := types.NewActivationTx(idx1, coinbase1, 0, *types.EmptyAtxId, 100, 0, *types.EmptyAtxId, 3, blocks, &types.NIPST{})
	hash, err := prevAtx.NIPSTChallenge.Hash()
	assert.NoError(t, err)
	prevAtx.Nipst = nipst.NewNIPSTWithChallenge(hash, poetRef)

	atx := types.NewActivationTx(idx1, coinbase1, 1, prevAtx.Id(), 1012, 0, prevAtx.Id(), 3, blocks, &types.NIPST{})
	hash, err = atx.NIPSTChallenge.Hash()
	assert.NoError(t, err)
	atx.Nipst = nipst.NewNIPSTWithChallenge(hash, poetRef)
	err = atxdb.StoreAtx(1, prevAtx)
	assert.NoError(t, err)

	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.NoError(t, err)

	err = atxdb.ContextuallyValidateAtx(&atx.ActivationTxHeader)
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
		types.NewActivationTx(id1, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id2, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
		types.NewActivationTx(id3, coinbase, 0, *types.EmptyAtxId, 1, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{}),
	}

	blocks := createLayerWithAtx(t, layers, 1, 10, atxs, []types.BlockID{}, []types.BlockID{})
	blocks = createLayerWithAtx(t, layers, 10, 10, []*types.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(t, layers, 100, 10, []*types.ActivationTx{}, blocks, blocks)

	//atx := types.NewActivationTx(id1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 3, blocks, &nipst.NIPST{})
	chlng := common.HexToHash("0x3333")
	poetRef := []byte{0xba, 0xbe}
	npst := nipst.NewNIPSTWithChallenge(&chlng, poetRef)
	prevAtx := types.NewActivationTx(idx1, coinbase, 0, *types.EmptyAtxId, 100, 0, *types.EmptyAtxId, 3, blocks, npst)

	err := atxdb.StoreAtx(1, prevAtx)
	assert.NoError(t, err)

	//todo: can test against exact error strings
	//wrong sequnce
	atx := types.NewActivationTx(idx1, coinbase, 0, prevAtx.Id(), 1012, 0, prevAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "sequence number is not one more than prev sequence number")

	//wrong active set
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 1012, 0, prevAtx.Id(), 10, []types.BlockID{}, &types.NIPST{})
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "atx contains view with unequal active ids (10) than seen (0)")

	//wrong positioning atx
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 1012, 0, atxs[0].Id(), 3, []types.BlockID{}, &types.NIPST{})
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "expected distance of one epoch (1000 layers) from pos ATX but found 1011")

	//wrong prevATx
	atx = types.NewActivationTx(idx1, coinbase, 1, atxs[0].Id(), 1012, 0, prevAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, fmt.Sprintf("previous ATX belongs to different miner. atx.Id: %v, atx.NodeId: %v, prevAtx.NodeId: %v", atx.ShortId(), atx.NodeId.Key, atxs[0].NodeId.Key))

	//wrong layerId
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 12, 0, prevAtx.Id(), 3, []types.BlockID{}, npst)
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "atx layer (12) must be after positioning atx layer (100)")

	//atx already exists
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	atx = types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), 12, 0, prevAtx.Id(), 3, []types.BlockID{}, &types.NIPST{})
	err = atxdb.ContextuallyValidateAtx(&atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")
}

func TestActivationDB_ValidateAndInsertSorted(t *testing.T) {
	atxdb, layers := getAtxDb("t8")

	idx1 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("12345")}
	coinbase := address.HexToAddress("aaaa")

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

	//atx := types.NewActivationTx(id1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 3, blocks, &nipst.NIPST{})
	chlng := common.HexToHash("0x3333")
	poetRef := []byte{0x56, 0xbe}
	npst := nipst.NewNIPSTWithChallenge(&chlng, poetRef)
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
	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	nodeAtxIds = append(nodeAtxIds, atx.Id())
	atx2id := atx.Id()

	atx = types.NewActivationTx(idx1, coinbase, 4, atx.Id(), 1012, 0, prevAtx.Id(), 0, []types.BlockID{}, &types.NIPST{})
	err = atxdb.SyntacticallyValidateAtx(atx)
	assert.EqualError(t, err, "sequence number is not one more than prev sequence number")

	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)
	id4 := atx.Id()

	atx = types.NewActivationTx(idx1, coinbase, 3, atx2id, 1012, 0, prevAtx.Id(), 0, []types.BlockID{}, &types.NIPST{})
	err = atxdb.ContextuallyValidateAtx(&atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")

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
	err = atxdb.ContextuallyValidateAtx(&atx.ActivationTxHeader)
	assert.EqualError(t, err, "last atx is not the one referenced")

	err = atxdb.StoreAtx(1, atx)
	assert.NoError(t, err)

}

func TestActivationDb_ProcessAtx(t *testing.T) {
	atxdb, _ := getAtxDb("t8")
	idx1 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	coinbase := address.HexToAddress("aaaa")
	atx := types.NewActivationTx(idx1, coinbase, 0, *types.EmptyAtxId, 100, 0, *types.EmptyAtxId, 3, []types.BlockID{}, &types.NIPST{})
	atxdb.ProcessAtx(atx)
	res, err := atxdb.GetIdentity(idx1.Key)
	assert.Nil(t, err)
	assert.Equal(t, idx1, res)
}

func BenchmarkActivationDb_SyntacticallyValidateAtx(b *testing.B) {
	r := require.New(b)
	nopLogger := log.NewDefault("").WithOptions(log.Nop)

	atxdb, layers := getAtxDb("t8")
	atxdb.log = nopLogger
	layers.Log = nopLogger

	const (
		activesetSize  = 300
		blocksPerLayer = 200
		numberOfLayers = 100
	)

	coinbase := address.HexToAddress("c012ba5e")
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
		atx.Nipst = nipst.NewNIPSTWithChallenge(hash, poetRef)
	}

	blocks := createLayerWithAtx(b, layers, 0, blocksPerLayer, atxs, []types.BlockID{}, []types.BlockID{})
	for i := 1; i < numberOfLayers; i++ {
		blocks = createLayerWithAtx(b, layers, types.LayerID(i), blocksPerLayer, []*types.ActivationTx{}, blocks, blocks)
	}

	idx1 := types.NodeId{Key: uuid.New().String(), VRFPublicKey: []byte("anton")}
	challenge := newChallenge(idx1, 0, *types.EmptyAtxId, *types.EmptyAtxId, numberOfLayers+1)
	hash, err := challenge.Hash()
	r.NoError(err)
	prevAtx := newAtx(challenge, activesetSize, blocks, nipst.NewNIPSTWithChallenge(hash, poetRef))

	atx := types.NewActivationTx(idx1, coinbase, 1, prevAtx.Id(), numberOfLayers+1+layersPerEpochBig, 0, prevAtx.Id(), activesetSize, blocks, &types.NIPST{})
	hash, err = atx.NIPSTChallenge.Hash()
	r.NoError(err)
	atx.Nipst = nipst.NewNIPSTWithChallenge(hash, poetRef)
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
	err = atxdb.ContextuallyValidateAtx(&atx.ActivationTxHeader)
	fmt.Printf("\nContextual validation took %v\n\n", time.Since(start))
	r.NoError(err)
}
