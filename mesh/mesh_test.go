package mesh

import (
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type MeshValidatorMock struct {}

func (m *MeshValidatorMock)	HandleIncomingLayer(layer *Layer) {}
func (m *MeshValidatorMock) HandleLateBlock(bl *Block) {}

func getMesh(id string) *Mesh {
	time := time.Now()
	bdb := database.NewLevelDbStore("blocks_test_"+id+"_"+time.String(), nil, nil)
	ldb := database.NewLevelDbStore("layers_test_"+id+"_"+time.String(), nil, nil)
	cdb := database.NewLevelDbStore("contextual_test_"+id+"_"+time.String(), nil, nil)
	odb := database.NewLevelDbStore("orphans_test_"+id+"_"+time.String(), nil, nil)
	layers := NewMesh(ldb, bdb, cdb, odb, &MeshValidatorMock{},log.New(id, "", ""))
	return layers
}

func TestLayers_AddBlock(t *testing.T) {

	layers := getMesh("t1")
	defer layers.Close()

	block1 := NewBlock(true, []byte("data1"), time.Now(), 1)
	block2 := NewBlock(true, []byte("data2"), time.Now(), 2)
	block3 := NewBlock(true, []byte("data3"), time.Now(), 3)

	layers.AddBlock(block1)
	layers.AddBlock(block2)
	layers.AddBlock(block3)

	rBlock2, _ := layers.GetBlock(block2.Id)

	assert.True(t, string(rBlock2.Data) == "data2", "wrong layer count")
}

func TestLayers_AddLayer(t *testing.T) {

	layers := getMesh("t2")
	defer layers.Close()
	id := LayerID(1)
	block1 := NewBlock(true, []byte("data"), time.Now(), id)
	block2 := NewBlock(true, []byte("data"), time.Now(), id)
	block3 := NewBlock(true, []byte("data"), time.Now(), id)
	l, err := layers.GetLayer(id)
	assert.True(t, err != nil, "error: ", err)
	layers.AddLayer(NewExistingLayer(1, []*Block{block1, block2, block3}))
	l, err = layers.GetLayer(id)
	//assert.True(t, layers.LocalLayer() == 0, "wrong layer count")
	assert.True(t, string(l.blocks[1].Data) == "data", "wrong block data ")
}

func TestLayers_AddWrongLayer(t *testing.T) {
	layers := getMesh("t3")
	defer layers.Close()
	block1 := NewBlock(true, nil, time.Now(), 1)
	block2 := NewBlock(true, nil, time.Now(), 2)
	block3 := NewBlock(true, nil, time.Now(), 4)
	layers.AddLayer(NewExistingLayer(1, []*Block{block1}))
	layers.AddLayer(NewExistingLayer(2, []*Block{block2}))
	layers.AddLayer(NewExistingLayer(4, []*Block{block3}))
	_, err := layers.GetLayer(1)
	assert.True(t, err == nil, "error: ", err)
	_, err1 := layers.GetLayer(2)
	assert.True(t, err1 == nil, "error: ", err1)
	_, err2 := layers.GetLayer(4)
	assert.True(t, err2 != nil, "added wrong layer ", err2)
}

func TestLayers_GetLayer(t *testing.T) {
	layers := getMesh("t4")
	defer layers.Close()
	block1 := NewBlock(true, nil, time.Now(), 1)
	block2 := NewBlock(true, nil, time.Now(), 1)
	block3 := NewBlock(true, nil, time.Now(), 1)
	layers.AddLayer(NewExistingLayer(1, []*Block{block1}))
	l, err := layers.GetLayer(0)
	layers.AddLayer(NewExistingLayer(3, []*Block{block2}))
	layers.AddLayer(NewExistingLayer(2, []*Block{block3}))
	l, err = layers.GetLayer(1)
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
	assert.True(t, layers.LocalLayer() == 3, "wrong layer count")
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
	assert.True(t, len(layers.GetOrphanBlocks()) == 4, "wrong layer")
	layers.AddBlock(block5)
	assert.True(t, len(layers.GetOrphanBlocks()) == 1, "wrong layer")

}
