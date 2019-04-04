package mesh

import (
	"bytes"
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"math/big"
	"testing"
	"time"
)

type MeshValidatorMock struct{}

func (m *MeshValidatorMock) HandleIncomingLayer(layer *Layer) (LayerID, LayerID) {
	return layer.Index() - 1, layer.Index()
}
func (m *MeshValidatorMock) HandleLateBlock(bl *Block)              {}
func (m *MeshValidatorMock) RegisterLayerCallback(func(id LayerID)) {}
func (mlg *MeshValidatorMock) ContextualValidity(id BlockID) bool   { return true }

type MockState struct{}

func (MockState) ApplyTransactions(layer LayerID, txs Transactions) (uint32, error) {
	return 0, nil
}

func (MockState) ApplyRewards(layer LayerID, miners map[string]struct{}, underQuota map[string]struct{}, bonusReward, diminishedReward *big.Int) {
}

func getMesh(id string) *Mesh {
	lg := log.New(id, "", "")
	layers := NewMesh(NewMemMeshDB(lg), ConfigTst(), &MeshValidatorMock{}, &MockState{}, lg)
	return layers
}

func TestLayers_AddBlock(t *testing.T) {

	layers := getMesh("t1")
	defer layers.Close()

	block1 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data1"))
	block2 := NewExistingBlock(BlockID(uuid.New().ID()), 2, []byte("data2"))
	block3 := NewExistingBlock(BlockID(uuid.New().ID()), 3, []byte("data3"))

	addTransactionsToBlock(block1, 4)

	fmt.Println(block1)

	err := layers.AddBlock(block1)
	assert.NoError(t, err)
	err = layers.AddBlock(block2)
	assert.NoError(t, err)
	err = layers.AddBlock(block3)
	assert.NoError(t, err)

	rBlock2, err := layers.GetBlock(block2.Id)
	assert.NoError(t, err)

	rBlock1, err := layers.GetBlock(block1.Id)
	assert.NoError(t, err)

	assert.True(t, len(rBlock1.Txs) == len(block1.Txs), "block content was wrong")
	assert.True(t, bytes.Compare(rBlock2.Data, []byte("data2")) == 0, "block content was wrong")
}

func TestLayers_AddLayer(t *testing.T) {
	layers := getMesh("t2")
	defer layers.Close()
	id := LayerID(1)
	block1 := NewExistingBlock(BlockID(uuid.New().ID()), id, []byte("data"))
	block2 := NewExistingBlock(BlockID(uuid.New().ID()), id, []byte("data"))
	block3 := NewExistingBlock(BlockID(uuid.New().ID()), id, []byte("data"))
	l, err := layers.GetLayer(id)
	assert.True(t, err != nil, "error: ", err)

	err = layers.AddBlock(block1)
	assert.NoError(t, err)
	err = layers.AddBlock(block2)
	assert.NoError(t, err)
	err = layers.AddBlock(block3)
	assert.NoError(t, err)
	l, err = layers.GetLayer(id)
	assert.NoError(t, err)
	//assert.True(t, layers.VerifiedLayer() == 0, "wrong layer count")
	assert.True(t, string(l.blocks[1].Data) == "data", "wrong block data ")
}

func TestLayers_AddWrongLayer(t *testing.T) {
	layers := getMesh("t3")
	defer layers.Close()
	block1 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block2 := NewExistingBlock(BlockID(uuid.New().ID()), 2, []byte("data data data"))
	block3 := NewExistingBlock(BlockID(uuid.New().ID()), 4, []byte("data data data"))
	l1 := NewExistingLayer(1, []*Block{block1})
	layers.AddBlock(block1)
	layers.ValidateLayer(l1)
	l2 := NewExistingLayer(2, []*Block{block2})
	layers.AddBlock(block2)
	layers.ValidateLayer(l2)
	layers.AddBlock(block3)
	_, err := layers.GetVerifiedLayer(1)
	assert.True(t, err == nil, "error: ", err)
	_, err1 := layers.GetVerifiedLayer(2)
	assert.True(t, err1 == nil, "error: ", err1)
	_, err2 := layers.GetVerifiedLayer(4)
	assert.True(t, err2 != nil, "added wrong layer ", err2)
}

func TestLayers_GetLayer(t *testing.T) {
	layers := getMesh("t4")
	defer layers.Close()
	block1 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block2 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block3 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data data data"))
	l1 := NewExistingLayer(1, []*Block{block1})
	layers.AddBlock(block1)
	layers.ValidateLayer(l1)
	l, err := layers.GetVerifiedLayer(0)
	layers.AddBlock(block2)
	layers.AddBlock(block3)
	l, err = layers.GetVerifiedLayer(1)
	assert.True(t, err == nil, "error: ", err)
	assert.True(t, l.Index() == 1, "wrong layer")
}

func TestLayers_LatestKnownLayer(t *testing.T) {
	layers := getMesh("t6")
	defer layers.Close()
	layers.SetLatestLayer(3)
	layers.SetLatestLayer(7)
	layers.SetLatestLayer(10)
	layers.SetLatestLayer(1)
	layers.SetLatestLayer(2)
	assert.True(t, layers.LatestLayer() == 10, "wrong layer")
}

func TestLayers_WakeUp(t *testing.T) {
	//layers := getMesh(make(chan Peer),  "t5")
	//defer layers.Close()
	//layers.SetLatestLayer(10)
	//assert.True(t, layers.LocalLayerCount() == 10, "wrong layer")
}

func TestLayers_OrphanBlocks(t *testing.T) {
	layers := getMesh("t6")
	defer layers.Close()
	block1 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block2 := NewExistingBlock(BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block3 := NewExistingBlock(BlockID(uuid.New().ID()), 2, []byte("data data data"))
	block4 := NewExistingBlock(BlockID(uuid.New().ID()), 2, []byte("data data data"))
	block5 := NewExistingBlock(BlockID(uuid.New().ID()), 3, []byte("data data data"))
	block5.AddView(block1.ID())
	block5.AddView(block2.ID())
	block5.AddView(block3.ID())
	block5.AddView(block4.ID())
	layers.AddBlock(block1)
	layers.AddBlock(block2)
	layers.AddBlock(block3)
	layers.AddBlock(block4)
	arr, _ := layers.GetOrphanBlocksBefore(3)
	assert.True(t, len(arr) == 4, "wrong layer")
	arr2, _ := layers.GetOrphanBlocksBefore(2)
	assert.Equal(t, len(arr2), 2)
	layers.AddBlock(block5)
	time.Sleep(1 * time.Second)
	arr3, _ := layers.GetOrphanBlocksBefore(4)
	assert.True(t, len(arr3) == 1, "wrong layer")

}
