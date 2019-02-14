package mesh

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/stretchr/testify/assert"
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

func (MockState) ApplyTransactions(layer state.LayerID, txs state.Transactions) (uint32, error) {
	return 0, nil
}

func getMesh(id string) *Mesh {

	//time := time.Now()
	bdb := database.NewMemDatabase()
	ldb := database.NewMemDatabase()
	cdb := database.NewMemDatabase()

	layers := NewMesh(ldb, bdb, cdb, &MeshValidatorMock{}, &MockState{}, log.New(id, "", ""))
	return layers
}

func TestLayers_AddBlock(t *testing.T) {

	layers := getMesh("t1")
	defer layers.Close()

	block1 := NewBlock(true, []byte("data1"), time.Now(), 1)
	block2 := NewBlock(true, []byte("data2"), time.Now(), 2)
	block3 := NewBlock(true, []byte("data3"), time.Now(), 3)

	err := layers.AddBlock(block1)
	assert.NoError(t, err)
	err = layers.AddBlock(block2)
	assert.NoError(t, err)
	err = layers.AddBlock(block3)
	assert.NoError(t, err)

	rBlock2, err := layers.GetBlock(block2.Id)
	assert.NoError(t, err)

	assert.True(t, bytes.Compare(rBlock2.Data, []byte("data2")) == 0, "block content was wrong")
}

func TestLayers_AddLayer(t *testing.T) {
	layers := getMesh("t2")
	defer layers.Close()
	id := LayerID(1)
	data := []byte("data")
	block1 := NewBlock(true, data, time.Now(), id)
	block2 := NewBlock(true, data, time.Now(), id)
	block3 := NewBlock(true, data, time.Now(), id)
	l, err := layers.GetLayer(id)
	assert.True(t, err != nil, "error: ", err)

	err = layers.AddLayer(NewExistingLayer(1, []*Block{block1, block2, block3}))
	assert.NoError(t, err)
	l, err = layers.GetLayer(id)
	assert.NoError(t, err)
	//assert.True(t, layers.VerifiedLayer() == 0, "wrong layer count")
	assert.True(t, string(l.blocks[1].Data) == "data", "wrong block data ")
}

func TestLayers_AddWrongLayer(t *testing.T) {
	layers := getMesh("t3")
	defer layers.Close()
	block1 := NewBlock(true, nil, time.Now(), 1)
	block2 := NewBlock(true, nil, time.Now(), 2)
	block3 := NewBlock(true, nil, time.Now(), 4)
	l1 := NewExistingLayer(1, []*Block{block1})
	layers.AddLayer(l1)
	layers.ValidateLayer(l1)
	l2 := NewExistingLayer(2, []*Block{block2})
	layers.AddLayer(l2)
	layers.ValidateLayer(l2)
	l3 := NewExistingLayer(4, []*Block{block3})
	layers.AddLayer(l3)
	layers.ValidateLayer(l3)
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
	block1 := NewBlock(true, nil, time.Now(), 1)
	block2 := NewBlock(true, nil, time.Now(), 1)
	block3 := NewBlock(true, nil, time.Now(), 1)
	l1 := NewExistingLayer(1, []*Block{block1})
	layers.AddLayer(l1)
	layers.ValidateLayer(l1)
	l, err := layers.GetVerifiedLayer(0)
	layers.AddLayer(NewExistingLayer(3, []*Block{block2}))
	layers.AddLayer(NewExistingLayer(2, []*Block{block3}))
	l, err = layers.GetVerifiedLayer(1)
	assert.True(t, err == nil, "error: ", err)
	assert.True(t, l.Index() == 1, "wrong layer")
}

func TestLayers_LocalLayerCount(t *testing.T) {
	layers := getMesh("t5")
	defer layers.Close()
	block1 := NewBlock(true, nil, time.Now(), 1)
	block2 := NewBlock(true, nil, time.Now(), 4)
	block3 := NewBlock(true, nil, time.Now(), 2)
	block4 := NewBlock(true, nil, time.Now(), 1)
	layers.AddLayer(NewExistingLayer(1, []*Block{block1}))
	layers.AddLayer(NewExistingLayer(4, []*Block{block2}))
	layers.AddLayer(NewExistingLayer(2, []*Block{block3}))
	layers.AddLayer(NewExistingLayer(3, []*Block{block4}))
	assert.Equal(t, uint32(3), layers.LatestLayer(), "wrong layer count")
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
	block1 := NewBlock(true, nil, time.Now(), 1)
	block2 := NewBlock(true, nil, time.Now(), 1)
	block3 := NewBlock(true, nil, time.Now(), 2)
	block4 := NewBlock(true, nil, time.Now(), 2)
	block5 := NewBlock(true, nil, time.Now(), 3)
	block5.AddView(block1.ID())
	block5.AddView(block2.ID())
	block5.AddView(block3.ID())
	block5.AddView(block4.ID())
	layers.AddBlock(block1)
	layers.AddBlock(block2)
	layers.AddBlock(block3)
	layers.AddBlock(block4)
	assert.True(t, len(layers.GetOrphanBlocksExcept(5)) == 4, "wrong layer")
	assert.Equal(t, len(layers.GetOrphanBlocksExcept(2)), 2)
	layers.AddBlock(block5)
	assert.True(t, len(layers.GetOrphanBlocksExcept(5)) == 1, "wrong layer")

}
