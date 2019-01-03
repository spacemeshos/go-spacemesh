package mesh

import (
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func getMesh(id string) Mesh {
	bdb := database.NewLevelDbStore("blocks_test_"+id, nil, nil)
	ldb := database.NewLevelDbStore("layers_test_"+id, nil, nil)
	cdb := database.NewLevelDbStore("contextual_test_"+id, nil, nil)
	layers := NewMesh(ldb, bdb, cdb)
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
	layers.AddLayer(NewExistingLayer(1, []*TortoiseBlock{block1, block2, block3}))
	l, err = layers.GetLayer(id)
	//assert.True(t, layers.LatestIrreversible() == 0, "wrong layer count")
	assert.True(t, string(l.blocks[1].Data) == "data", "wrong block data ")
}

func TestLayers_AddWrongLayer(t *testing.T) {
	layers := getMesh("t3")
	defer layers.Close()
	block1 := NewBlock(true, nil, time.Now(), 1)
	block2 := NewBlock(true, nil, time.Now(), 2)
	block3 := NewBlock(true, nil, time.Now(), 4)
	layers.AddLayer(NewExistingLayer(1, []*TortoiseBlock{block1}))
	layers.AddLayer(NewExistingLayer(2, []*TortoiseBlock{block2}))
	layers.AddLayer(NewExistingLayer(4, []*TortoiseBlock{block3}))
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
	layers.AddLayer(NewExistingLayer(1, []*TortoiseBlock{block1}))
	l, err := layers.GetLayer(0)
	layers.AddLayer(NewExistingLayer(3, []*TortoiseBlock{block2}))
	layers.AddLayer(NewExistingLayer(2, []*TortoiseBlock{block3}))
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
	layers.AddLayer(NewExistingLayer(1, []*TortoiseBlock{block1}))
	layers.AddLayer(NewExistingLayer(4, []*TortoiseBlock{block2}))
	layers.AddLayer(NewExistingLayer(2, []*TortoiseBlock{block3}))
	layers.AddLayer(NewExistingLayer(3, []*TortoiseBlock{block4}))
	assert.True(t, layers.LatestIrreversible() == 3, "wrong layer count")
}

func TestLayers_LatestKnownLayer(t *testing.T) {
	layers := getMesh("t6")
	defer layers.Close()
	layers.SetLatestKnownLayer(3)
	layers.SetLatestKnownLayer(7)
	layers.SetLatestKnownLayer(10)
	layers.SetLatestKnownLayer(1)
	layers.SetLatestKnownLayer(2)
	assert.True(t, layers.LatestKnownLayer() == 10, "wrong layer")
}

func TestLayers_WakeUp(t *testing.T) {
	//layers := getMesh(make(chan Peer),  "t5")
	//defer layers.Close()
	//layers.SetLatestKnownLayer(10)
	//assert.True(t, layers.LocalLayerCount() == 10, "wrong layer")
}
