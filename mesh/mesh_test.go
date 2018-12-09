package mesh

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestLayers_AddLayer(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan *Block)
	layers := NewLayers(newPeerCh, newBlockCh)
	defer layers.Close()
	block1 := NewBlock(true, nil, time.Now(), 0)
	block2 := NewBlock(true, nil, time.Now(), 0)
	block3 := NewBlock(true, nil, time.Now(), 0)
	l, err := layers.GetLayer(uint64(0))
	log.Debug(string(l.index))
	layers.AddLayer(NewExistingLayer(0, []*Block{block1, block2, block3}))
	_, err = layers.GetLayer(uint64(0))
	assert.True(t, layers.LocalLayerCount() == 1, "wrong layer count")
	//_, err := layers.GetLayer(uint64(0))
	assert.True(t, err == nil, "error: ", err)
}

func TestLayers_AddWrongLayer(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan *Block)
	layers := NewLayers(newPeerCh, newBlockCh)
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

//func TestLayers_AddInvalidLayer(t *testing.T) {
//	//todo complete layer validation and write test
//}

func TestLayers_GetLayer(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan *Block)
	layers := NewLayers(newPeerCh, newBlockCh)
	defer layers.Close()
	block1 := NewBlock(true, nil, time.Now(), 0)
	block2 := NewBlock(true, nil, time.Now(), 0)
	block3 := NewBlock(true, nil, time.Now(), 0)
	layers.AddLayer(NewExistingLayer(0, []*Block{block1}))
	layers.AddLayer(NewExistingLayer(3, []*Block{block2}))
	layers.AddLayer(NewExistingLayer(1, []*Block{block3}))
	l, err := layers.GetLayer(1)
	assert.True(t, err == nil, "error: ", err)
	assert.True(t, l.Index() == 1, "wrong layer")
}

func TestLayers_LocalLayerCount(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan *Block)
	layers := NewLayers(newPeerCh, newBlockCh)
	defer layers.Close()
	block1 := NewBlock(true, nil, time.Now(), 0)
	block2 := NewBlock(true, nil, time.Now(), 0)
	block3 := NewBlock(true, nil, time.Now(), 0)
	block4 := NewBlock(true, nil, time.Now(), 0)
	layers.AddLayer(NewExistingLayer(1, []*Block{block1}))
	layers.AddLayer(NewExistingLayer(3, []*Block{block2}))
	layers.AddLayer(NewExistingLayer(2, []*Block{block3}))
	layers.AddLayer(NewExistingLayer(2, []*Block{block4}))
	assert.True(t, layers.LocalLayerCount() == 3, "wrong layer count")
}

func TestLayers_LatestKnownLayer(t *testing.T) {
	newPeerCh := make(chan Peer)
	newBlockCh := make(chan *Block)
	layers := NewLayers(newPeerCh, newBlockCh)
	defer layers.Close()
	layers.SetLatestKnownLayer(10)
	assert.True(t, layers.LatestKnownLayer() == 10, "wrong layer")
}
