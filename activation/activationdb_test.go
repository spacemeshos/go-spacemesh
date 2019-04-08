package activation

import (
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/block"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/stretchr/testify/assert"
	"math/big"
	"strconv"
	"testing"
)

func createLayerWithAtx(msh *mesh.Mesh, id block.LayerID, numOfBlocks int, atxs []*block.ActivationTx, votes []block.BlockID, views []block.BlockID) (created []block.BlockID) {
	for i := 0; i < numOfBlocks; i++ {
		block1 := block.NewExistingBlock(block.BlockID(uuid.New().ID()), id, []byte("data1"))
		block1.MinerID = strconv.Itoa(i)
		block1.ATXs = append(block1.ATXs, atxs...)
		block1.BlockVotes = append(block1.BlockVotes, votes...)
		block1.ViewEdges = append(block1.ViewEdges, views...)
		msh.AddBlock(block1)
		created = append(created, block1.Id)
	}
	return
}

type MeshValidatorMock struct{}

func (m *MeshValidatorMock) HandleIncomingLayer(layer *block.Layer) (block.LayerID, block.LayerID) {
	return layer.Index() - 1, layer.Index()
}
func (m *MeshValidatorMock) HandleLateBlock(bl *block.Block)              {}
func (m *MeshValidatorMock) RegisterLayerCallback(func(id block.LayerID)) {}
func (mlg *MeshValidatorMock) ContextualValidity(id block.BlockID) bool   { return true }

type MockState struct{}

func (MockState) ApplyTransactions(layer block.LayerID, txs mesh.Transactions) (uint32, error) {
	return 0, nil
}

func (MockState) ApplyRewards(layer block.LayerID, miners map[string]struct{}, underQuota map[string]struct{}, bonusReward, diminishedReward *big.Int) {
}

type AtxDbMock struct {}

func (AtxDbMock) ProcessBlockATXs(block *block.Block) {

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

func getAtxDb(id string) (*ActivationDb,  *mesh.Mesh){
	lg := log.New(id, "", "")
	memesh := mesh.NewMemMeshDB(lg)
	atxdb := NewActivationDb(database.NewMemDatabase(),memesh,1000)
	layers := mesh.NewMesh(memesh, atxdb, ConfigTst(), &MeshValidatorMock{}, &MockState{}, lg)
	return atxdb, layers
}

func Test_CalcActiveSetFromView(t *testing.T) {
	atxdb ,layers := getAtxDb("t6")


	id1 := block.NodeId{Key: uuid.New().String()}
	id2 := block.NodeId{Key: uuid.New().String()}
	id3 := block.NodeId{Key: uuid.New().String()}
	atxs := []*block.ActivationTx{
		block.NewActivationTx(id1, 0, block.EmptyAtx, 1, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
		block.NewActivationTx(id2, 0, block.EmptyAtx, 1, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
		block.NewActivationTx(id3, 0, block.EmptyAtx, 1, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
	}

	blocks := createLayerWithAtx(layers, 1, 10, atxs, []block.BlockID{}, []block.BlockID{})
	blocks = createLayerWithAtx(layers, 10, 10, []*block.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(layers, 100, 10, []*block.ActivationTx{}, blocks, blocks)

	atx := block.NewActivationTx(id1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 3, blocks, &nipst.NIPST{})
	num, err := atxdb.CalcActiveSetFromView(atx)
	assert.NoError(t, err)
	assert.Equal(t, 3, int(num))

	// check that further atxs dont affect current epoch count
	atxs2 := []*block.ActivationTx{
		block.NewActivationTx(id1, 0, block.EmptyAtx, 1012, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
		block.NewActivationTx(id2, 0, block.EmptyAtx, 1300, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
		block.NewActivationTx(id3, 0, block.EmptyAtx, 1435, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
	}

	block2 := block.NewExistingBlock(block.BlockID(uuid.New().ID()), 1200, []byte("data1"))
	block2.MinerID = strconv.Itoa(1)
	block2.ATXs = append(block2.ATXs, atxs2...)
	block2.ViewEdges = blocks
	layers.AddBlock(block2)

	atx2 := block.NewActivationTx(id3, 0, block.EmptyAtx, 1435, 0, block.EmptyAtx, 6, []block.BlockID {block2.Id}, &nipst.NIPST{})
	num, err = atxdb.CalcActiveSetFromView(atx2)
	assert.NoError(t, err)
	assert.Equal(t, 3, int(num))
}

func Test_Wrong_CalcActiveSetFromView(t *testing.T) {
	atxdb ,layers := getAtxDb("t6")

	id1 := block.NodeId{Key: uuid.New().String()}
	id2 := block.NodeId{Key: uuid.New().String()}
	id3 := block.NodeId{Key: uuid.New().String()}
	atxs := []*block.ActivationTx{
		block.NewActivationTx(id1, 0, block.EmptyAtx, 1, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
		block.NewActivationTx(id2, 0, block.EmptyAtx, 1, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
		block.NewActivationTx(id3, 0, block.EmptyAtx, 1, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
	}

	blocks := createLayerWithAtx(layers, 1, 10, atxs, []block.BlockID{}, []block.BlockID{})
	blocks = createLayerWithAtx(layers, 10, 10, []*block.ActivationTx{}, blocks, blocks)
	blocks = createLayerWithAtx(layers, 100, 10, []*block.ActivationTx{}, blocks, blocks)

	atx := block.NewActivationTx(id1, 1, atxs[0].Id(), 1000, 0, atxs[0].Id(), 20, blocks, &nipst.NIPST{})
	num, err := atxdb.CalcActiveSetFromView(atx)
	assert.NoError(t, err)
	assert.NotEqual(t, 20, int(num))

}

func TestMesh_processBlockATXs(t *testing.T) {
	atxdb ,_ := getAtxDb("t6")

	id1 := block.NodeId{Key: uuid.New().String()}
	id2 := block.NodeId{Key: uuid.New().String()}
	id3 := block.NodeId{Key: uuid.New().String()}
	atxs := []*block.ActivationTx{
		block.NewActivationTx(id1, 0, block.EmptyAtx, 1012, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
		block.NewActivationTx(id2, 0, block.EmptyAtx, 1300, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
		block.NewActivationTx(id3, 0, block.EmptyAtx, 1435, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
	}

	block1 := block.NewExistingBlock(block.BlockID(uuid.New().ID()), 1, []byte("data1"))
	block1.MinerID = strconv.Itoa(1)
	block1.ATXs = append(block1.ATXs, atxs...)

	atxdb.ProcessBlockATXs(block1)
	assert.Equal(t, 3, int(atxdb.ActiveIds(1)))

	// check that further atxs dont affect current epoch count
	atxs2 := []*block.ActivationTx{
		block.NewActivationTx(id1, 0, block.EmptyAtx, 2012, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
		block.NewActivationTx(id2, 0, block.EmptyAtx, 2300, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
		block.NewActivationTx(id3, 0, block.EmptyAtx, 2435, 0, block.EmptyAtx, 3, []block.BlockID{}, &nipst.NIPST{}),
	}

	block2 := block.NewExistingBlock(block.BlockID(uuid.New().ID()), 2000, []byte("data1"))
	block2.MinerID = strconv.Itoa(1)
	block2.ATXs = append(block1.ATXs, atxs2...)
	atxdb.ProcessBlockATXs(block2)

	assert.Equal(t, 3, int(atxdb.ActiveIds(1)))
	assert.Equal(t, 3, int(atxdb.ActiveIds(2)))
}

