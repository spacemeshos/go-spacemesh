package mesh

import (
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func getMesh(newBlockCh chan *Block, id string) Mesh {
	bdb := database.NewLevelDbStore("blocks_test_"+id, nil, nil)
	ldb := database.NewLevelDbStore("layers_test_"+id, nil, nil)
	layers := NewMesh(newBlockCh, ldb, bdb)
	return layers
}

func TestLayers_AddBlock(t *testing.T) {

	layers := getMesh(make(chan *Block), "t1")
	defer layers.Close()

	block1 := NewBlock(true, []byte("data1"), time.Now(), 1)
	block2 := NewBlock(true, []byte("data2"), time.Now(), 2)
	block3 := NewBlock(true, []byte("data3"), time.Now(), 3)

	layers.AddBlock(block1)
	layers.AddBlock(block2)
	layers.AddBlock(block3)

	rBlock2, _ := layers.GetBlock(block2.BlockId)

	assert.True(t, string(rBlock2.Data) == "data2", "wrong layer count")
}

func TestLayers_AddLayer(t *testing.T) {

	layers := getMesh(make(chan *Block), "t2")
	defer layers.Close()

	block1 := NewBlock(true, []byte("data1"), time.Now(), 1)
	block2 := NewBlock(true, []byte("data2"), time.Now(), 2)
	block3 := NewBlock(true, []byte("data3"), time.Now(), 3)
	l, err := layers.GetLayer(0)
	assert.True(t, err != nil, "error: ", err)
	layers.AddLayer(NewExistingLayer(0, []*Block{block1, block2, block3}))
	l, err = layers.GetLayer(0)
	assert.True(t, layers.LocalLayerCount() == 1, "wrong layer count")
	assert.True(t, string(l.blocks[1].Data) == "data2", "wrong block data ")
}

func TestLayers_AddWrongLayer(t *testing.T) {
	layers := getMesh(make(chan *Block), "t3")
	defer layers.Close()
	block1 := NewBlock(true, nil, time.Now(), 0)
	block2 := NewBlock(true, nil, time.Now(), 0)
	block3 := NewBlock(true, nil, time.Now(), 0)
	layers.AddLayer(NewExistingLayer(0, []*Block{block1}))
	layers.AddLayer(NewExistingLayer(1, []*Block{block2}))
	layers.AddLayer(NewExistingLayer(3, []*Block{block3}))
	assert.True(t, layers.LocalLayerCount() == 2, "wrong layer count")
	_, err := layers.GetLayer(0)
	assert.True(t, err == nil, "error: ", err)
	_, err1 := layers.GetLayer(1)
	assert.True(t, err1 == nil, "error: ", err1)
	_, err2 := layers.GetLayer(3)
	assert.True(t, err2 != nil, "added wrong layer ", err2)
}

func TestLayers_GetLayer(t *testing.T) {
	layers := getMesh(make(chan *Block), "t4")
	defer layers.Close()
	block1 := NewBlock(true, nil, time.Now(), 0)
	block2 := NewBlock(true, nil, time.Now(), 0)
	block3 := NewBlock(true, nil, time.Now(), 0)
	layers.AddLayer(NewExistingLayer(0, []*Block{block1}))
	l, err := layers.GetLayer(0)
	layers.AddLayer(NewExistingLayer(3, []*Block{block2}))
	layers.AddLayer(NewExistingLayer(1, []*Block{block3}))
	l, err = layers.GetLayer(1)
	assert.True(t, err == nil, "error: ", err)
	assert.True(t, l.Index() == 1, "wrong layer")
}

func TestLayers_LocalLayerCount(t *testing.T) {
	layers := getMesh(make(chan *Block), "t5")
	defer layers.Close()
	block1 := NewBlock(true, nil, time.Now(), 0)
	block2 := NewBlock(true, nil, time.Now(), 3)
	block3 := NewBlock(true, nil, time.Now(), 1)
	block4 := NewBlock(true, nil, time.Now(), 2)
	layers.AddLayer(NewExistingLayer(0, []*Block{block1}))
	layers.AddLayer(NewExistingLayer(3, []*Block{block2}))
	layers.AddLayer(NewExistingLayer(1, []*Block{block3}))
	layers.AddLayer(NewExistingLayer(2, []*Block{block4}))
	assert.True(t, layers.LocalLayerCount() == 3, "wrong layer count")
}

func TestLayers_LatestKnownLayer(t *testing.T) {
	layers := getMesh(make(chan *Block), "t6")
	defer layers.Close()
	layers.SetLatestKnownLayer(10)
	assert.True(t, layers.LatestKnownLayer() == 10, "wrong layer")
}

func TestLayers_WakeUp(t *testing.T) {
	//layers := getMesh(make(chan Peer), make(chan *Block), "t5")
	//defer layers.Close()
	//layers.SetLatestKnownLayer(10)
	//assert.True(t, layers.LatestKnownLayer() == 10, "wrong layer")
}
